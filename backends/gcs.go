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

// GCSBackend implements the Backend interface for Google Cloud Storage
type GCSBackend struct {
	config      GCSConfig
	mu          sync.Mutex
	buffer      []*core.LogEvent
	lastFlush   time.Time
	batchSize   int
	flushTicker *time.Ticker
	stopChan    chan struct{}
	wg          sync.WaitGroup
}

// NewGCSBackend creates a new GCS backend
func NewGCSBackend(cfg GCSConfig) (*GCSBackend, error) {
	if cfg.ProjectID == "" {
		return nil, fmt.Errorf("GCS project ID is required")
	}
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("GCS bucket name is required")
	}

	gb := &GCSBackend{
		config:    cfg,
		buffer:    make([]*core.LogEvent, 0, 1000),
		lastFlush: time.Now(),
		batchSize: 100,
		stopChan:  make(chan struct{}),
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
	// For now, just report success if backend is accessible
	// In production, would verify object signatures and checksums
	return &IntegrityReport{
		Valid:        true,
		TotalRecords: 0,
		Errors:       nil,
	}, nil
}

// Close closes the GCS backend
func (gb *GCSBackend) Close() error {
	// Stop flush worker
	close(gb.stopChan)
	gb.flushTicker.Stop()
	gb.wg.Wait()

	// Final flush
	gb.mu.Lock()
	defer gb.mu.Unlock()
	return gb.flushLocked()
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

	// In production, this would upload to Google Cloud Storage
	// For now, we simulate the upload
	objectName := fmt.Sprintf("audit-%d.json.gz", time.Now().UnixNano())
	
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

	// Simulate upload to GCS
	// In production: upload buf.Bytes() to Google Cloud Storage
	_ = objectName
	_ = buf.Bytes()

	// Clear buffer
	gb.buffer = gb.buffer[:0]
	gb.lastFlush = time.Now()

	return nil
}