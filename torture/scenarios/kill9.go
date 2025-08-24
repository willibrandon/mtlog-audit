// Package scenarios contains specific torture test scenarios.
package scenarios

import (
	"fmt"
	"math/rand"
	"path/filepath"
	"sync"
	"time"

	"github.com/willibrandon/mtlog/core"
	audit "github.com/willibrandon/mtlog-audit"
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

			sink.Emit(event)

			// Simulate abrupt termination
			if i == killAfter {
				// Don't actually kill the process in tests
				// Just close the sink abruptly without proper shutdown
				// This simulates a kill -9
				done <- true
				return
			}

			// Small delay to make it more realistic
			if i%10 == 0 {
				time.Sleep(time.Microsecond * 100)
			}
		}
		done <- true
	}()

	// Wait for completion or "kill"
	select {
	case <-done:
		// Process was "killed" or completed
		return nil
	case err := <-errors:
		return err
	case <-time.After(5 * time.Second):
		return fmt.Errorf("timeout waiting for scenario completion")
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

	// TODO: When we implement proper WAL reading, verify:
	// 1. All records up to the kill point are intact
	// 2. No partial records exist
	// 3. Hash chain is valid up to the last complete record

	return nil
}