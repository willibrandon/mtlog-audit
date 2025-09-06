//go:build integration
// +build integration

package integration

import (
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/stretchr/testify/require"
	audit "github.com/willibrandon/mtlog-audit"
	"github.com/willibrandon/mtlog-audit/backends"
	"github.com/willibrandon/mtlog-audit/testutil"
	"github.com/willibrandon/mtlog/core"
)

func TestS3BackendCompliance(t *testing.T) {
	// Check if MinIO is available
	checker := testutil.NewServiceChecker()
	if !checker.IsMinIOAvailable() {
		t.Skip("MinIO not available - run 'make docker-up' first")
	}

	// Get MinIO client
	s3Client, err := testutil.GetMinIOClient()
	require.NoError(t, err, "Failed to create MinIO client")

	// Test bucket name
	testBucket := fmt.Sprintf("test-compliance-%d", time.Now().Unix())

	// Clean up bucket after test
	defer func() {
		_ = testutil.CleanupTestBucket(s3Client, testBucket)
	}()

	t.Run("ServerSideEncryption", func(t *testing.T) {
		// Create bucket with encryption
		err := testutil.CreateTestBucket(s3Client, testBucket, false, true)
		require.NoError(t, err)

		// Create sink with S3 backend
		tempDir := t.TempDir()
		sink, err := audit.New(
			audit.WithWAL(tempDir+"/s3-enc.wal"),
			audit.WithCompliance("HIPAA"),
			audit.WithBackend(backends.S3Config{
				Bucket:               testBucket,
				Region:               "us-east-1",
				Prefix:               "encrypted/",
				ServerSideEncryption: true,
			}),
		)
		require.NoError(t, err)
		defer sink.Close()

		// Write an event
		event := &core.LogEvent{
			Timestamp:       time.Now(),
			Level:           core.InformationLevel,
			MessageTemplate: "Test encryption",
			Properties: map[string]interface{}{
				"data": "sensitive",
			},
		}
		sink.Emit(event)

		// Force flush to S3
		time.Sleep(2 * time.Second)

		// List objects in bucket
		listResp, err := s3Client.ListObjectsV2(&s3.ListObjectsV2Input{
			Bucket: aws.String(testBucket),
			Prefix: aws.String("encrypted/"),
		})
		require.NoError(t, err)
		require.Greater(t, len(listResp.Contents), 0, "No objects found in S3")

		// Verify encryption on each object
		for _, obj := range listResp.Contents {
			headResp, err := s3Client.HeadObject(&s3.HeadObjectInput{
				Bucket: aws.String(testBucket),
				Key:    obj.Key,
			})
			require.NoError(t, err)

			// Check server-side encryption
			require.NotNil(t, headResp.ServerSideEncryption,
				"Object %s is not encrypted", *obj.Key)
			require.Equal(t, s3.ServerSideEncryptionAes256, *headResp.ServerSideEncryption,
				"Object %s has wrong encryption type", *obj.Key)
		}
	})

	t.Run("Versioning", func(t *testing.T) {
		versionBucket := fmt.Sprintf("test-versioning-%d", time.Now().Unix())
		defer func() {
			_ = testutil.CleanupTestBucket(s3Client, versionBucket)
		}()

		// Create bucket with versioning
		err := testutil.CreateTestBucket(s3Client, versionBucket, true, false)
		require.NoError(t, err)

		// Verify versioning is enabled
		versionResp, err := s3Client.GetBucketVersioning(&s3.GetBucketVersioningInput{
			Bucket: aws.String(versionBucket),
		})
		require.NoError(t, err)
		require.NotNil(t, versionResp.Status)
		require.Equal(t, "Enabled", *versionResp.Status, "Versioning not enabled")

		// Create sink with versioning
		tempDir := t.TempDir()
		sink, err := audit.New(
			audit.WithWAL(tempDir+"/s3-version.wal"),
			audit.WithCompliance("SOX"),
			audit.WithBackend(backends.S3Config{
				Bucket:     versionBucket,
				Region:     "us-east-1",
				Prefix:     "versioned/",
				Versioning: true,
			}),
		)
		require.NoError(t, err)
		defer sink.Close()

		// Write multiple events
		for i := 0; i < 5; i++ {
			event := &core.LogEvent{
				Timestamp:       time.Now(),
				Level:           core.InformationLevel,
				MessageTemplate: fmt.Sprintf("Version test %d", i),
				Properties: map[string]interface{}{
					"iteration": i,
				},
			}
			sink.Emit(event)
		}
	})

	t.Run("ObjectLockCompliance", func(t *testing.T) {
		// Note: Object Lock requires bucket to be created with object lock enabled
		// MinIO supports this but requires special bucket creation
		lockBucket := fmt.Sprintf("test-lock-%d", time.Now().Unix())
		defer func() {
			_ = testutil.CleanupTestBucket(s3Client, lockBucket)
		}()

		// Create bucket with object lock configuration
		_, err := s3Client.CreateBucket(&s3.CreateBucketInput{
			Bucket:                     aws.String(lockBucket),
			ObjectLockEnabledForBucket: aws.Bool(true),
		})
		if err != nil {
			// MinIO might not support object lock in default config
			t.Skipf("Object lock not supported: %v", err)
		}

		// Configure object lock retention
		_, err = s3Client.PutObjectLockConfiguration(&s3.PutObjectLockConfigurationInput{
			Bucket: aws.String(lockBucket),
			ObjectLockConfiguration: &s3.ObjectLockConfiguration{
				ObjectLockEnabled: aws.String(s3.ObjectLockEnabledEnabled),
				Rule: &s3.ObjectLockRule{
					DefaultRetention: &s3.DefaultRetention{
						Mode: aws.String(s3.ObjectLockRetentionModeCompliance),
						Days: aws.Int64(2190), // 6 years for HIPAA
					},
				},
			},
		})
		require.NoError(t, err)

		// Verify object lock configuration
		lockConfig, err := s3Client.GetObjectLockConfiguration(&s3.GetObjectLockConfigurationInput{
			Bucket: aws.String(lockBucket),
		})
		require.NoError(t, err)
		require.NotNil(t, lockConfig.ObjectLockConfiguration)
		require.Equal(t, s3.ObjectLockEnabledEnabled,
			*lockConfig.ObjectLockConfiguration.ObjectLockEnabled)

		// Create sink with object lock
		tempDir := t.TempDir()
		sink, err := audit.New(
			audit.WithWAL(tempDir+"/s3-lock.wal"),
			audit.WithCompliance("HIPAA"),
			audit.WithBackend(backends.S3Config{
				Bucket:        lockBucket,
				Region:        "us-east-1",
				Prefix:        "locked/",
				ObjectLock:    true,
				RetentionDays: 2190, // 6 years
			}),
		)
		require.NoError(t, err)
		defer sink.Close()

		// Write PHI event
		event := &core.LogEvent{
			Timestamp:       time.Now(),
			Level:           core.InformationLevel,
			MessageTemplate: "Patient record access",
			Properties: map[string]interface{}{
				"PatientID": "P-12345",
				"PHI":       true,
			},
		}
		sink.Emit(event)
	})

	t.Run("StorageClass", func(t *testing.T) {
		storageBucket := fmt.Sprintf("test-storage-%d", time.Now().Unix())
		defer func() {
			_ = testutil.CleanupTestBucket(s3Client, storageBucket)
		}()

		// Create bucket
		err := testutil.CreateTestBucket(s3Client, storageBucket, false, false)
		require.NoError(t, err)

		// Test different storage classes
		storageClasses := []string{
			"STANDARD",
			"REDUCED_REDUNDANCY",
			"STANDARD_IA",
		}

		for _, storageClass := range storageClasses {
			t.Run(storageClass, func(t *testing.T) {
				// Create sink with specific storage class
				tempDir := t.TempDir()
				sink, err := audit.New(
					audit.WithWAL(tempDir+fmt.Sprintf("/s3-%s.wal", storageClass)),
					audit.WithBackend(backends.S3Config{
						Bucket:       storageBucket,
						Region:       "us-east-1",
						Prefix:       fmt.Sprintf("%s/", storageClass),
						StorageClass: storageClass,
					}),
				)
				// MinIO might not support all storage classes
				if err != nil {
					t.Skipf("Storage class %s not supported: %v", storageClass, err)
				}
				defer sink.Close()

				// Write event
				event := &core.LogEvent{
					Timestamp:       time.Now(),
					Level:           core.InformationLevel,
					MessageTemplate: fmt.Sprintf("Test %s storage", storageClass),
				}
				sink.Emit(event)
			})
		}
	})
}

func TestS3BackendIntegrityVerification(t *testing.T) {
	// Check if MinIO is available
	checker := testutil.NewServiceChecker()
	if !checker.IsMinIOAvailable() {
		t.Skip("MinIO not available - run 'make docker-up' first")
	}

	// Get MinIO client
	s3Client, err := testutil.GetMinIOClient()
	require.NoError(t, err)

	// Test bucket
	testBucket := fmt.Sprintf("test-integrity-%d", time.Now().Unix())
	defer func() {
		_ = testutil.CleanupTestBucket(s3Client, testBucket)
	}()

	// Create bucket
	err = testutil.CreateTestBucket(s3Client, testBucket, false, true)
	require.NoError(t, err)

	// Create sink
	tempDir := t.TempDir()
	sink, err := audit.New(
		audit.WithWAL(tempDir+"/s3-integrity.wal"),
		audit.WithCompliance("PCI-DSS"),
		audit.WithBackend(backends.S3Config{
			Bucket:               testBucket,
			Region:               "us-east-1",
			Prefix:               "audit/",
			ServerSideEncryption: true,
		}),
	)
	require.NoError(t, err)
	defer sink.Close()

	// Write events
	numEvents := 100
	for i := 0; i < numEvents; i++ {
		event := &core.LogEvent{
			Timestamp:       time.Now(),
			Level:           core.InformationLevel,
			MessageTemplate: fmt.Sprintf("Event %d", i),
			Properties: map[string]interface{}{
				"index":     i,
				"timestamp": time.Now().Format(time.RFC3339),
			},
		}
		sink.Emit(event)
	}

	// Verify integrity
	report, err := sink.VerifyIntegrity()
	require.NoError(t, err)
	require.True(t, report.Valid, "Integrity check failed")
	require.GreaterOrEqual(t, report.TotalRecords, numEvents,
		"Not all records were stored")

	// Verify S3-specific integrity features
	// The following code verifies:
	// - ETags exist for content integrity verification
	// - Server-side encryption is applied to objects
	// - Content-Type headers are properly set

	listResp, err := s3Client.ListObjectsV2(&s3.ListObjectsV2Input{
		Bucket: aws.String(testBucket),
		Prefix: aws.String("audit/"),
	})
	require.NoError(t, err)

	for _, obj := range listResp.Contents {
		// Verify each object
		headResp, err := s3Client.HeadObject(&s3.HeadObjectInput{
			Bucket: aws.String(testBucket),
			Key:    obj.Key,
		})
		require.NoError(t, err)

		// Check ETag exists (integrity checksum)
		require.NotNil(t, headResp.ETag, "Object missing ETag")
		require.NotEmpty(t, *headResp.ETag, "Empty ETag")

		// Check encryption
		require.NotNil(t, headResp.ServerSideEncryption, "Object not encrypted")

		// Check content type
		if headResp.ContentType != nil {
			require.Contains(t, []string{"application/json", "application/octet-stream"},
				*headResp.ContentType, "Unexpected content type")
		}
	}
}
