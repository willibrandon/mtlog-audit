package wal

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/willibrandon/mtlog/core"
)

// IntegrityReport contains the results of a WAL integrity check.
type IntegrityReport struct {
	Valid             bool
	TotalRecords      int
	CorruptedSegments int
	RecoveredRecords  int
	LastSequence      uint64
	LastTimestamp     time.Time
}

// WAL implements a Write-Ahead Log with guaranteed durability.
type WAL struct {
	mu          sync.Mutex
	path        string
	file        *os.File
	sequence    uint64
	lastHash    [32]byte
	segmentSize int64
	currentSize int64
	syncMode    SyncMode
	closed      atomic.Bool
	
	// Segment management
	segments    *SegmentManager
	
	// Buffering for group commit
	buffer      []byte
	flushTicker *time.Ticker
	flushStop   chan struct{}
	
	// Torn-write protection
	doubleWrite *DoubleWriteBuffer
	journalFile *os.File
}

// SyncMode defines when the WAL syncs to disk.
type SyncMode int

const (
	// SyncImmediate syncs after every write (safest, slowest)
	SyncImmediate SyncMode = iota
	// SyncInterval syncs periodically
	SyncInterval
	// SyncBatch syncs after a batch of writes
	SyncBatch
)

// Option configures the WAL.
type Option func(*config) error

type config struct {
	segmentSize   int64
	syncMode      SyncMode
	syncInterval  time.Duration
	bufferSize    int
	createDirPerm os.FileMode
}

// New creates a new WAL instance with guaranteed durability.
func New(path string, opts ...Option) (*WAL, error) {
	cfg := &config{
		segmentSize:   64 * 1024 * 1024, // 64MB default
		syncMode:      SyncImmediate,
		syncInterval:  100 * time.Millisecond,
		bufferSize:    4 * 1024 * 1024, // 4MB buffer
		createDirPerm: 0700,
	}

	for _, opt := range opts {
		if err := opt(cfg); err != nil {
			return nil, err
		}
	}

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, cfg.createDirPerm); err != nil {
		return nil, fmt.Errorf("failed to create WAL directory: %w", err)
	}

	// Initialize segment manager
	segments, err := NewSegmentManager(path, cfg.segmentSize)
	if err != nil {
		return nil, fmt.Errorf("failed to create segment manager: %w", err)
	}

	// Get the active segment path
	activePath := segments.GetActivePath()

	// Open or create WAL file
	// Note: We don't use O_SYNC as we do explicit syncing
	flags := os.O_CREATE | os.O_RDWR | os.O_APPEND

	file, err := os.OpenFile(activePath, flags, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to open WAL file: %w", err)
	}

	// Get file size
	stat, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to stat WAL file: %w", err)
	}

	// Initialize double-write buffer for torn-write protection
	journalPath := path + ".journal"
	journalFile, err := os.OpenFile(journalPath, os.O_CREATE|os.O_RDWR|os.O_SYNC, 0600)
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to open journal file: %w", err)
	}
	
	doubleWrite, err := NewDoubleWriteBuffer(journalFile, cfg.bufferSize)
	if err != nil {
		file.Close()
		journalFile.Close()
		return nil, fmt.Errorf("failed to create double-write buffer: %w", err)
	}

	w := &WAL{
		path:        path,
		file:        file,
		segments:    segments,
		segmentSize: cfg.segmentSize,
		currentSize: stat.Size(),
		syncMode:    cfg.syncMode,
		buffer:      make([]byte, 0, cfg.bufferSize),
		doubleWrite: doubleWrite,
		journalFile: journalFile,
	}

	// Recover from journal first (for torn-write protection)
	if err := w.recoverFromJournal(); err != nil {
		file.Close()
		journalFile.Close()
		return nil, fmt.Errorf("failed to recover from journal: %w", err)
	}
	
	// Recover from existing WAL if present
	if stat.Size() > 0 {
		if err := w.recover(); err != nil {
			file.Close()
			journalFile.Close()
			return nil, fmt.Errorf("failed to recover WAL: %w", err)
		}
	}

	// Start flush ticker for interval sync mode
	if cfg.syncMode == SyncInterval {
		w.flushStop = make(chan struct{})
		w.flushTicker = time.NewTicker(cfg.syncInterval)
		go w.flushLoop()
	}

	return w, nil
}

// Write appends a log event to the WAL with guaranteed durability.
func (w *WAL) Write(event *core.LogEvent) error {
	if w.closed.Load() {
		return fmt.Errorf("WAL is closed")
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	// Create record with sequence number and hash chain
	w.sequence++
	record, err := NewRecord(event, w.sequence, w.lastHash)
	if err != nil {
		return fmt.Errorf("failed to create record: %w", err)
	}

	// Marshal record
	data, err := record.Marshal()
	if err != nil {
		return fmt.Errorf("failed to marshal record: %w", err)
	}

	// Use double-write buffer for torn-write protection
	// 1. First write to journal with sync
	if err := w.doubleWrite.WriteToJournal(data, w.currentSize); err != nil {
		return fmt.Errorf("journal write failed: %w", err)
	}
	
	// 2. Then write to main WAL file
	n, err := w.file.Write(data)
	if err != nil {
		// Mark journal entry as incomplete
		w.doubleWrite.MarkIncomplete()
		return fmt.Errorf("write failed: %w", err)
	}
	if n != len(data) {
		// Mark journal entry as incomplete
		w.doubleWrite.MarkIncomplete()
		return fmt.Errorf("incomplete write: wrote %d of %d bytes", n, len(data))
	}
	
	// 3. Mark journal entry as complete
	if err := w.doubleWrite.MarkComplete(); err != nil {
		return fmt.Errorf("failed to mark journal complete: %w", err)
	}

	// Update state
	w.currentSize += int64(n)
	w.lastHash = record.ComputeHash()

	// Sync based on mode
	switch w.syncMode {
	case SyncImmediate:
		// For immediate mode, sync after every write
		if err := w.file.Sync(); err != nil {
			return fmt.Errorf("sync failed: %w", err)
		}
	case SyncBatch:
		// For batch mode, sync every 10 writes or on rotation
		if w.sequence%10 == 0 {
			if err := w.file.Sync(); err != nil {
				return fmt.Errorf("sync failed: %w", err)
			}
		}
	}

	// Check if rotation is needed
	if w.segments.ShouldRotate(w.currentSize) {
		if err := w.rotate(); err != nil {
			return fmt.Errorf("rotation failed: %w", err)
		}
	}

	return nil
}

// Flush forces any buffered data to disk.
func (w *WAL) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file == nil {
		return nil
	}

	return w.file.Sync()
}

// Close gracefully shuts down the WAL.
func (w *WAL) Close() error {
	if !w.closed.CompareAndSwap(false, true) {
		return nil
	}

	// Stop flush ticker if running
	if w.flushTicker != nil {
		w.flushTicker.Stop()
		close(w.flushStop)
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	var errs []error
	
	if w.file != nil {
		if err := w.file.Sync(); err != nil {
			errs = append(errs, fmt.Errorf("failed to sync WAL: %w", err))
		}
		if err := w.file.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close WAL: %w", err))
		}
	}
	
	if w.journalFile != nil {
		if err := w.journalFile.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close journal: %w", err))
		}
	}
	
	if len(errs) > 0 {
		return fmt.Errorf("close errors: %v", errs)
	}

	return nil
}

// VerifyIntegrity checks the integrity of the entire WAL.
func (w *WAL) VerifyIntegrity() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Read all records and verify integrity
	records, err := w.readAllRecords()
	if err != nil {
		return fmt.Errorf("failed to read records: %w", err)
	}

	// Empty WAL is valid
	if len(records) == 0 {
		return nil
	}

	// Verify hash chain
	var prevHash [32]byte
	for i, recordData := range records {
		record, err := UnmarshalRecord(recordData)
		if err != nil {
			return fmt.Errorf("failed to unmarshal record %d: %w", i, err)
		}

		// Verify hash chain (first record should have zero prev hash)
		if i > 0 && record.PrevHash != prevHash {
			return fmt.Errorf("hash chain broken at record %d", i)
		}

		prevHash = record.ComputeHash()
	}

	return nil
}

// VerifyIntegrityReport performs a detailed integrity check.
func (w *WAL) VerifyIntegrityReport() (*IntegrityReport, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	report := &IntegrityReport{
		Valid:        true,
		TotalRecords: 0,
	}

	// Read all records and verify integrity
	records, err := w.readAllRecords()
	if err != nil {
		report.Valid = false
		return report, fmt.Errorf("failed to read records: %w", err)
	}

	// Empty WAL is valid
	if len(records) == 0 {
		return report, nil
	}

	// Verify each record and hash chain
	var prevHash [32]byte
	var lastSeq uint64
	var lastTime time.Time

	for i, recordData := range records {
		record, err := UnmarshalRecord(recordData)
		if err != nil {
			report.Valid = false
			report.CorruptedSegments++
			continue
		}

		// Verify hash chain
		if i > 0 && record.PrevHash != prevHash {
			report.Valid = false
			report.CorruptedSegments++
		}

		report.TotalRecords++
		report.RecoveredRecords++
		lastSeq = record.Sequence
		lastTime = time.Unix(0, record.Timestamp)
		prevHash = record.ComputeHash()
	}

	report.LastSequence = lastSeq
	report.LastTimestamp = lastTime

	return report, nil
}

// Private methods

// readAllRecords reads all records from the WAL file
func (w *WAL) readAllRecords() ([][]byte, error) {
	// Save current position
	currentPos, err := w.file.Seek(0, 1)
	if err != nil {
		return nil, err
	}
	defer w.file.Seek(currentPos, 0)

	// Seek to beginning
	_, err = w.file.Seek(0, 0)
	if err != nil {
		return nil, err
	}

	// Read entire file
	stat, err := w.file.Stat()
	if err != nil {
		return nil, err
	}

	if stat.Size() == 0 {
		return nil, nil
	}

	data := make([]byte, stat.Size())
	_, err = io.ReadFull(w.file, data)
	if err != nil {
		return nil, err
	}

	// Parse records
	var records [][]byte
	offset := 0

	for offset < len(data) {
		// Check if we have enough data for header
		if offset+24 > len(data) { // 24 is minimum header size
			break
		}

		// Read magic number
		magic := binary.LittleEndian.Uint32(data[offset:])
		if magic != MagicHeader {
			break // End of valid records
		}

		// Read record length from header (offset 8, 4 bytes)
		length := binary.LittleEndian.Uint32(data[offset+8:offset+12])
		
		// Calculate total record size:
		// header(24) + sequence(8) + prevhash(32) + data(length) + crc(4) + footer(4)
		totalSize := 24 + 8 + 32 + int(length) + 4 + 4
		
		if offset+totalSize > len(data) {
			break // Incomplete record
		}

		// Extract complete record
		record := make([]byte, totalSize)
		copy(record, data[offset:offset+totalSize])
		records = append(records, record)
		offset += totalSize
	}

	return records, nil
}

func (w *WAL) recover() error {
	// Read all existing records to recover state
	records, err := w.readAllRecords()
	if err != nil {
		return fmt.Errorf("failed to read records during recovery: %w", err)
	}

	if len(records) > 0 {
		// Parse the last record to get sequence and hash
		lastRecord, err := UnmarshalRecord(records[len(records)-1])
		if err != nil {
			return fmt.Errorf("failed to parse last record: %w", err)
		}

		w.sequence = lastRecord.Sequence
		w.lastHash = lastRecord.ComputeHash()
	}

	// Seek to end for appending
	_, err = w.file.Seek(0, 2)
	return err
}

// recoverFromJournal recovers any incomplete writes from the journal
func (w *WAL) recoverFromJournal() error {
	// Check if journal has any incomplete writes
	incompleteWrites, err := w.doubleWrite.RecoverIncomplete()
	if err != nil {
		return fmt.Errorf("failed to recover from journal: %w", err)
	}
	
	// Apply any incomplete writes to the main WAL
	for _, write := range incompleteWrites {
		// Seek to the position where write should have occurred
		if _, err := w.file.Seek(write.Position, 0); err != nil {
			return fmt.Errorf("failed to seek to position %d: %w", write.Position, err)
		}
		
		// Write the data
		n, err := w.file.Write(write.Data)
		if err != nil {
			return fmt.Errorf("failed to replay journal write: %w", err)
		}
		if n != len(write.Data) {
			return fmt.Errorf("incomplete journal replay: wrote %d of %d bytes", n, len(write.Data))
		}
	}
	
	// Sync the file after recovery
	if len(incompleteWrites) > 0 {
		if err := w.file.Sync(); err != nil {
			return fmt.Errorf("failed to sync after journal recovery: %w", err)
		}
	}
	
	// Clear the journal after successful recovery
	return w.doubleWrite.Clear()
}

func (w *WAL) rotate() error {
	// Close current file
	if err := w.file.Close(); err != nil {
		return err
	}

	// Use segment manager to rotate
	newPath, err := w.segments.Rotate(w.sequence)
	if err != nil {
		return err
	}

	// Open new file
	flags := os.O_CREATE | os.O_RDWR | os.O_APPEND
	if w.syncMode == SyncImmediate {
		flags |= os.O_SYNC
	}

	file, err := os.OpenFile(newPath, flags, 0600)
	if err != nil {
		return err
	}

	w.file = file
	w.currentSize = 0

	return nil
}

func (w *WAL) flushLoop() {
	for {
		select {
		case <-w.flushTicker.C:
			w.Flush()
		case <-w.flushStop:
			return
		}
	}
}

// Option functions

// WithSegmentSize sets the maximum size of a WAL segment before rotation.
func WithSegmentSize(size int64) Option {
	return func(c *config) error {
		if size <= 0 {
			return fmt.Errorf("segment size must be positive")
		}
		c.segmentSize = size
		return nil
	}
}

// WithSyncMode sets when the WAL syncs to disk.
func WithSyncMode(mode SyncMode) Option {
	return func(c *config) error {
		c.syncMode = mode
		return nil
	}
}

// WithSyncInterval sets the sync interval for SyncInterval mode.
func WithSyncInterval(interval time.Duration) Option {
	return func(c *config) error {
		if interval <= 0 {
			return fmt.Errorf("sync interval must be positive")
		}
		c.syncInterval = interval
		return nil
	}
}

// GetSegments returns all segments managed by this WAL.
func (w *WAL) GetSegments() []*Segment {
	w.mu.Lock()
	defer w.mu.Unlock()
	
	// Update segment sizes before returning
	w.segments.UpdateSegmentSizes()
	return w.segments.GetSegments()
}