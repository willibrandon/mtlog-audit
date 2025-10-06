// Package testutil provides test utilities and helpers for integration tests.
package testutil

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	//nolint:staticcheck // AWS SDK v1 - migration to v2 planned
	"github.com/aws/aws-sdk-go/aws"
	//nolint:staticcheck // AWS SDK v1 - migration to v2 planned
	"github.com/aws/aws-sdk-go/aws/credentials"
	//nolint:staticcheck // AWS SDK v1 - migration to v2 planned
	"github.com/aws/aws-sdk-go/aws/session"
	//nolint:staticcheck // AWS SDK v1 - migration to v2 planned
	"github.com/aws/aws-sdk-go/service/s3"
)

// ServiceChecker checks if test services are available
type ServiceChecker struct {
	client *http.Client
}

// NewServiceChecker creates a new service checker
func NewServiceChecker() *ServiceChecker {
	return &ServiceChecker{
		client: &http.Client{
			Timeout: 2 * time.Second,
		},
	}
}

// IsMinIOAvailable checks if MinIO is running
func (sc *ServiceChecker) IsMinIOAvailable() bool {
	endpoint := os.Getenv("MINIO_ENDPOINT")
	if endpoint == "" {
		endpoint = "http://localhost:9000"
	}

	resp, err := sc.client.Get(endpoint + "/minio/health/live")
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()

	return resp.StatusCode == http.StatusOK
}

// IsLocalStackAvailable checks if LocalStack is running
func (sc *ServiceChecker) IsLocalStackAvailable() bool {
	endpoint := os.Getenv("LOCALSTACK_ENDPOINT")
	if endpoint == "" {
		endpoint = "http://localhost:4566"
	}

	resp, err := sc.client.Get(endpoint + "/_localstack/health")
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()

	return resp.StatusCode == http.StatusOK
}

// IsAzuriteAvailable checks if Azurite is running
func (sc *ServiceChecker) IsAzuriteAvailable() bool {
	// Check if we can connect to Azurite blob endpoint
	endpoint := "http://localhost:10000"
	resp, err := sc.client.Get(endpoint + "/devstoreaccount1?comp=list")
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()

	// Azurite returns 403 for unauthorized, which means it's running
	// 200 would mean successful auth (shouldn't happen without proper headers)
	return resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusOK
}

// IsFakeGCSAvailable checks if Fake GCS is running
func (sc *ServiceChecker) IsFakeGCSAvailable() bool {
	endpoint := "http://localhost:4443"
	resp, err := sc.client.Get(endpoint + "/storage/v1/b")
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()

	return resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusUnauthorized
}

// GetMinIOClient returns a configured S3 client for MinIO
func GetMinIOClient() (*s3.S3, error) {
	endpoint := os.Getenv("MINIO_ENDPOINT")
	if endpoint == "" {
		endpoint = "http://localhost:9000"
	}

	accessKey := os.Getenv("MINIO_ACCESS_KEY")
	if accessKey == "" {
		accessKey = "minioadmin"
	}

	secretKey := os.Getenv("MINIO_SECRET_KEY")
	if secretKey == "" {
		secretKey = "minioadmin"
	}

	sess, err := session.NewSession(&aws.Config{
		Endpoint:         aws.String(endpoint),
		Region:           aws.String("us-east-1"),
		Credentials:      credentials.NewStaticCredentials(accessKey, secretKey, ""),
		S3ForcePathStyle: aws.Bool(true),
		DisableSSL:       aws.Bool(true),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	return s3.New(sess), nil
}

// CreateTestBucket creates a test bucket with appropriate settings
func CreateTestBucket(client *s3.S3, bucket string, enableVersioning, enableEncryption bool) error {
	// Create bucket
	_, err := client.CreateBucket(&s3.CreateBucketInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		// Ignore if bucket already exists
		if !isAlreadyExistsError(err) {
			return fmt.Errorf("failed to create bucket: %w", err)
		}
	}

	// Enable versioning if requested
	if enableVersioning {
		_, err = client.PutBucketVersioning(&s3.PutBucketVersioningInput{
			Bucket: aws.String(bucket),
			VersioningConfiguration: &s3.VersioningConfiguration{
				Status: aws.String("Enabled"),
			},
		})
		if err != nil {
			return fmt.Errorf("failed to enable versioning: %w", err)
		}
	}

	// Enable encryption if requested
	if enableEncryption {
		_, err = client.PutBucketEncryption(&s3.PutBucketEncryptionInput{
			Bucket: aws.String(bucket),
			ServerSideEncryptionConfiguration: &s3.ServerSideEncryptionConfiguration{
				Rules: []*s3.ServerSideEncryptionRule{
					{
						ApplyServerSideEncryptionByDefault: &s3.ServerSideEncryptionByDefault{
							SSEAlgorithm: aws.String(s3.ServerSideEncryptionAes256),
						},
					},
				},
			},
		})
		if err != nil {
			return fmt.Errorf("failed to enable encryption: %w", err)
		}
	}

	return nil
}

// VerifyS3Encryption checks if an object is encrypted
func VerifyS3Encryption(client *s3.S3, bucket, key string) (bool, error) {
	resp, err := client.HeadObject(&s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return false, fmt.Errorf("failed to head object: %w", err)
	}

	// Check if server-side encryption is enabled
	return resp.ServerSideEncryption != nil && *resp.ServerSideEncryption == s3.ServerSideEncryptionAes256, nil
}

// VerifyS3ObjectLock checks if object lock is configured
func VerifyS3ObjectLock(client *s3.S3, bucket string) (bool, *time.Duration, error) {
	resp, err := client.GetObjectLockConfiguration(&s3.GetObjectLockConfigurationInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		return false, nil, err
	}

	if resp.ObjectLockConfiguration == nil || resp.ObjectLockConfiguration.ObjectLockEnabled == nil {
		return false, nil, nil
	}

	enabled := *resp.ObjectLockConfiguration.ObjectLockEnabled == s3.ObjectLockEnabledEnabled

	var retention *time.Duration
	if resp.ObjectLockConfiguration.Rule != nil &&
		resp.ObjectLockConfiguration.Rule.DefaultRetention != nil &&
		resp.ObjectLockConfiguration.Rule.DefaultRetention.Days != nil {
		days := *resp.ObjectLockConfiguration.Rule.DefaultRetention.Days
		duration := time.Duration(days) * 24 * time.Hour
		retention = &duration
	}

	return enabled, retention, nil
}

// CleanupTestBucket removes all objects and deletes the bucket
func CleanupTestBucket(client *s3.S3, bucket string) error {
	// List and delete all objects
	listResp, err := client.ListObjectsV2(&s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		return fmt.Errorf("failed to list objects: %w", err)
	}

	for _, obj := range listResp.Contents {
		_, err = client.DeleteObject(&s3.DeleteObjectInput{
			Bucket: aws.String(bucket),
			Key:    obj.Key,
		})
		if err != nil {
			return fmt.Errorf("failed to delete object %s: %w", *obj.Key, err)
		}
	}

	// Delete the bucket
	_, err = client.DeleteBucket(&s3.DeleteBucketInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		return fmt.Errorf("failed to delete bucket: %w", err)
	}

	return nil
}

// WaitForService waits for a service to be available
func WaitForService(name string, checkFunc func() bool, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s service not available after %v", name, timeout)
		case <-ticker.C:
			if checkFunc() {
				return nil
			}
		}
	}
}

func isAlreadyExistsError(err error) bool {
	if err == nil {
		return false
	}
	return err.Error() == s3.ErrCodeBucketAlreadyExists ||
		err.Error() == s3.ErrCodeBucketAlreadyOwnedByYou
}
