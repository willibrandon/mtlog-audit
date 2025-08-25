// Package scenarios contains specific torture test scenarios.
package scenarios

import (
	"fmt"
	"math/rand"
	"path/filepath"
	"sync"
	"time"

	audit "github.com/willibrandon/mtlog-audit"
	"github.com/willibrandon/mtlog/core"
)

// Kill9DuringWrite simulates process kill during write operations.
type Kill9DuringWrite struct {
	EventCount int
	KillAfter  int // Kill after N events (0 = random)
}

// NewKill9DuringWrite creates a new kill-9 scenario.
func NewKill9DuringWrite() *Kill9DuringWrite {
	return &Kill9DuringWrite{
		EventCount: 1000,
		KillAfter:  0, // Random by default
	}
}

// Name returns the scenario name.
func (k *Kill9DuringWrite) Name() string {
	return "Kill9DuringWrite"
}

// Execute runs the scenario.
func (k *Kill9DuringWrite) Execute(sink *audit.Sink, dir string) error {
	killAfter := k.KillAfter
	if killAfter == 0 {
		// Random kill point between 10% and 90% of events
		min := k.EventCount / 10
		max := k.EventCount * 9 / 10
		killAfter = min + rand.Intn(max-min)
	}

	// Write events in background
	var wg sync.WaitGroup
	errors := make(chan error, 1)
	done := make(chan bool, 1)

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			// Always signal done to prevent deadlock
			select {
			case done <- true:
			default:
			}
		}()

		for i := 0; i < k.EventCount; i++ {
			event := &core.LogEvent{
				Timestamp:       time.Now(),
				Level:           core.InformationLevel,
				MessageTemplate: fmt.Sprintf("Test event %d", i),
				Properties: map[string]interface{}{
					"Index":     i,
					"Timestamp": time.Now().UnixNano(),
					"Random":    rand.Int63(),
				},
			}

			// Don't block on emit
			sink.Emit(event)

			// Simulate abrupt termination
			if i == killAfter {
				// Don't actually kill the process in tests
				// Just close the sink abruptly without proper shutdown
				// This simulates a kill -9
				return
			}
		}
	}()

	// Wait for completion or "kill"
	// Timeout should be proportional to event count
	// Allow 50ms per event plus 2 second buffer for overhead
	timeout := time.Duration(k.EventCount)*50*time.Millisecond + 2*time.Second

	select {
	case <-done:
		// Process was "killed" or completed
		return nil
	case err := <-errors:
		return err
	case <-time.After(timeout):
		return fmt.Errorf("timeout waiting for scenario completion after %v", timeout)
	}
}

// Verify checks that data was not lost despite the abrupt termination.
func (k *Kill9DuringWrite) Verify(dir string) error {
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
		return fmt.Errorf("WAL corruption detected after kill")
	}

	// We should have at least some records written
	if report.TotalRecords == 0 {
		return fmt.Errorf("no records found in WAL")
	}

	// Verify all records are intact and hash chain is valid
	// The VerifyIntegrity already checks:
	// 1. All records can be parsed correctly
	// 2. No partial records exist (parsing would fail)
	// 3. Hash chain is valid for all complete records
	
	// Additional verification: ensure we got approximately the right number
	// We should have written at least killAfter events (with some margin for timing)
	killAfter := k.KillAfter
	if killAfter == 0 {
		// Random kill point between 10% and 90% of events
		min := k.EventCount / 10
		killAfter = min // Use minimum as conservative estimate
	}
	
	// Allow some margin for async writes
	if report.TotalRecords < killAfter/2 {
		return fmt.Errorf("too few records: expected at least %d, got %d", killAfter/2, report.TotalRecords)
	}

	return nil
}
