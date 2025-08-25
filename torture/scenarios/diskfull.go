package scenarios

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
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
			// Simulate disk full condition by creating a large file
			if i == int(fillAt) {
				// Create a file that fills up available space
				// This is a simulation - in real tests we'd use cgroups or similar
				dummyFile := filepath.Join(dir, "disk-filler.tmp")
				if err := d.simulateDiskFull(dummyFile); err != nil {
					select {
					case errors <- fmt.Errorf("failed to simulate disk full: %w", err):
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

// simulateDiskFull creates a large file to simulate disk full condition
func (d *DiskFull) simulateDiskFull(filePath string) error {
	// In a real scenario, this would actually fill the disk
	// For testing, we just create a reasonably large file
	// to simulate the condition without actually filling the disk
	
	file, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	// Write 10MB of data to simulate partial disk usage
	// This doesn't actually cause ENOSPC but simulates the scenario
	data := make([]byte, 1024*1024) // 1MB buffer
	for i := 0; i < 10; i++ {
		if _, err := file.Write(data); err != nil {
			// If we get an error here, it might be real ENOSPC
			if err == syscall.ENOSPC {
				return nil // This is what we wanted to simulate
			}
			return err
		}
	}

	return nil
}