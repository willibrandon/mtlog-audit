package backends

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/willibrandon/mtlog/core"
)

// FilesystemBackend implements filesystem-based storage with redundancy
type FilesystemBackend struct {
	mu          sync.RWMutex
	config      FilesystemConfig
	currentFile *os.File
	currentPath string
	currentSize int64
	rotateAt    time.Time
	writeCount  int64
	errorCount  int64
	syncTimer   *time.Timer
	shadowPath  string
	shadowFile  *os.File
	closed      atomic.Bool
}

// NewFilesystemBackend creates a new filesystem backend
func NewFilesystemBackend(config FilesystemConfig) (*FilesystemBackend, error) {
	// Validate and set defaults
	if err := config.Validate(); err != nil {
		return nil, err
	}

	// Create directories
	if err := os.MkdirAll(config.Path, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory %s: %w", config.Path, err)
	}

	backend := &FilesystemBackend{
		config:   config,
		rotateAt: time.Now().Add(config.MaxAge),
	}

	// Setup shadow copy if enabled
	if config.Shadow {
		shadowPath := config.Path + ".shadow"
		if err := os.MkdirAll(shadowPath, 0755); err != nil {
			return nil, fmt.Errorf("failed to create shadow directory %s: %w", shadowPath, err)
		}
		backend.shadowPath = shadowPath
	}

	// Open initial file
	if err := backend.rotate(); err != nil {
		return nil, fmt.Errorf("failed to open initial file: %w", err)
	}

	// Start sync timer if using interval mode
	if config.SyncMode == SyncInterval {
		backend.startSyncTimer()
	}

	return backend, nil
}

// Write writes an event to the filesystem
func (fb *FilesystemBackend) Write(event *core.LogEvent) error {
	if fb.closed.Load() {
		return &BackendError{Backend: "filesystem", Op: "write", Err: fmt.Errorf("backend closed")}
	}

	fb.mu.Lock()
	defer fb.mu.Unlock()

	// Check if rotation is needed
	if fb.shouldRotate() {
		if err := fb.rotate(); err != nil {
			atomic.AddInt64(&fb.errorCount, 1)
			return &BackendError{Backend: "filesystem", Op: "rotate", Err: err}
		}
	}

	// Serialize event
	data, err := json.Marshal(event)
	if err != nil {
		atomic.AddInt64(&fb.errorCount, 1)
		return &BackendError{Backend: "filesystem", Op: "marshal", Err: err}
	}

	// Add newline for line-delimited JSON
	data = append(data, '\n')

	// Write to primary file
	n, err := fb.currentFile.Write(data)
	if err != nil {
		atomic.AddInt64(&fb.errorCount, 1)
		return &BackendError{Backend: "filesystem", Op: "write", Err: err}
	}

	fb.currentSize += int64(n)
	atomic.AddInt64(&fb.writeCount, 1)

	// Write to shadow copy if enabled
	if fb.shadowFile != nil {
		if _, err := fb.shadowFile.Write(data); err != nil {
			// Log shadow write error but don't fail the operation
			atomic.AddInt64(&fb.errorCount, 1)
		}
	}

	// Sync based on mode
	if fb.config.SyncMode == SyncImmediate {
		if err := fb.sync(); err != nil {
			return &BackendError{Backend: "filesystem", Op: "sync", Err: err}
		}
	}

	return nil
}

// WriteBatch writes multiple events efficiently
func (fb *FilesystemBackend) WriteBatch(events []*core.LogEvent) error {
	if fb.closed.Load() {
		return &BackendError{Backend: "filesystem", Op: "write_batch", Err: fmt.Errorf("backend closed")}
	}

	fb.mu.Lock()
	defer fb.mu.Unlock()

	// Check if rotation is needed
	if fb.shouldRotate() {
		if err := fb.rotate(); err != nil {
			atomic.AddInt64(&fb.errorCount, 1)
			return &BackendError{Backend: "filesystem", Op: "rotate", Err: err}
		}
	}

	// Buffer all writes
	var buffer []byte
	for _, event := range events {
		data, err := json.Marshal(event)
		if err != nil {
			atomic.AddInt64(&fb.errorCount, 1)
			continue // Skip invalid events
		}
		buffer = append(buffer, data...)
		buffer = append(buffer, '\n')
	}

	// Write entire buffer
	n, err := fb.currentFile.Write(buffer)
	if err != nil {
		atomic.AddInt64(&fb.errorCount, int64(len(events)))
		return &BackendError{Backend: "filesystem", Op: "write_batch", Err: err}
	}

	fb.currentSize += int64(n)
	atomic.AddInt64(&fb.writeCount, int64(len(events)))

	// Write to shadow copy if enabled
	if fb.shadowFile != nil {
		if _, err := fb.shadowFile.Write(buffer); err != nil {
			atomic.AddInt64(&fb.errorCount, 1)
		}
	}

	// Sync after batch
	if fb.config.SyncMode == SyncBatch || fb.config.SyncMode == SyncImmediate {
		if err := fb.sync(); err != nil {
			return &BackendError{Backend: "filesystem", Op: "sync", Err: err}
		}
	}

	return nil
}

// Read reads events within a time range
func (fb *FilesystemBackend) Read(start, end time.Time) ([]*core.LogEvent, error) {
	fb.mu.RLock()
	defer fb.mu.RUnlock()

	var events []*core.LogEvent

	// Find relevant files
	files, err := fb.findFiles(start, end)
	if err != nil {
		return nil, &BackendError{Backend: "filesystem", Op: "find_files", Err: err}
	}

	// Read from each file
	for _, file := range files {
		fileEvents, err := fb.readFile(file, start, end)
		if err != nil {
			// Continue reading other files even if one fails
			atomic.AddInt64(&fb.errorCount, 1)
			continue
		}
		events = append(events, fileEvents...)
	}

	return events, nil
}

// VerifyIntegrity verifies the integrity of stored data
func (fb *FilesystemBackend) VerifyIntegrity() (*IntegrityReport, error) {
	fb.mu.RLock()
	defer fb.mu.RUnlock()

	report := &IntegrityReport{
		Timestamp: time.Now(),
		Backend:   "filesystem",
		Valid:     true,
	}

	// List all files
	files, err := filepath.Glob(filepath.Join(fb.config.Path, "*.json*"))
	if err != nil {
		return nil, &BackendError{Backend: "filesystem", Op: "list_files", Err: err}
	}

	// Verify each file
	for _, file := range files {
		fileReport, err := fb.verifyFile(file)
		if err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("%s: %v", file, err))
			report.Valid = false
			continue
		}

		report.TotalRecords += fileReport.TotalRecords
		report.VerifiedRecords += fileReport.VerifiedRecords
		report.CorruptedRecords += fileReport.CorruptedRecords

		if !fileReport.Valid {
			report.Valid = false
		}
	}

	// Verify shadow copies if enabled
	if fb.config.Shadow {
		shadowFiles, err := filepath.Glob(filepath.Join(fb.shadowPath, "*.json*"))
		if err == nil {
			if len(shadowFiles) != len(files) {
				report.Errors = append(report.Errors,
					fmt.Sprintf("shadow copy mismatch: %d files vs %d shadow files",
						len(files), len(shadowFiles)))
				report.Valid = false
			}
		}
	}

	return report, nil
}

// Name returns the backend name
func (fb *FilesystemBackend) Name() string {
	return fmt.Sprintf("filesystem[%s]", fb.config.Path)
}

// Close closes the backend
func (fb *FilesystemBackend) Close() error {
	if !fb.closed.CompareAndSwap(false, true) {
		return nil
	}

	fb.mu.Lock()
	defer fb.mu.Unlock()

	// Stop sync timer
	if fb.syncTimer != nil {
		fb.syncTimer.Stop()
	}

	// Final sync
	fb.sync()

	// Close files
	var errs []error

	if fb.currentFile != nil {
		if err := fb.currentFile.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close primary: %w", err))
		}
	}

	if fb.shadowFile != nil {
		if err := fb.shadowFile.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close shadow: %w", err))
		}
	}

	if len(errs) > 0 {
		return &BackendError{Backend: "filesystem", Op: "close", Err: fmt.Errorf("%v", errs)}
	}

	return nil
}

// shouldRotate checks if rotation is needed
func (fb *FilesystemBackend) shouldRotate() bool {
	return fb.currentSize >= fb.config.MaxSize || time.Now().After(fb.rotateAt)
}

// rotate rotates to a new file
func (fb *FilesystemBackend) rotate() error {
	// Close current file
	if fb.currentFile != nil {
		fb.currentFile.Sync()
		fb.currentFile.Close()

		// Compress if configured
		if fb.config.Compress {
			go fb.compressFile(fb.currentPath)
		}
	}

	if fb.shadowFile != nil {
		fb.shadowFile.Sync()
		fb.shadowFile.Close()

		if fb.config.Compress {
			shadowPath := filepath.Join(fb.shadowPath, filepath.Base(fb.currentPath))
			go fb.compressFile(shadowPath)
		}
	}

	// Generate new filename
	timestamp := time.Now().Format("20060102-150405")
	filename := fmt.Sprintf("audit-%s.json", timestamp)
	fb.currentPath = filepath.Join(fb.config.Path, filename)

	// Open new file with O_SYNC for durability
	file, err := os.OpenFile(fb.currentPath,
		os.O_CREATE|os.O_WRONLY|os.O_APPEND|os.O_SYNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to open file %s: %w", fb.currentPath, err)
	}

	fb.currentFile = file
	fb.currentSize = 0
	fb.rotateAt = time.Now().Add(fb.config.MaxAge)

	// Open shadow file if enabled
	if fb.config.Shadow {
		shadowFilePath := filepath.Join(fb.shadowPath, filename)
		shadowFile, err := os.OpenFile(shadowFilePath,
			os.O_CREATE|os.O_WRONLY|os.O_APPEND|os.O_SYNC, 0644)
		if err != nil {
			// Log error but continue - shadow is best effort
			atomic.AddInt64(&fb.errorCount, 1)
		} else {
			fb.shadowFile = shadowFile
		}
	}

	return nil
}

// sync flushes data to disk
func (fb *FilesystemBackend) sync() error {
	if fb.currentFile != nil {
		if err := fb.currentFile.Sync(); err != nil {
			return err
		}
	}

	if fb.shadowFile != nil {
		fb.shadowFile.Sync() // Best effort for shadow
	}

	return nil
}

// startSyncTimer starts the periodic sync timer
func (fb *FilesystemBackend) startSyncTimer() {
	fb.syncTimer = time.AfterFunc(5*time.Second, func() {
		fb.mu.Lock()
		fb.sync()
		fb.mu.Unlock()

		if !fb.closed.Load() {
			fb.startSyncTimer() // Reschedule
		}
	})
}

// compressFile compresses a file using gzip
func (fb *FilesystemBackend) compressFile(path string) error {
	// Open source file
	source, err := os.Open(path)
	if err != nil {
		return err
	}
	defer source.Close()

	// Create compressed file
	dest, err := os.Create(path + ".gz")
	if err != nil {
		return err
	}
	defer dest.Close()

	// Create gzip writer
	gz := gzip.NewWriter(dest)
	gz.Name = filepath.Base(path)
	gz.ModTime = time.Now()
	defer gz.Close()

	// Copy data
	if _, err := io.Copy(gz, source); err != nil {
		return err
	}

	// Remove original file after successful compression
	return os.Remove(path)
}

// findFiles finds files within a time range
func (fb *FilesystemBackend) findFiles(start, end time.Time) ([]string, error) {
	pattern := filepath.Join(fb.config.Path, "audit-*.json*")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}

	// Parse timestamps from filenames (audit-YYYYMMDD-HHMMSS.json format)
	var relevant []string
	for _, file := range files {
		// Extract timestamp from filename
		base := filepath.Base(file)
		// Remove "audit-" prefix and any extension
		if len(base) < 22 { // "audit-YYYYMMDD-HHMMSS" is 22 chars
			continue
		}

		// Extract timestamp portion
		timestampStr := base[6:21] // Skip "audit-" and get next 15 chars

		// Parse timestamp (YYYYMMDD-HHMMSS format)
		fileTime, err := time.Parse("20060102-150405", timestampStr)
		if err != nil {
			// Fall back to modification time if parsing fails
			stat, err := os.Stat(file)
			if err != nil {
				continue
			}
			fileTime = stat.ModTime()
		}

		// Check if file is within the time range
		// Files are created when they start, so we check if the file start time
		// is before the end of our range, and add files that might contain relevant events
		if fileTime.Before(end) || fileTime.Equal(end) {
			// File might contain events in our range
			relevant = append(relevant, file)
		}
	}

	return relevant, nil
}

// readFile reads events from a file
func (fb *FilesystemBackend) readFile(path string, start, end time.Time) ([]*core.LogEvent, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var reader io.Reader = file

	// Handle compressed files
	if filepath.Ext(path) == ".gz" {
		gz, err := gzip.NewReader(file)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		reader = gz
	}

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

		// Filter by time range
		if event.Timestamp.After(start) && event.Timestamp.Before(end) {
			events = append(events, &event)
		}
	}

	return events, nil
}

// verifyFile verifies a single file
func (fb *FilesystemBackend) verifyFile(path string) (*IntegrityReport, error) {
	report := &IntegrityReport{
		Timestamp: time.Now(),
		Backend:   "filesystem",
		Valid:     true,
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var reader io.Reader = file

	// Handle compressed files
	if filepath.Ext(path) == ".gz" {
		gz, err := gzip.NewReader(file)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		reader = gz
	}

	decoder := json.NewDecoder(reader)

	for {
		var event core.LogEvent
		if err := decoder.Decode(&event); err != nil {
			if err == io.EOF {
				break
			}
			report.CorruptedRecords++
			report.Valid = false
			continue
		}

		report.TotalRecords++

		// Basic validation
		if event.Timestamp.IsZero() {
			report.CorruptedRecords++
			report.Valid = false
		} else {
			report.VerifiedRecords++
		}
	}

	return report, nil
}
