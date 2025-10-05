package backends

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"cloud.google.com/go/storage"
	"github.com/willibrandon/mtlog/core"
	"google.golang.org/api/option"
)

// GCSBackend implements the Backend interface for Google Cloud Storage
type GCSBackend struct {
	config       GCSConfig
	client       *storage.Client
	bucket       *storage.BucketHandle
	mu           sync.Mutex
	buffer       []*core.LogEvent
	lastFlush    time.Time
	batchSize    int
	flushTicker  *time.Ticker
	stopChan     chan struct{}
	wg           sync.WaitGroup
	uploadedObjs map[string]string // object name -> MD5 hash for verification
}

// NewGCSBackend creates a new GCS backend
func NewGCSBackend(cfg GCSConfig) (*GCSBackend, error) {
	if cfg.ProjectID == "" {
		return nil, fmt.Errorf("GCS project ID is required")
	}
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("GCS bucket name is required")
	}

	// Create GCS client
	ctx := context.Background()
	var clientOpts []option.ClientOption
	if cfg.CredentialsFile != "" {
		clientOpts = append(clientOpts, option.WithCredentialsFile(cfg.CredentialsFile))
	}

	client, err := storage.NewClient(ctx, clientOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCS client: %w", err)
	}

	// Get bucket handle
	bucket := client.Bucket(cfg.Bucket)

	// Verify bucket exists
	_, err = bucket.Attrs(ctx)
	if err != nil {
		// Try to create bucket if it doesn't exist
		if err == storage.ErrBucketNotExist {
			if err := bucket.Create(ctx, cfg.ProjectID, &storage.BucketAttrs{
				Location:     cfg.Region,
				StorageClass: cfg.StorageClass,
			}); err != nil {
				client.Close()
				return nil, fmt.Errorf("failed to create bucket: %w", err)
			}
		} else {
			client.Close()
			return nil, fmt.Errorf("bucket verification failed: %w", err)
		}
	}

	gb := &GCSBackend{
		config:       cfg,
		client:       client,
		bucket:       bucket,
		buffer:       make([]*core.LogEvent, 0, 1000),
		lastFlush:    time.Now(),
		batchSize:    100,
		stopChan:     make(chan struct{}),
		uploadedObjs: make(map[string]string),
	}

	// Start background flush worker
	gb.flushTicker = time.NewTicker(30 * time.Second)
	gb.wg.Add(1)
	go gb.flushWorker()

	return gb, nil
}

// Write writes an event to GCS
func (gb *GCSBackend) Write(event *core.LogEvent) error {
	gb.mu.Lock()
	defer gb.mu.Unlock()

	gb.buffer = append(gb.buffer, event)

	// Flush if buffer is full
	if len(gb.buffer) >= gb.batchSize {
		return gb.flushLocked()
	}

	return nil
}

// WriteBatch writes multiple events to GCS
func (gb *GCSBackend) WriteBatch(events []*core.LogEvent) error {
	gb.mu.Lock()
	defer gb.mu.Unlock()

	gb.buffer = append(gb.buffer, events...)

	// Flush if buffer is getting large
	if len(gb.buffer) >= gb.batchSize {
		return gb.flushLocked()
	}

	return nil
}

// Read reads events from GCS (not implemented for audit logs)
func (gb *GCSBackend) Read(start, end time.Time) ([]*core.LogEvent, error) {
	// Audit logs are write-only for compliance
	return nil, fmt.Errorf("reading from audit backend is not supported")
}

// VerifyIntegrity verifies the integrity of stored data
func (gb *GCSBackend) VerifyIntegrity() (*IntegrityReport, error) {
	report := &IntegrityReport{
		Valid:        true,
		TotalRecords: 0,
		Errors:       make([]string, 0),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// List all objects and verify their MD5 hashes
	query := &storage.Query{
		Prefix: gb.config.Prefix,
	}

	it := gb.bucket.Objects(ctx, query)
	for {
		attrs, err := it.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to list objects: %w", err)
		}

		report.TotalRecords++

		// Verify MD5 if we have it recorded
		if storedHash, exists := gb.uploadedObjs[attrs.Name]; exists {
			objMD5 := base64.StdEncoding.EncodeToString(attrs.MD5)
			if objMD5 != storedHash {
				report.Errors = append(report.Errors, fmt.Sprintf("MD5 mismatch for %s: expected %s, got %s",
					attrs.Name, storedHash, objMD5))
				report.Valid = false
			}
		}

		// Check CRC32C checksum
		if attrs.CRC32C != 0 {
			// Verify CRC32C is present (GCS always calculates it)
			obj := gb.bucket.Object(attrs.Name)
			reader, err := obj.NewReader(ctx)
			if err != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("Failed to read object %s: %v", attrs.Name, err))
				report.Valid = false
				continue
			}

			// Verify checksums match
			if reader.Attrs.CRC32C != attrs.CRC32C {
				report.Errors = append(report.Errors, fmt.Sprintf("CRC32C mismatch for %s", attrs.Name))
				report.Valid = false
			}
			reader.Close()
		}

		// Check retention policy if configured
		if gb.config.RetentionDays > 0 && attrs.RetentionExpirationTime.IsZero() {
			report.Errors = append(report.Errors, fmt.Sprintf("Object %s missing retention policy", attrs.Name))
			report.Valid = false
		}
	}

	return report, nil
}

// Close closes the GCS backend
func (gb *GCSBackend) Close() error {
	// Stop flush worker
	close(gb.stopChan)
	gb.flushTicker.Stop()
	gb.wg.Wait()

	// Final flush
	gb.mu.Lock()
	err := gb.flushLocked()
	gb.mu.Unlock()

	// Close GCS client
	if gb.client != nil {
		if closeErr := gb.client.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}

	return err
}

// Name returns the backend name
func (gb *GCSBackend) Name() string {
	return "gcs"
}

// flushWorker periodically flushes the buffer
func (gb *GCSBackend) flushWorker() {
	defer gb.wg.Done()

	for {
		select {
		case <-gb.flushTicker.C:
			gb.mu.Lock()
			gb.flushLocked()
			gb.mu.Unlock()
		case <-gb.stopChan:
			return
		}
	}
}

// flushLocked flushes the buffer (must be called with lock held)
func (gb *GCSBackend) flushLocked() error {
	if len(gb.buffer) == 0 {
		return nil
	}

	// Generate object name with prefix if configured
	objectName := fmt.Sprintf("audit-%d.json.gz", time.Now().UnixNano())
	if gb.config.Prefix != "" {
		objectName = fmt.Sprintf("%s/%s", gb.config.Prefix, objectName)
	}

	// Compress the data
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	encoder := json.NewEncoder(gw)

	for _, event := range gb.buffer {
		if err := encoder.Encode(event); err != nil {
			return fmt.Errorf("failed to encode event: %w", err)
		}
	}

	if err := gw.Close(); err != nil {
		return fmt.Errorf("failed to compress data: %w", err)
	}

	// Calculate MD5 hash for integrity verification
	data := buf.Bytes()
	md5Hash := md5.Sum(data)
	md5String := base64.StdEncoding.EncodeToString(md5Hash[:])

	// Upload to Google Cloud Storage
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	obj := gb.bucket.Object(objectName)
	writer := obj.NewWriter(ctx)

	// Set object metadata
	writer.ContentType = "application/gzip"
	writer.ContentEncoding = "gzip"
	writer.MD5 = md5Hash[:]
	writer.Metadata = map[string]string{
		"audit":     "true",
		"timestamp": fmt.Sprintf("%d", time.Now().Unix()),
		"events":    fmt.Sprintf("%d", len(gb.buffer)),
	}

	// Set storage class if configured
	if gb.config.StorageClass != "" {
		writer.StorageClass = gb.config.StorageClass
	}

	// Write data
	if _, err := writer.Write(data); err != nil {
		writer.Close()
		return fmt.Errorf("failed to write to GCS: %w", err)
	}

	// Close writer to finalize upload
	if err := writer.Close(); err != nil {
		return fmt.Errorf("failed to finalize GCS upload: %w", err)
	}

	// Store MD5 for later verification
	gb.uploadedObjs[objectName] = md5String

	// Set retention policy if configured
	if gb.config.RetentionDays > 0 {
		// Update object metadata to set retention
		attrs, err := obj.Attrs(ctx)
		if err == nil {
			retentionTime := time.Now().Add(time.Duration(gb.config.RetentionDays) * 24 * time.Hour)
			_, err = obj.Update(ctx, storage.ObjectAttrsToUpdate{
				Metadata: map[string]string{
					"retention-until": retentionTime.Format(time.RFC3339),
				},
			})
			if err != nil {
				// Log but don't fail - retention might require special permissions
				fmt.Printf("Warning: Failed to set retention policy: %v\n", err)
			}
		}

		// If versioning is enabled, we can also set object lifecycle
		if gb.config.Versioning {
			// This would typically be set at the bucket level
			// during initial configuration
			_ = attrs // Use attrs if needed for versioning checks
		}
	}

	// Clear buffer
	gb.buffer = gb.buffer[:0]
	gb.lastFlush = time.Now()

	return nil
}
