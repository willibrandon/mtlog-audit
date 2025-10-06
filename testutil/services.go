// Package testutil provides test utilities and helpers for integration tests.
package testutil

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
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
func GetMinIOClient() (*s3.Client, error) {
	ctx := context.Background()

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

	// Load config with static credentials
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	// Create S3 client with custom endpoint for MinIO
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true
	})

	return client, nil
}

// CreateTestBucket creates a test bucket with appropriate settings
func CreateTestBucket(client *s3.Client, bucket string, enableVersioning, enableEncryption bool) error {
	ctx := context.Background()

	// Create bucket
	_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{
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
		_, err = client.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{
			Bucket: aws.String(bucket),
			VersioningConfiguration: &types.VersioningConfiguration{
				Status: types.BucketVersioningStatusEnabled,
			},
		})
		if err != nil {
			return fmt.Errorf("failed to enable versioning: %w", err)
		}
	}

	// Enable encryption if requested
	if enableEncryption {
		_, err = client.PutBucketEncryption(ctx, &s3.PutBucketEncryptionInput{
			Bucket: aws.String(bucket),
			ServerSideEncryptionConfiguration: &types.ServerSideEncryptionConfiguration{
				Rules: []types.ServerSideEncryptionRule{
					{
						ApplyServerSideEncryptionByDefault: &types.ServerSideEncryptionByDefault{
							SSEAlgorithm: types.ServerSideEncryptionAes256,
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
func VerifyS3Encryption(client *s3.Client, bucket, key string) (bool, error) {
	ctx := context.Background()

	resp, err := client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return false, fmt.Errorf("failed to head object: %w", err)
	}

	// Check if server-side encryption is enabled
	return resp.ServerSideEncryption == types.ServerSideEncryptionAes256, nil
}

// VerifyS3ObjectLock checks if object lock is configured
func VerifyS3ObjectLock(client *s3.Client, bucket string) (bool, *time.Duration, error) {
	ctx := context.Background()

	resp, err := client.GetObjectLockConfiguration(ctx, &s3.GetObjectLockConfigurationInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		return false, nil, err
	}

	if resp.ObjectLockConfiguration == nil {
		return false, nil, nil
	}

	enabled := resp.ObjectLockConfiguration.ObjectLockEnabled == types.ObjectLockEnabledEnabled

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
func CleanupTestBucket(client *s3.Client, bucket string) error {
	ctx := context.Background()

	// List and delete all objects using paginator
	paginator := s3.NewListObjectsV2Paginator(client, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to list objects: %w", err)
		}

		for _, obj := range page.Contents {
			_, err = client.DeleteObject(ctx, &s3.DeleteObjectInput{
				Bucket: aws.String(bucket),
				Key:    obj.Key,
			})
			if err != nil {
				return fmt.Errorf("failed to delete object %s: %w", *obj.Key, err)
			}
		}
	}

	// Delete the bucket
	_, err := client.DeleteBucket(ctx, &s3.DeleteBucketInput{
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

	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		code := apiErr.ErrorCode()
		return code == "BucketAlreadyExists" || code == "BucketAlreadyOwnedByYou"
	}

	return false
}
