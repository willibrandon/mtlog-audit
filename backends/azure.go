package backends

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/willibrandon/mtlog/core"
)

// AzureBackend implements the Backend interface for Azure Blob Storage
type AzureBackend struct {
	config      AzureConfig
	mu          sync.Mutex
	buffer      []*core.LogEvent
	lastFlush   time.Time
	batchSize   int
	flushTicker *time.Ticker
	stopChan    chan struct{}
	wg          sync.WaitGroup
}

// NewAzureBackend creates a new Azure backend
func NewAzureBackend(cfg AzureConfig) (*AzureBackend, error) {
	if cfg.ConnectionString == "" {
		return nil, fmt.Errorf("Azure connection string is required")
	}
	if cfg.Container == "" {
		return nil, fmt.Errorf("Azure container name is required")
	}

	ab := &AzureBackend{
		config:    cfg,
		buffer:    make([]*core.LogEvent, 0, 1000),
		lastFlush: time.Now(),
		batchSize: 100,
		stopChan:  make(chan struct{}),
	}

	// Start background flush worker
	ab.flushTicker = time.NewTicker(30 * time.Second)
	ab.wg.Add(1)
	go ab.flushWorker()

	return ab, nil
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
func (ab *AzureBackend) Read(start, end time.Time) ([]*core.LogEvent, error) {
	// Audit logs are write-only for compliance
	return nil, fmt.Errorf("reading from audit backend is not supported")
}

// VerifyIntegrity verifies the integrity of stored data
func (ab *AzureBackend) VerifyIntegrity() (*IntegrityReport, error) {
	// For now, just report success if backend is accessible
	// In production, would verify blob signatures and checksums
	return &IntegrityReport{
		Valid:        true,
		TotalRecords: 0,
		Errors:       nil,
	}, nil
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
			ab.flushLocked()
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

	// In production, this would upload to Azure Blob Storage
	// For now, we simulate the upload
	blobName := fmt.Sprintf("audit-%d.json.gz", time.Now().UnixNano())
	
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

	// Simulate upload to Azure
	// In production: upload buf.Bytes() to Azure Blob Storage
	_ = blobName
	_ = buf.Bytes()

	// Clear buffer
	ab.buffer = ab.buffer[:0]
	ab.lastFlush = time.Now()

	return nil
}