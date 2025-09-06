package scenarios

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	audit "github.com/willibrandon/mtlog-audit"
	"github.com/willibrandon/mtlog/core"
)

// DiskFull simulates disk full conditions during write operations.
type DiskFull struct {
	EventCount int
	FillAt     int // Fill disk after N events (0 = random)
}

// NewDiskFull creates a new disk full scenario.
func NewDiskFull() *DiskFull {
	return &DiskFull{
		EventCount: 50, // Fewer events since we're simulating disk full
		FillAt:     0,  // Random by default
	}
}

// Name returns the scenario name.
func (d *DiskFull) Name() string {
	return "DiskFull"
}

// Execute runs the scenario.
func (d *DiskFull) Execute(sink *audit.Sink, dir string) error {
	fillAt := d.FillAt
	if fillAt == 0 {
		// Random fill point between 20% and 80% of events
		min := d.EventCount / 5
		max := d.EventCount * 4 / 5
		if min >= max {
			min = max - 1
		}
		fillAt = min + int(time.Now().UnixNano() % int64(max-min+1))
	}

	// Write events and simulate disk full
	var wg sync.WaitGroup
	errors := make(chan error, 1)
	done := make(chan bool, 1)

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			select {
			case done <- true:
			default:
			}
		}()

		for i := 0; i < d.EventCount; i++ {
			// Fill the disk at the specified point
			if i == int(fillAt) {
				// Create files that actually fill up available disk space
				// When running in Docker, this will hit the volume/tmpfs limit
				// When running locally, this will fill available space minus buffer
				dummyFile := filepath.Join(dir, "disk-filler.tmp")
				if err := d.simulateDiskFull(dummyFile); err != nil {
					select {
					case errors <- fmt.Errorf("failed to fill disk: %w", err):
					default:
					}
					return
				}
			}

			event := &core.LogEvent{
				Timestamp:       time.Now(),
				Level:           core.InformationLevel,
				MessageTemplate: fmt.Sprintf("Test event %d", i),
				Properties: map[string]interface{}{
					"Index":     i,
					"Timestamp": time.Now().UnixNano(),
					"Random":    i * 7,
				},
			}

			// This might fail due to "disk full" simulation
			sink.Emit(event)
		}
	}()

	// Wait for completion with timeout
	timeout := time.Duration(d.EventCount)*20*time.Millisecond + 5*time.Second

	select {
	case <-done:
		wg.Wait()
		return nil
	case err := <-errors:
		wg.Wait()
		return err
	case <-time.After(timeout):
		return fmt.Errorf("timeout waiting for disk full scenario completion after %v", timeout)
	}
}

// Verify checks that the system handled disk full gracefully.
func (d *DiskFull) Verify(dir string) error {
	// Clean up the disk filler file first
	dummyFile := filepath.Join(dir, "disk-filler.tmp")
	os.Remove(dummyFile)

	// Reopen the WAL and verify we can read it
	sink, err := audit.New(
		audit.WithWAL(filepath.Join(dir, "test.wal")),
	)
	if err != nil {
		return fmt.Errorf("failed to reopen WAL: %w", err)
	}
	defer sink.Close()

	// Verify integrity
	report, err := sink.VerifyIntegrity()
	if err != nil {
		return fmt.Errorf("integrity check failed: %w", err)
	}

	if !report.Valid {
		return fmt.Errorf("WAL corruption detected after disk full")
	}

	// We should have some records written (before disk filled up)
	if report.TotalRecords == 0 {
		return fmt.Errorf("no records found in WAL")
	}

	// The system should have handled the disk full gracefully
	// and written at least some events before the disk filled
	return nil
}

// simulateDiskFull creates files to actually fill the available disk space
func (d *DiskFull) simulateDiskFull(filePath string) error {
	// Get available disk space using platform-specific implementation
	available, err := getAvailableDiskSpace(filepath.Dir(filePath))
	if err != nil {
		return fmt.Errorf("failed to get disk space: %w", err)
	}

	// Leave 1MB buffer to avoid completely filling the disk
	// This ensures the OS can still function
	buffer := uint64(1024 * 1024)
	if available <= buffer {
		// Already at or near capacity
		return nil
	}
	toWrite := available - buffer

	// Create the filler file
	file, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	// Write in chunks to fill the disk
	chunkSize := uint64(1024 * 1024) // 1MB chunks
	chunk := make([]byte, chunkSize)
	
	// Fill chunk with non-zero data to ensure it actually uses disk space
	// (some filesystems optimize away zero-filled files)
	for i := range chunk {
		chunk[i] = byte(i % 256)
	}

	written := uint64(0)
	for written < toWrite {
		// Calculate how much to write in this iteration
		remaining := toWrite - written
		writeSize := chunkSize
		if remaining < chunkSize {
			writeSize = remaining
		}

		n, err := file.Write(chunk[:writeSize])
		if err != nil {
			// We expect disk full errors at some point
			if isNoSpaceError(err) || os.IsNotExist(err) {
				return nil // Success - disk is full
			}
			return fmt.Errorf("write failed: %w", err)
		}
		written += uint64(n)

		// Sync periodically to ensure data hits disk
		if written%(10*chunkSize) == 0 {
			if err := file.Sync(); err != nil {
				if isNoSpaceError(err) {
					return nil // Success - disk is full
				}
				// Ignore sync errors, continue writing
			}
		}
	}

	// Final sync to ensure all data is on disk
	file.Sync()

	return nil
}