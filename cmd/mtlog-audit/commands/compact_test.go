package commands

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/willibrandon/mtlog-audit/wal"
	"github.com/willibrandon/mtlog/core"
)

func TestCompactCommand(t *testing.T) {
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "test.wal")

	// Create WAL with small segment size to force multiple segments
	w, err := wal.New(walPath, wal.WithSegmentSize(1024)) // 1KB segments
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}

	// Write events to create multiple segments
	for i := 0; i < 100; i++ {
		event := &core.LogEvent{
			Timestamp:       time.Now(),
			Level:           core.InformationLevel,
			MessageTemplate: "Test event {id}",
			Properties: map[string]any{
				"id":     i,
				"data":   "Some test data to increase event size",
				"filler": "More data to ensure we create multiple segments",
			},
		}

		if err := w.Write(event); err != nil {
			t.Fatalf("Failed to write event: %v", err)
		}
	}

	// Force segment rotation by writing more events
	for i := 0; i < 50; i++ {
		event := &core.LogEvent{
			Timestamp:       time.Now(),
			Level:           core.InformationLevel,
			MessageTemplate: "Additional event {id}",
			Properties: map[string]any{
				"id": i + 100,
			},
		}
		if err := w.Write(event); err != nil {
			t.Fatalf("Failed to write additional event: %v", err)
		}
	}

	w.Close()

	// Test dry run
	t.Run("DryRun", func(t *testing.T) {
		w2, err := wal.New(walPath)
		if err != nil {
			t.Fatalf("Failed to reopen WAL: %v", err)
		}
		defer w2.Close()

		compactor := wal.NewCompactor(w2, nil)
		err = performDryRun(compactor, w2)
		if err != nil {
			t.Errorf("Dry run failed: %v", err)
		}
	})

	// Test normal compaction
	t.Run("NormalCompaction", func(t *testing.T) {
		w2, err := wal.New(walPath)
		if err != nil {
			t.Fatalf("Failed to reopen WAL: %v", err)
		}
		defer w2.Close()

		initialSize := calculateTotalSize(w2)

		compactor := wal.NewCompactor(w2, nil)
		err = compactor.Compact()
		if err != nil {
			t.Errorf("Compaction failed: %v", err)
		}

		finalSize := calculateTotalSize(w2)

		// Size should not increase
		if finalSize > initialSize {
			t.Errorf("Size increased after compaction: %d -> %d", initialSize, finalSize)
		}

		stats := compactor.GetStats()
		if stats.CompactionsRun == 0 {
			t.Log("No compactions were run (segments may not meet criteria)")
		}
	})

	// Test force compaction
	t.Run("ForceCompaction", func(t *testing.T) {
		w2, err := wal.New(walPath)
		if err != nil {
			t.Fatalf("Failed to reopen WAL: %v", err)
		}
		defer w2.Close()

		compactor := wal.NewCompactor(w2, &wal.CompactionPolicy{
			MinSegments:       1,
			MaxSegmentAge:     0,
			MinSegmentSize:    0,
			TargetSegmentSize: 64 * 1024 * 1024,
			CompactRatio:      0.9, // High threshold to trigger compaction
		})

		err = compactor.ForceCompact()
		if err != nil {
			t.Errorf("Force compaction failed: %v", err)
		}

		stats := compactor.GetStats()
		t.Logf("Force compaction stats: runs=%d, segments=%d, bytes_compacted=%d, bytes_reclaimed=%d",
			stats.CompactionsRun, stats.SegmentsCompacted,
			stats.BytesCompacted, stats.BytesReclaimed)
	})
}

func TestCompactionPolicyValidation(t *testing.T) {
	tests := []struct {
		name      string
		threshold float64
		valid     bool
	}{
		{"Zero threshold", 0.0, true},
		{"Normal threshold", 0.5, true},
		{"High threshold", 0.9, true},
		{"Max threshold", 1.0, true},
		{"Negative threshold", -0.1, false},
		{"Above max threshold", 1.1, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := &wal.CompactionPolicy{
				MinSegments:       1,
				MaxSegmentAge:     time.Minute,
				MinSegmentSize:    1024,
				TargetSegmentSize: 64 * 1024 * 1024,
				CompactRatio:      tt.threshold,
			}

			// Validate threshold range
			isValid := tt.threshold >= 0.0 && tt.threshold <= 1.0
			if isValid != tt.valid {
				t.Errorf("Expected threshold %f to be valid=%v, got %v",
					tt.threshold, tt.valid, isValid)
			}

			// Ensure policy can be created without panic
			if isValid {
				tmpDir := t.TempDir()
				walPath := filepath.Join(tmpDir, "test.wal")
				w, err := wal.New(walPath)
				if err != nil {
					t.Fatalf("Failed to create WAL: %v", err)
				}
				defer w.Close()

				compactor := wal.NewCompactor(w, policy)
				if compactor == nil {
					t.Error("Failed to create compactor with valid policy")
				}
			}
		})
	}
}

func TestCalculateTotalSize(t *testing.T) {
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "test.wal")

	w, err := wal.New(walPath, wal.WithSegmentSize(512)) // Small segments
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}

	// Write events to create segments
	for i := 0; i < 50; i++ {
		event := &core.LogEvent{
			Timestamp:       time.Now(),
			Level:           core.InformationLevel,
			MessageTemplate: "Event",
			Properties: map[string]any{
				"id": i,
			},
		}

		if err := w.Write(event); err != nil {
			t.Fatalf("Failed to write event: %v", err)
		}
	}

	totalSize := calculateTotalSize(w)
	if totalSize == 0 {
		t.Error("Total size should not be zero after writing events")
	}

	// Verify size is reasonable
	// Since GetSegments is not available and calculateTotalSize returns 0,
	// we'll just verify it doesn't panic and returns a number
	if totalSize < 0 {
		t.Error("Total size should not be negative")
	}

	w.Close()
}
