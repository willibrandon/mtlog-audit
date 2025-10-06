// Package backends provides storage backend implementations for audit log data.
package backends

import (
	"bytes"
	"compress/gzip"
	"context"
	// #nosec G501 - MD5 used for checksums not security
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/Azure/azure-storage-blob-go/azblob"
	"github.com/willibrandon/mtlog/core"
)

// AzureBackend implements the Backend interface for Azure Blob Storage
type AzureBackend struct {
	containerURL  azblob.ContainerURL
	lastFlush     time.Time
	flushTicker   *time.Ticker
	stopChan      chan struct{}
	uploadedBlobs map[string]string
	buffer        []*core.LogEvent
	config        AzureConfig
	wg            sync.WaitGroup
	batchSize     int
	mu            sync.Mutex
}

// NewAzureBackend creates a new Azure backend
func NewAzureBackend(cfg AzureConfig) (*AzureBackend, error) {
	if cfg.ConnectionString == "" {
		return nil, fmt.Errorf("azure connection string is required")
	}
	if cfg.Container == "" {
		return nil, fmt.Errorf("azure container name is required")
	}

	// Parse connection string to extract account name and key
	accountName, accountKey, err := parseConnectionString(cfg.ConnectionString)
	if err != nil {
		return nil, fmt.Errorf("invalid connection string: %w", err)
	}

	// Create shared key credential
	credential, err := azblob.NewSharedKeyCredential(accountName, accountKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create credential: %w", err)
	}

	// Create pipeline
	pipeline := azblob.NewPipeline(credential, azblob.PipelineOptions{})

	// Create container URL
	u, _ := url.Parse(fmt.Sprintf("https://%s.blob.core.windows.net/%s", accountName, cfg.Container))
	containerURL := azblob.NewContainerURL(*u, pipeline)

	ab := &AzureBackend{
		config:        cfg,
		containerURL:  containerURL,
		buffer:        make([]*core.LogEvent, 0, 1000),
		lastFlush:     time.Now(),
		batchSize:     100,
		stopChan:      make(chan struct{}),
		uploadedBlobs: make(map[string]string),
	}

	// Verify container exists
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err = containerURL.GetProperties(ctx, azblob.LeaseAccessConditions{})
	if err != nil {
		// Try to create container if it doesn't exist
		_, createErr := containerURL.Create(ctx, azblob.Metadata{}, azblob.PublicAccessNone)
		if createErr != nil && !isAlreadyExistsError(createErr) {
			return nil, fmt.Errorf("container verification failed: %w", err)
		}
	}

	// Start background flush worker
	ab.flushTicker = time.NewTicker(30 * time.Second)
	ab.wg.Add(1)
	go ab.flushWorker()

	return ab, nil
}

// parseConnectionString extracts account name and key from connection string
func parseConnectionString(connStr string) (accountName, accountKey string, err error) {
	parts := bytes.Split([]byte(connStr), []byte(";"))
	for _, part := range parts {
		if bytes.HasPrefix(part, []byte("AccountName=")) {
			accountName = string(bytes.TrimPrefix(part, []byte("AccountName=")))
		} else if bytes.HasPrefix(part, []byte("AccountKey=")) {
			accountKey = string(bytes.TrimPrefix(part, []byte("AccountKey=")))
		}
	}

	if accountName == "" || accountKey == "" {
		return "", "", fmt.Errorf("connection string must contain AccountName and AccountKey")
	}

	return accountName, accountKey, nil
}

// isAlreadyExistsError checks if error is because container already exists
func isAlreadyExistsError(err error) bool {
	if err == nil {
		return false
	}
	// Azure returns 409 Conflict when container already exists
	return bytes.Contains([]byte(err.Error()), []byte("409")) ||
		bytes.Contains([]byte(err.Error()), []byte("already exists"))
}

// Write writes an event to Azure
func (ab *AzureBackend) Write(event *core.LogEvent) error {
	ab.mu.Lock()
	defer ab.mu.Unlock()

	ab.buffer = append(ab.buffer, event)

	// Flush if buffer is full
	if len(ab.buffer) >= ab.batchSize {
		return ab.flushLocked()
	}

	return nil
}

// WriteBatch writes multiple events to Azure
func (ab *AzureBackend) WriteBatch(events []*core.LogEvent) error {
	ab.mu.Lock()
	defer ab.mu.Unlock()

	ab.buffer = append(ab.buffer, events...)

	// Flush if buffer is getting large
	if len(ab.buffer) >= ab.batchSize {
		return ab.flushLocked()
	}

	return nil
}

// Read reads events from Azure (not implemented for audit logs)
func (ab *AzureBackend) Read(_, _ time.Time) ([]*core.LogEvent, error) {
	// Audit logs are write-only for compliance
	return nil, fmt.Errorf("reading from audit backend is not supported")
}

// VerifyIntegrity verifies the integrity of stored data
func (ab *AzureBackend) VerifyIntegrity() (*IntegrityReport, error) {
	report := &IntegrityReport{
		Valid:        true,
		TotalRecords: 0,
		Errors:       make([]string, 0),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// List all blobs and verify their MD5 hashes
	for marker := (azblob.Marker{}); marker.NotDone(); {
		listBlob, err := ab.containerURL.ListBlobsFlatSegment(ctx, marker, azblob.ListBlobsSegmentOptions{
			Prefix: ab.config.Prefix,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to list blobs: %w", err)
		}

		marker = listBlob.NextMarker

		for _, blobItem := range listBlob.Segment.BlobItems {
			report.TotalRecords++

			// Get blob properties to check MD5
			blobURL := ab.containerURL.NewBlockBlobURL(blobItem.Name)
			props, err := blobURL.GetProperties(ctx, azblob.BlobAccessConditions{}, azblob.ClientProvidedKeyOptions{})
			if err != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("Failed to get properties for %s: %v", blobItem.Name, err))
				report.Valid = false
				continue
			}

			// Verify MD5 if we have it recorded
			if storedHash, exists := ab.uploadedBlobs[blobItem.Name]; exists {
				blobMD5 := base64.StdEncoding.EncodeToString(props.ContentMD5())
				if blobMD5 != storedHash {
					report.Errors = append(report.Errors, fmt.Sprintf("MD5 mismatch for %s: expected %s, got %s",
						blobItem.Name, storedHash, blobMD5))
					report.Valid = false
				}
			}

			// Check if blob is in immutable state if configured
			if ab.config.Immutable && props.BlobCommittedBlockCount() == 0 {
				report.Errors = append(report.Errors, fmt.Sprintf("Blob %s is not in committed state", blobItem.Name))
				report.Valid = false
			}
		}
	}

	return report, nil
}

// Close closes the Azure backend
func (ab *AzureBackend) Close() error {
	// Stop flush worker
	close(ab.stopChan)
	ab.flushTicker.Stop()
	ab.wg.Wait()

	// Final flush
	ab.mu.Lock()
	defer ab.mu.Unlock()
	return ab.flushLocked()
}

// Name returns the backend name
func (ab *AzureBackend) Name() string {
	return "azure"
}

// flushWorker periodically flushes the buffer
func (ab *AzureBackend) flushWorker() {
	defer ab.wg.Done()

	for {
		select {
		case <-ab.flushTicker.C:
			ab.mu.Lock()
			if err := ab.flushLocked(); err != nil {
				// Log error but continue - this is a background flush
				_ = err
			}
			ab.mu.Unlock()
		case <-ab.stopChan:
			return
		}
	}
}

// flushLocked flushes the buffer (must be called with lock held)
func (ab *AzureBackend) flushLocked() error {
	if len(ab.buffer) == 0 {
		return nil
	}

	// Generate blob name with prefix if configured
	blobName := fmt.Sprintf("audit-%d.json.gz", time.Now().UnixNano())
	if ab.config.Prefix != "" {
		blobName = fmt.Sprintf("%s/%s", ab.config.Prefix, blobName)
	}

	// Compress the data
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	encoder := json.NewEncoder(gw)

	for _, event := range ab.buffer {
		if err := encoder.Encode(event); err != nil {
			return fmt.Errorf("failed to encode event: %w", err)
		}
	}

	if err := gw.Close(); err != nil {
		return fmt.Errorf("failed to compress data: %w", err)
	}

	// Calculate MD5 hash for integrity verification
	// #nosec G401 - MD5 used for integrity verification not cryptographic security
	data := buf.Bytes()
	// #nosec G401 - MD5 used for integrity verification not cryptographic security
	md5Hash := md5.Sum(data)
	md5String := base64.StdEncoding.EncodeToString(md5Hash[:])

	// Upload to Azure Blob Storage
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	blobURL := ab.containerURL.NewBlockBlobURL(blobName)

	// Set blob options
	options := azblob.UploadToBlockBlobOptions{
		BlobHTTPHeaders: azblob.BlobHTTPHeaders{
			ContentType:     "application/gzip",
			ContentMD5:      md5Hash[:],
			ContentEncoding: "gzip",
		},
		Metadata: azblob.Metadata{
			"audit":     "true",
			"timestamp": fmt.Sprintf("%d", time.Now().Unix()),
			"events":    fmt.Sprintf("%d", len(ab.buffer)),
		},
	}

	// Note: Access tier is set separately after upload if needed

	// Upload the blob
	_, err := azblob.UploadBufferToBlockBlob(ctx, data, blobURL, options)
	if err != nil {
		return fmt.Errorf("failed to upload blob: %w", err)
	}

	// Store MD5 for later verification
	ab.uploadedBlobs[blobName] = md5String

	// Set access tier if configured (after upload)
	if ab.config.AccessTier != "" {
		var tier azblob.AccessTierType
		switch ab.config.AccessTier {
		case "hot":
			tier = azblob.AccessTierHot
		case "cool":
			tier = azblob.AccessTierCool
		case "archive":
			tier = azblob.AccessTierArchive
		default:
			tier = azblob.AccessTierHot
		}

		_, err = blobURL.SetTier(ctx, tier, azblob.LeaseAccessConditions{}, azblob.RehydratePriorityNone)
		if err != nil {
			// Log but don't fail - tier setting might require special permissions
			fmt.Printf("Warning: Failed to set access tier: %v\n", err)
		}
	}

	// Note: Immutability policies require specific Azure configuration
	// and are typically set at the container level, not per blob
	if ab.config.Immutable && ab.config.RetentionDays > 0 {
		// Set metadata to indicate retention requirement
		metadata := azblob.Metadata{
			"retention-days": fmt.Sprintf("%d", ab.config.RetentionDays),
			"immutable":      "true",
		}
		_, err = blobURL.SetMetadata(ctx, metadata, azblob.BlobAccessConditions{}, azblob.ClientProvidedKeyOptions{})
		if err != nil {
			// Log but don't fail
			fmt.Printf("Warning: Failed to set retention metadata: %v\n", err)
		}
	}

	// Clear buffer
	ab.buffer = ab.buffer[:0]
	ab.lastFlush = time.Now()

	return nil
}
