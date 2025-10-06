package backends

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/willibrandon/mtlog-audit/monitoring"
	"github.com/willibrandon/mtlog/core"
)

// S3Backend implements AWS S3 storage backend with compliance features
type S3Backend struct {
	lastWrite     time.Time
	client        *s3.Client
	uploader      *manager.Uploader
	downloader    *manager.Downloader
	bucket        string
	prefix        string
	region        string
	storageClass  string
	encryption    string
	kmsKeyID      string
	currentBatch  []*core.LogEvent
	retentionDays int
	batchSize     int
	writeCount    int64
	errorCount    int64
	mu            sync.RWMutex
	closed        atomic.Bool
	objectLock    bool
	compress      bool
	versioning    bool
}

// S3Option configures S3 backend
type S3Option func(*S3Backend)

// WithStorageClass sets the S3 storage class
func WithStorageClass(class string) S3Option {
	return func(s *S3Backend) {
		s.storageClass = class
	}
}

// WithServerSideEncryption enables server-side encryption
func WithServerSideEncryption(algorithm string) S3Option {
	return func(s *S3Backend) {
		s.encryption = algorithm
	}
}

// WithKMSKeyID sets the KMS key for encryption
func WithKMSKeyID(keyID string) S3Option {
	return func(s *S3Backend) {
		s.kmsKeyID = keyID
	}
}

// WithVersioning enables S3 versioning
func WithVersioning() S3Option {
	return func(s *S3Backend) {
		s.versioning = true
	}
}

// WithObjectLock enables S3 Object Lock for compliance
func WithObjectLock(retentionDays int) S3Option {
	return func(s *S3Backend) {
		s.objectLock = true
		s.retentionDays = retentionDays
	}
}

// WithCompression enables gzip compression
func WithCompression() S3Option {
	return func(s *S3Backend) {
		s.compress = true
	}
}

// WithBatchSize sets the batch size for writes
func WithBatchSize(size int) S3Option {
	return func(s *S3Backend) {
		s.batchSize = size
	}
}

// NewS3Backend creates a new S3 backend
func NewS3Backend(cfg S3Config, opts ...S3Option) (*S3Backend, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid S3 config: %w", err)
	}

	ctx := context.Background()

	// Load AWS SDK config
	// For testing with MinIO/LocalStack, check for static credentials first
	configOpts := []func(*config.LoadOptions) error{
		config.WithRegion(cfg.Region),
	}

	// Check for static credentials in environment (for testing)
	if accessKey := os.Getenv("AWS_ACCESS_KEY_ID"); accessKey != "" {
		if secretKey := os.Getenv("AWS_SECRET_ACCESS_KEY"); secretKey != "" {
			configOpts = append(configOpts,
				config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
			)
		}
	}

	awsCfg, err := config.LoadDefaultConfig(ctx, configOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Support LocalStack/MinIO for testing
	if endpoint := getS3Endpoint(); endpoint != "" {
		awsCfg.BaseEndpoint = aws.String(endpoint)
	}

	// Create S3 client with custom options
	s3Client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		// Support LocalStack/MinIO path-style URLs
		if endpoint := getS3Endpoint(); endpoint != "" {
			o.UsePathStyle = true
		}
	})

	backend := &S3Backend{
		client:       s3Client,
		uploader:     manager.NewUploader(s3Client),
		downloader:   manager.NewDownloader(s3Client),
		bucket:       cfg.Bucket,
		prefix:       cfg.Prefix,
		region:       cfg.Region,
		storageClass: "STANDARD",
		encryption:   "AES256", // Default to SSE-S3
		batchSize:    100,
		currentBatch: make([]*core.LogEvent, 0, 100),
	}

	// Apply options
	for _, opt := range opts {
		opt(backend)
	}

	// Apply config options
	if cfg.ServerSideEncryption {
		backend.encryption = "AES256"
	}
	if cfg.Versioning {
		backend.versioning = true
	}
	if cfg.ObjectLock {
		backend.objectLock = true
		backend.retentionDays = cfg.RetentionDays
	}
	if cfg.StorageClass != "" {
		backend.storageClass = cfg.StorageClass
	}

	// Verify bucket exists and is accessible
	if err := backend.verifyBucket(); err != nil {
		return nil, fmt.Errorf("bucket verification failed: %w", err)
	}

	// Enable versioning if requested
	if backend.versioning {
		if err := backend.enableVersioning(); err != nil {
			return nil, fmt.Errorf("failed to enable versioning: %w", err)
		}
	}

	// Configure Object Lock if requested
	if backend.objectLock {
		if err := backend.configureObjectLock(); err != nil {
			return nil, fmt.Errorf("failed to configure Object Lock: %w", err)
		}
	}

	return backend, nil
}

// Write writes an event to S3
func (s *S3Backend) Write(event *core.LogEvent) error {
	if s.closed.Load() {
		return &BackendError{Backend: "s3", Op: "write", Err: fmt.Errorf("backend closed")}
	}

	startTime := time.Now()
	defer func() {
		monitoring.RecordBackendLatency("s3", "write", time.Since(startTime))
	}()

	s.mu.Lock()
	s.currentBatch = append(s.currentBatch, event)

	// Check if we should flush
	shouldFlush := len(s.currentBatch) >= s.batchSize ||
		time.Since(s.lastWrite) > 5*time.Second

	if !shouldFlush {
		s.mu.Unlock()
		return nil
	}

	// Flush the batch
	batch := s.currentBatch
	s.currentBatch = make([]*core.LogEvent, 0, s.batchSize)
	s.lastWrite = time.Now()
	s.mu.Unlock()

	if err := s.writeBatch(batch); err != nil {
		atomic.AddInt64(&s.errorCount, 1)
		monitoring.RecordBackendOperation("s3", "write", false)
		return err
	}

	atomic.AddInt64(&s.writeCount, int64(len(batch)))
	monitoring.RecordBackendOperation("s3", "write", true)
	return nil
}

// WriteBatch writes multiple events efficiently
func (s *S3Backend) WriteBatch(events []*core.LogEvent) error {
	if s.closed.Load() {
		return &BackendError{Backend: "s3", Op: "write_batch", Err: fmt.Errorf("backend closed")}
	}

	startTime := time.Now()
	defer func() {
		monitoring.RecordBackendLatency("s3", "write_batch", time.Since(startTime))
	}()

	if err := s.writeBatch(events); err != nil {
		atomic.AddInt64(&s.errorCount, int64(len(events)))
		monitoring.RecordBackendOperation("s3", "write_batch", false)
		return err
	}

	atomic.AddInt64(&s.writeCount, int64(len(events)))
	monitoring.RecordBackendOperation("s3", "write_batch", true)
	return nil
}

// writeBatch performs the actual batch write to S3
func (s *S3Backend) writeBatch(events []*core.LogEvent) error {
	if len(events) == 0 {
		return nil
	}

	// Serialize events
	var buffer bytes.Buffer

	// Optionally compress
	var writer io.Writer = &buffer
	var gzWriter *gzip.Writer
	if s.compress {
		gzWriter = gzip.NewWriter(&buffer)
		writer = gzWriter
	}

	encoder := json.NewEncoder(writer)
	for _, event := range events {
		if err := encoder.Encode(event); err != nil {
			return fmt.Errorf("failed to encode event: %w", err)
		}
	}

	if gzWriter != nil {
		if err := gzWriter.Close(); err != nil {
			return fmt.Errorf("failed to compress: %w", err)
		}
	}

	// Generate S3 key
	timestamp := time.Now().Format("2006/01/02/15/")
	filename := fmt.Sprintf("%s%09d.json", timestamp, time.Now().UnixNano())
	if s.compress {
		filename += ".gz"
	}

	key := path.Join(s.prefix, filename)

	// Prepare upload input
	input := &s3.PutObjectInput{
		Bucket:       aws.String(s.bucket),
		Key:          aws.String(key),
		Body:         bytes.NewReader(buffer.Bytes()),
		StorageClass: types.StorageClass(s.storageClass),
		ContentType:  aws.String("application/json"),
	}

	// Add encryption
	if s.encryption != "" {
		input.ServerSideEncryption = types.ServerSideEncryption(s.encryption)
		if s.kmsKeyID != "" && s.encryption == "aws:kms" {
			input.SSEKMSKeyId = aws.String(s.kmsKeyID)
		}
	}

	// Add Object Lock retention
	if s.objectLock && s.retentionDays > 0 {
		retainUntil := time.Now().AddDate(0, 0, s.retentionDays)
		input.ObjectLockMode = types.ObjectLockModeCompliance
		input.ObjectLockRetainUntilDate = aws.Time(retainUntil)
		input.ObjectLockLegalHoldStatus = types.ObjectLockLegalHoldStatusOff
	}

	// Add metadata
	input.Metadata = map[string]string{
		"EventCount": fmt.Sprintf("%d", len(events)),
		"FirstEvent": events[0].Timestamp.Format(time.RFC3339),
		"LastEvent":  events[len(events)-1].Timestamp.Format(time.RFC3339),
		"Compressed": fmt.Sprintf("%v", s.compress),
	}

	// Upload with retry
	_, err := s.uploadWithRetry(input, 3)
	if err != nil {
		return &BackendError{Backend: "s3", Op: "upload", Err: err}
	}

	return nil
}

// uploadWithRetry uploads with exponential backoff retry
func (s *S3Backend) uploadWithRetry(input *s3.PutObjectInput, maxRetries int) (*s3.PutObjectOutput, error) {
	var lastErr error
	ctx := context.Background()

	for attempt := 0; attempt < maxRetries; attempt++ {
		output, err := s.client.PutObject(ctx, input)
		if err == nil {
			monitoring.RecordRetry("s3_upload", true)
			return output, nil
		}

		lastErr = err

		// Check if error is retryable using smithy errors
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) {
			switch apiErr.ErrorCode() {
			case "NoSuchBucket":
				return nil, fmt.Errorf("bucket does not exist: %w", err)
			case "AccessDenied":
				return nil, fmt.Errorf("access denied: %w", err)
			}
		}

		// Exponential backoff
		if attempt < maxRetries-1 {
			delay := time.Duration(math.Pow(2, float64(attempt))) * time.Second
			time.Sleep(delay)
			monitoring.RecordRetry("s3_upload", false)
		}
	}

	return nil, fmt.Errorf("upload failed after %d attempts: %w", maxRetries, lastErr)
}

// Read reads events within a time range
func (s *S3Backend) Read(start, end time.Time) ([]*core.LogEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ctx := context.Background()

	// List objects in the time range
	prefix := s.generateTimeRangePrefix(start, end)

	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(prefix),
	}

	var events []*core.LogEvent
	paginator := s3.NewListObjectsV2Paginator(s.client, input)

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, &BackendError{Backend: "s3", Op: "list", Err: err}
		}

		for _, obj := range page.Contents {
			// Download and parse each object
			objEvents, err := s.downloadAndParse(*obj.Key)
			if err != nil {
				// Log error but continue
				atomic.AddInt64(&s.errorCount, 1)
				continue
			}

			// Filter by time range
			for _, event := range objEvents {
				if event.Timestamp.After(start) && event.Timestamp.Before(end) {
					events = append(events, event)
				}
			}
		}
	}

	return events, nil
}

// downloadAndParse downloads and parses an S3 object
func (s *S3Backend) downloadAndParse(key string) ([]*core.LogEvent, error) {
	ctx := context.Background()

	// Download object to buffer
	buffer := manager.NewWriteAtBuffer([]byte{})
	_, err := s.downloader.Download(ctx, buffer, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, err
	}

	reader := io.Reader(bytes.NewBuffer(buffer.Bytes()))

	// Decompress if needed
	if path.Ext(key) == ".gz" {
		gzReader, err := gzip.NewReader(bytes.NewBuffer(buffer.Bytes()))
		if err != nil {
			return nil, err
		}
		defer func() { _ = gzReader.Close() }()
		reader = gzReader
	}

	// Parse events
	var events []*core.LogEvent
	decoder := json.NewDecoder(reader)

	for {
		var event core.LogEvent
		if err := decoder.Decode(&event); err != nil {
			if err == io.EOF {
				break
			}
			// Skip corrupted records
			continue
		}
		events = append(events, &event)
	}

	return events, nil
}

// VerifyIntegrity verifies the integrity of S3 data
func (s *S3Backend) VerifyIntegrity() (*IntegrityReport, error) {
	ctx := context.Background()

	report := &IntegrityReport{
		Timestamp: time.Now(),
		Backend:   "s3",
		Valid:     true,
	}

	// List all objects
	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(s.prefix),
	}

	var totalSize int64
	var objectCount int64

	paginator := s3.NewListObjectsV2Paginator(s.client, input)

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, &BackendError{Backend: "s3", Op: "verify", Err: err}
		}

		for _, obj := range page.Contents {
			objectCount++
			totalSize += *obj.Size

			// Verify object metadata
			headInput := &s3.HeadObjectInput{
				Bucket: aws.String(s.bucket),
				Key:    obj.Key,
			}

			headOutput, err := s.client.HeadObject(ctx, headInput)
			if err != nil {
				report.Errors = append(report.Errors,
					fmt.Sprintf("Failed to head object %s: %v", *obj.Key, err))
				report.Valid = false
				continue
			}

			// Check encryption
			if s.encryption != "" && headOutput.ServerSideEncryption == "" {
				report.Errors = append(report.Errors,
					fmt.Sprintf("Object %s is not encrypted", *obj.Key))
				report.Valid = false
			}

			// Check Object Lock
			if s.objectLock && headOutput.ObjectLockMode == "" {
				report.Errors = append(report.Errors,
					fmt.Sprintf("Object %s does not have Object Lock", *obj.Key))
				report.Valid = false
			}

			report.VerifiedRecords++
		}
	}

	report.TotalRecords = objectCount

	// Update metrics
	monitoring.UpdateBackendSize("s3", totalSize)

	return report, nil
}

// Name returns the backend name
func (s *S3Backend) Name() string {
	return fmt.Sprintf("s3[%s/%s]", s.bucket, s.prefix)
}

// Close closes the backend
func (s *S3Backend) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Flush any pending batch
	if len(s.currentBatch) > 0 {
		if err := s.writeBatch(s.currentBatch); err != nil {
			return &BackendError{Backend: "s3", Op: "flush", Err: err}
		}
	}

	return nil
}

// verifyBucket verifies the bucket exists and is accessible
func (s *S3Backend) verifyBucket() error {
	ctx := context.Background()

	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(s.bucket),
	})

	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) {
			switch apiErr.ErrorCode() {
			case "NoSuchBucket", "NotFound":
				// Try to create the bucket
				return s.createBucket()
			}
		}
		return fmt.Errorf("bucket verification failed: %w", err)
	}

	return nil
}

// createBucket creates the S3 bucket
func (s *S3Backend) createBucket() error {
	ctx := context.Background()

	input := &s3.CreateBucketInput{
		Bucket: aws.String(s.bucket),
	}

	// Add location constraint for non-us-east-1 regions
	if s.region != "us-east-1" {
		input.CreateBucketConfiguration = &types.CreateBucketConfiguration{
			LocationConstraint: types.BucketLocationConstraint(s.region),
		}
	}

	// Enable Object Lock if requested
	if s.objectLock {
		input.ObjectLockEnabledForBucket = aws.Bool(true)
	}

	_, err := s.client.CreateBucket(ctx, input)
	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) {
			switch apiErr.ErrorCode() {
			case "BucketAlreadyExists", "BucketAlreadyOwnedByYou":
				return nil // Bucket already exists
			}
		}
		return fmt.Errorf("failed to create bucket: %w", err)
	}

	// Wait for bucket to be available using waiter
	waiter := s3.NewBucketExistsWaiter(s.client)
	return waiter.Wait(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(s.bucket),
	}, 2*time.Minute)
}

// enableVersioning enables versioning on the bucket
func (s *S3Backend) enableVersioning() error {
	ctx := context.Background()

	input := &s3.PutBucketVersioningInput{
		Bucket: aws.String(s.bucket),
		VersioningConfiguration: &types.VersioningConfiguration{
			Status: types.BucketVersioningStatusEnabled,
		},
	}

	_, err := s.client.PutBucketVersioning(ctx, input)
	return err
}

// configureObjectLock configures Object Lock on the bucket
func (s *S3Backend) configureObjectLock() error {
	if s.retentionDays <= 0 {
		return nil // No retention configured
	}

	ctx := context.Background()

	// Validate retention days before conversion
	const maxRetentionDays = 36500 // 100 years - reasonable upper bound
	if s.retentionDays > maxRetentionDays {
		return fmt.Errorf("retention days %d exceeds maximum %d", s.retentionDays, maxRetentionDays)
	}

	input := &s3.PutObjectLockConfigurationInput{
		Bucket: aws.String(s.bucket),
		ObjectLockConfiguration: &types.ObjectLockConfiguration{
			ObjectLockEnabled: types.ObjectLockEnabledEnabled,
			Rule: &types.ObjectLockRule{
				DefaultRetention: &types.DefaultRetention{
					Mode: types.ObjectLockRetentionModeCompliance,
					Days: aws.Int32(int32(s.retentionDays)),
				},
			},
		},
	}

	_, err := s.client.PutObjectLockConfiguration(ctx, input)
	return err
}

// generateTimeRangePrefix generates an S3 prefix for a time range
func (s *S3Backend) generateTimeRangePrefix(start, _ time.Time) string {
	// Use the start date for prefix
	prefix := path.Join(s.prefix, start.Format("2006/01/02/"))
	return prefix
}

// getS3Endpoint returns the S3 endpoint for testing (e.g., MinIO, LocalStack)
func getS3Endpoint() string {
	// Check environment variable first
	if endpoint := os.Getenv("S3_ENDPOINT"); endpoint != "" {
		return endpoint
	}
	if endpoint := os.Getenv("MINIO_ENDPOINT"); endpoint != "" {
		return endpoint
	}

	// Check for MinIO
	if isMinIORunning("http://localhost:9000") {
		return "http://localhost:9000"
	}

	// Check for LocalStack
	if isLocalStackRunning("http://localhost:4566") {
		return "http://localhost:4566"
	}

	return ""
}

// isMinIORunning checks if MinIO is running
func isMinIORunning(endpoint string) bool {
	if endpoint == "" {
		return false
	}

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(endpoint + "/minio/health/live")
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()

	return resp.StatusCode == 200
}

// isLocalStackRunning checks if LocalStack is running
func isLocalStackRunning(endpoint string) bool {
	if endpoint == "" {
		return false
	}

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(endpoint + "/_localstack/health")
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()

	return resp.StatusCode == 200
}

// GetStats returns backend statistics
func (s *S3Backend) GetStats() S3Stats {
	return S3Stats{
		WriteCount:    atomic.LoadInt64(&s.writeCount),
		ErrorCount:    atomic.LoadInt64(&s.errorCount),
		LastWrite:     s.lastWrite,
		Bucket:        s.bucket,
		Prefix:        s.prefix,
		ObjectLock:    s.objectLock,
		Versioning:    s.versioning,
		Encryption:    s.encryption != "",
		RetentionDays: s.retentionDays,
	}
}

// S3Stats contains S3 backend statistics
type S3Stats struct {
	LastWrite     time.Time
	Bucket        string
	Prefix        string
	WriteCount    int64
	ErrorCount    int64
	RetentionDays int
	ObjectLock    bool
	Versioning    bool
	Encryption    bool
}
