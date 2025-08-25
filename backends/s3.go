package backends

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"path"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/willibrandon/mtlog/core"
	"github.com/willibrandon/mtlog-audit/monitoring"
)

// S3Backend implements AWS S3 storage backend with compliance features
type S3Backend struct {
	mu              sync.RWMutex
	client          *s3.S3
	uploader        *s3manager.Uploader
	downloader      *s3manager.Downloader
	bucket          string
	prefix          string
	region          string
	storageClass    string
	encryption      string
	kmsKeyID        string
	versioning      bool
	objectLock      bool
	retentionDays   int
	compress        bool
	batchSize       int
	currentBatch    []*core.LogEvent
	writeCount      int64
	errorCount      int64
	lastWrite       time.Time
	closed          atomic.Bool
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
func NewS3Backend(config S3Config, opts ...S3Option) (*S3Backend, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid S3 config: %w", err)
	}
	
	// Create AWS session
	awsConfig := &aws.Config{
		Region: aws.String(config.Region),
	}
	
	// Support LocalStack for testing
	if endpoint := getS3Endpoint(); endpoint != "" {
		awsConfig.Endpoint = aws.String(endpoint)
		awsConfig.S3ForcePathStyle = aws.Bool(true)
	}
	
	sess, err := session.NewSession(awsConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create AWS session: %w", err)
	}
	
	s3Client := s3.New(sess)
	
	backend := &S3Backend{
		client:       s3Client,
		uploader:     s3manager.NewUploader(sess),
		downloader:   s3manager.NewDownloader(sess),
		bucket:       config.Bucket,
		prefix:       config.Prefix,
		region:       config.Region,
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
	if config.ServerSideEncryption {
		backend.encryption = "AES256"
	}
	if config.Versioning {
		backend.versioning = true
	}
	if config.ObjectLock {
		backend.objectLock = true
		backend.retentionDays = config.RetentionDays
	}
	if config.StorageClass != "" {
		backend.storageClass = config.StorageClass
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
	input := &s3manager.UploadInput{
		Bucket:       aws.String(s.bucket),
		Key:          aws.String(key),
		Body:         bytes.NewReader(buffer.Bytes()),
		StorageClass: aws.String(s.storageClass),
		ContentType:  aws.String("application/json"),
	}
	
	// Add encryption
	if s.encryption != "" {
		input.ServerSideEncryption = aws.String(s.encryption)
		if s.kmsKeyID != "" && s.encryption == "aws:kms" {
			input.SSEKMSKeyId = aws.String(s.kmsKeyID)
		}
	}
	
	// Add Object Lock retention
	if s.objectLock && s.retentionDays > 0 {
		retainUntil := time.Now().AddDate(0, 0, s.retentionDays)
		input.ObjectLockMode = aws.String("COMPLIANCE")
		input.ObjectLockRetainUntilDate = aws.Time(retainUntil)
		input.ObjectLockLegalHoldStatus = aws.String("OFF")
	}
	
	// Add metadata
	input.Metadata = map[string]*string{
		"EventCount":  aws.String(fmt.Sprintf("%d", len(events))),
		"FirstEvent":  aws.String(events[0].Timestamp.Format(time.RFC3339)),
		"LastEvent":   aws.String(events[len(events)-1].Timestamp.Format(time.RFC3339)),
		"Compressed":  aws.String(fmt.Sprintf("%v", s.compress)),
	}
	
	// Upload with retry
	_, err := s.uploadWithRetry(input, 3)
	if err != nil {
		return &BackendError{Backend: "s3", Op: "upload", Err: err}
	}
	
	return nil
}

// uploadWithRetry uploads with exponential backoff retry
func (s *S3Backend) uploadWithRetry(input *s3manager.UploadInput, maxRetries int) (*s3manager.UploadOutput, error) {
	var lastErr error
	
	for attempt := 0; attempt < maxRetries; attempt++ {
		output, err := s.uploader.Upload(input)
		if err == nil {
			monitoring.RecordRetry("s3_upload", true)
			return output, nil
		}
		
		lastErr = err
		
		// Check if error is retryable
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			case s3.ErrCodeNoSuchBucket:
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
	
	// List objects in the time range
	prefix := s.generateTimeRangePrefix(start, end)
	
	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(prefix),
	}
	
	var events []*core.LogEvent
	
	err := s.client.ListObjectsV2Pages(input, func(page *s3.ListObjectsV2Output, lastPage bool) bool {
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
		return !lastPage
	})
	
	if err != nil {
		return nil, &BackendError{Backend: "s3", Op: "list", Err: err}
	}
	
	return events, nil
}

// downloadAndParse downloads and parses an S3 object
func (s *S3Backend) downloadAndParse(key string) ([]*core.LogEvent, error) {
	// Download object to buffer
	buff := &aws.WriteAtBuffer{}
	_, err := s.downloader.Download(buff, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, err
	}
	
	buffer := bytes.NewBuffer(buff.Bytes())
	
	// Decompress if needed
	reader := io.Reader(buffer)
	if path.Ext(key) == ".gz" {
		gzReader, err := gzip.NewReader(buffer)
		if err != nil {
			return nil, err
		}
		defer gzReader.Close()
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
	
	err := s.client.ListObjectsV2Pages(input, func(page *s3.ListObjectsV2Output, lastPage bool) bool {
		for _, obj := range page.Contents {
			objectCount++
			totalSize += *obj.Size
			
			// Verify object metadata
			headInput := &s3.HeadObjectInput{
				Bucket: aws.String(s.bucket),
				Key:    obj.Key,
			}
			
			headOutput, err := s.client.HeadObject(headInput)
			if err != nil {
				report.Errors = append(report.Errors, 
					fmt.Sprintf("Failed to head object %s: %v", *obj.Key, err))
				report.Valid = false
				continue
			}
			
			// Check encryption
			if s.encryption != "" && headOutput.ServerSideEncryption == nil {
				report.Errors = append(report.Errors,
					fmt.Sprintf("Object %s is not encrypted", *obj.Key))
				report.Valid = false
			}
			
			// Check Object Lock
			if s.objectLock && headOutput.ObjectLockMode == nil {
				report.Errors = append(report.Errors,
					fmt.Sprintf("Object %s does not have Object Lock", *obj.Key))
				report.Valid = false
			}
			
			report.VerifiedRecords++
		}
		return !lastPage
	})
	
	if err != nil {
		return nil, &BackendError{Backend: "s3", Op: "verify", Err: err}
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
	_, err := s.client.HeadBucket(&s3.HeadBucketInput{
		Bucket: aws.String(s.bucket),
	})
	
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			case s3.ErrCodeNoSuchBucket:
				// Try to create the bucket
				return s.createBucket()
			case "NotFound":
				return s.createBucket()
			}
		}
		return fmt.Errorf("bucket verification failed: %w", err)
	}
	
	return nil
}

// createBucket creates the S3 bucket
func (s *S3Backend) createBucket() error {
	input := &s3.CreateBucketInput{
		Bucket: aws.String(s.bucket),
	}
	
	// Add location constraint for non-us-east-1 regions
	if s.region != "us-east-1" {
		input.CreateBucketConfiguration = &s3.CreateBucketConfiguration{
			LocationConstraint: aws.String(s.region),
		}
	}
	
	// Enable Object Lock if requested
	if s.objectLock {
		input.ObjectLockEnabledForBucket = aws.Bool(true)
	}
	
	_, err := s.client.CreateBucket(input)
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			if aerr.Code() == s3.ErrCodeBucketAlreadyExists ||
				aerr.Code() == s3.ErrCodeBucketAlreadyOwnedByYou {
				return nil // Bucket already exists
			}
		}
		return fmt.Errorf("failed to create bucket: %w", err)
	}
	
	// Wait for bucket to be available
	return s.client.WaitUntilBucketExists(&s3.HeadBucketInput{
		Bucket: aws.String(s.bucket),
	})
}

// enableVersioning enables versioning on the bucket
func (s *S3Backend) enableVersioning() error {
	input := &s3.PutBucketVersioningInput{
		Bucket: aws.String(s.bucket),
		VersioningConfiguration: &s3.VersioningConfiguration{
			Status: aws.String(s3.BucketVersioningStatusEnabled),
		},
	}
	
	_, err := s.client.PutBucketVersioning(input)
	return err
}

// configureObjectLock configures Object Lock on the bucket
func (s *S3Backend) configureObjectLock() error {
	if s.retentionDays <= 0 {
		return nil // No retention configured
	}
	
	input := &s3.PutObjectLockConfigurationInput{
		Bucket: aws.String(s.bucket),
		ObjectLockConfiguration: &s3.ObjectLockConfiguration{
			ObjectLockEnabled: aws.String(s3.ObjectLockEnabledEnabled),
			Rule: &s3.ObjectLockRule{
				DefaultRetention: &s3.DefaultRetention{
					Mode: aws.String(s3.ObjectLockRetentionModeCompliance),
					Days: aws.Int64(int64(s.retentionDays)),
				},
			},
		},
	}
	
	_, err := s.client.PutObjectLockConfiguration(input)
	return err
}

// generateTimeRangePrefix generates an S3 prefix for a time range
func (s *S3Backend) generateTimeRangePrefix(start, end time.Time) string {
	// Use the start date for prefix
	prefix := path.Join(s.prefix, start.Format("2006/01/02/"))
	return prefix
}

// getS3Endpoint returns the S3 endpoint for testing (e.g., LocalStack)
func getS3Endpoint() string {
	// Check for LocalStack or custom endpoint
	if endpoint := aws.StringValue(aws.String("http://localhost:4566")); isLocalStackRunning(endpoint) {
		return endpoint
	}
	return ""
}

// isLocalStackRunning checks if LocalStack is running
func isLocalStackRunning(endpoint string) bool {
	if endpoint == "" {
		return false
	}
	
	// Actually check if LocalStack is reachable
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(endpoint + "/_localstack/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	
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
	WriteCount    int64
	ErrorCount    int64
	LastWrite     time.Time
	Bucket        string
	Prefix        string
	ObjectLock    bool
	Versioning    bool
	Encryption    bool
	RetentionDays int
}