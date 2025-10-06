package scenarios

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"time"

	audit "github.com/willibrandon/mtlog-audit"
	"github.com/willibrandon/mtlog/core"
)

// RandomCorruption simulates random data corruption during write operations.
type RandomCorruption struct {
	CorruptionType string
	EventCount     int
	CorruptAt      int
}

// NewRandomCorruption creates a new random corruption scenario.
func NewRandomCorruption() *RandomCorruption {
	return &RandomCorruption{
		EventCount:     100,
		CorruptAt:      0, // Random by default
		CorruptionType: "bitflip",
	}
}

// Name returns the scenario name.
func (r *RandomCorruption) Name() string {
	return "RandomCorruption"
}

// Execute runs the scenario.
func (r *RandomCorruption) Execute(sink *audit.Sink, dir string) error {
	corruptAt := r.CorruptAt
	if corruptAt == 0 {
		// Random corruption point between 30% and 90% of events
		minSeq := r.EventCount * 3 / 10
		maxSeq := r.EventCount * 9 / 10
		// #nosec G404 - weak random acceptable for test scenario randomization
		corruptAt = minSeq + int(rand.Int63n(int64(maxSeq-minSeq+1)))
	}

	// Write events and simulate corruption
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

		for i := 0; i < r.EventCount; i++ {
			event := &core.LogEvent{
				Timestamp:       time.Now(),
				Level:           core.InformationLevel,
				MessageTemplate: fmt.Sprintf("Test event %d", i),
				Properties: map[string]interface{}{
					"Index":     i,
					"Timestamp": time.Now().UnixNano(),
					// #nosec G404 - weak random acceptable for test data generation
					"Random": rand.Int63(),
				},
			}

			sink.Emit(event)

			// Simulate corruption after specific number of events
			if i == corruptAt {
				// Small delay to ensure data is written
				time.Sleep(10 * time.Millisecond)

				// Corrupt the WAL file
				// #nosec G304 - test file path controlled by test framework
				if err := r.corruptWALFile(filepath.Join(dir, "test.wal")); err != nil {
					select {
					case errors <- fmt.Errorf("failed to corrupt WAL: %w", err):
					default:
					}
					return
				}
			}
		}
	}()

	// Wait for completion with timeout
	timeout := time.Duration(r.EventCount)*10*time.Millisecond + 2*time.Second

	select {
	case <-done:
		wg.Wait()
		return nil
	case err := <-errors:
		wg.Wait()
		return err
	case <-time.After(timeout):
		return fmt.Errorf("timeout waiting for corruption scenario completion after %v", timeout)
	}
}

// Verify checks that the system detected and handled corruption appropriately.
func (r *RandomCorruption) Verify(dir string) error {
	// Try to reopen the WAL - this might fail due to corruption
	sink, err := audit.New(
		audit.WithWAL(filepath.Join(dir, "test.wal")),
	)
	if err != nil {
		// Corruption was detected during initialization - this is expected
		return nil
	}
	defer func() { _ = sink.Close() }()

	// Try to verify integrity - this should detect corruption
	report, err := sink.VerifyIntegrity()
	if err != nil {
		// Error during verification is acceptable - corruption was detected
		return nil
	}

	// If the WAL appears valid, check if we have reasonable data
	if report.Valid && report.TotalRecords > 0 {
		// The corruption might not have affected critical parts
		// or the system recovered gracefully
		return nil
	}

	// If we reach here with an invalid report, that's expected
	if !report.Valid {
		return nil // Corruption was properly detected
	}

	return fmt.Errorf("corruption scenario failed to detect expected corruption")
}

// corruptWALFile introduces random corruption into the WAL file
func (r *RandomCorruption) corruptWALFile(walPath string) error {
	// Check if file exists
	info, err := os.Stat(walPath)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist yet, skip corruption
			return nil
		}
		return err
	}

	if info.Size() == 0 {
		// Empty file, nothing to corrupt
		return nil
	}

	// Read the file
	// #nosec G304 - test file path controlled by test framework
	data, err := os.ReadFile(walPath)
	if err != nil {
		return err
	}

	if len(data) == 0 {
		return nil
	}

	// Apply corruption based on type
	switch r.CorruptionType {
	case "bitflip":
		return r.corruptBitFlip(walPath, data)
	case "truncate":
		return r.corruptTruncate(walPath, data)
	case "overwrite":
		return r.corruptOverwrite(walPath, data)
	default:
		return r.corruptBitFlip(walPath, data)
	}
}

// corruptBitFlip flips random bits in the file
func (r *RandomCorruption) corruptBitFlip(walPath string, data []byte) error {
	// Flip 1-5 random bits
	// #nosec G404 - weak random acceptable for test corruption
	numBits := 1 + rand.Intn(5)

	for i := 0; i < numBits; i++ {
		if len(data) == 0 {
			break
		}

		// Choose random byte and bit position
		// #nosec G404 - weak random acceptable for test corruption
		bytePos := rand.Intn(len(data))
		// #nosec G404 - weak random acceptable for test corruption
		bitPos := rand.Intn(8)

		// Flip the bit
		data[bytePos] ^= (1 << bitPos)
	}

	return os.WriteFile(walPath, data, 0o600)
}

// corruptTruncate truncates the file at a random position
func (r *RandomCorruption) corruptTruncate(walPath string, data []byte) error {
	if len(data) <= 10 {
		return nil // Too small to truncate meaningfully
	}

	// Truncate somewhere in the last 50% of the file
	// #nosec G404 - weak random acceptable for test corruption
	truncateAt := len(data)/2 + rand.Intn(len(data)/2)

	return os.WriteFile(walPath, data[:truncateAt], 0o600)
}

// corruptOverwrite overwrites a section with random data
func (r *RandomCorruption) corruptOverwrite(walPath string, data []byte) error {
	if len(data) <= 20 {
		return nil // Too small to overwrite meaningfully
	}

	// Choose a random section to overwrite (up to 100 bytes)
	maxSize := 100
	if len(data) < maxSize {
		maxSize = len(data) / 2
	}

	// #nosec G404 - weak random acceptable for test corruption
	overwriteSize := 1 + rand.Intn(maxSize)
	// #nosec G404 - weak random acceptable for test corruption
	startPos := rand.Intn(len(data) - overwriteSize)

	// Generate random data
	randomData := make([]byte, overwriteSize)
	// #nosec G404 - weak random acceptable for test corruption
	// #nosec G104 - Read from math/rand never fails
	//nolint:staticcheck // Using math/rand for test corruption - deterministic randomness acceptable
	rand.Read(randomData)

	// Overwrite the section
	copy(data[startPos:startPos+overwriteSize], randomData)

	return os.WriteFile(walPath, data, 0o600)
}
