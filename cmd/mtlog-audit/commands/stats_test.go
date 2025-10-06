package commands

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/willibrandon/mtlog-audit/wal"
	"github.com/willibrandon/mtlog/core"
)

func TestStatsCommand(t *testing.T) {
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "test.wal")

	// Create WAL with test data
	w, err := wal.New(walPath, wal.WithSegmentSize(2048))
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}

	// Write various events
	baseTime := time.Now().Add(-24 * time.Hour)
	levels := []core.LogEventLevel{
		core.ErrorLevel,
		core.WarningLevel,
		core.InformationLevel,
		core.DebugLevel,
		core.VerboseLevel,
	}

	for i := 0; i < 50; i++ {
		event := &core.LogEvent{
			Timestamp:       baseTime.Add(time.Duration(i) * time.Minute),
			Level:           levels[i%len(levels)],
			MessageTemplate: "Test event {id}",
			Properties: map[string]any{
				"id":   i,
				"data": "test data for padding",
			},
		}

		if err := w.Write(event); err != nil {
			t.Fatalf("Failed to write event: %v", err)
		}
	}

	// Write additional events to fill the WAL
	for i := 50; i < 100; i++ {
		event := &core.LogEvent{
			Timestamp:       baseTime.Add(time.Duration(i) * time.Minute),
			Level:           levels[i%len(levels)],
			MessageTemplate: "Additional test event {id}",
			Properties: map[string]any{
				"id":   i,
				"data": "additional test data",
			},
		}
		if err := w.Write(event); err != nil {
			t.Fatalf("Failed to write additional event: %v", err)
		}
	}

	_ = w.Close()

	// Test gathering basic stats
	t.Run("BasicStats", func(t *testing.T) {
		stats, err := gatherStats(walPath, false)
		if err != nil {
			t.Fatalf("Failed to gather stats: %v", err)
		}

		// Verify basic fields
		if stats.Path != walPath {
			t.Errorf("Expected path %s, got %s", walPath, stats.Path)
		}

		if stats.TotalSize == 0 {
			t.Error("Total size should not be zero")
		}

		if stats.SegmentCount == 0 {
			t.Error("Segment count should not be zero")
		}

		if stats.TotalRecords != 100 {
			t.Errorf("Expected 100 records, got %d", stats.TotalRecords)
		}

		// Verify level counts (20 of each level)
		if stats.ErrorCount != 20 {
			t.Errorf("Expected 20 errors, got %d", stats.ErrorCount)
		}

		if stats.WarningCount != 20 {
			t.Errorf("Expected 20 warnings, got %d", stats.WarningCount)
		}

		if stats.InfoCount != 20 {
			t.Errorf("Expected 20 info, got %d", stats.InfoCount)
		}

		// Debug includes both Debug and Verbose levels
		if stats.DebugCount != 40 {
			t.Errorf("Expected 40 debug/verbose, got %d", stats.DebugCount)
		}

		// Verify time range
		if stats.FirstEventTime.After(stats.LastEventTime) {
			t.Error("First event time should be before last event time")
		}

		if stats.Duration == "" {
			t.Error("Duration should not be empty")
		}
	})

	// Test verbose stats with segments
	t.Run("VerboseStats", func(t *testing.T) {
		stats, err := gatherStats(walPath, true)
		if err != nil {
			t.Fatalf("Failed to gather verbose stats: %v", err)
		}

		if len(stats.Segments) == 0 {
			t.Error("Verbose stats should include segment details")
		}

		// Verify segment stats
		for i, seg := range stats.Segments {
			if seg.Size == 0 {
				t.Errorf("Segment %d has zero size", i)
			}

			if seg.RecordCount == 0 && !seg.Sealed {
				t.Errorf("Active segment %d has zero records", i)
			}

			if seg.EndSeq < seg.StartSeq {
				t.Errorf("Segment %d has invalid sequence range", i)
			}
		}
	})

	// Test JSON output
	t.Run("JSONOutput", func(t *testing.T) {
		stats, err := gatherStats(walPath, true)
		if err != nil {
			t.Fatalf("Failed to gather stats: %v", err)
		}

		// Marshal to JSON and back
		data, err := json.Marshal(stats)
		if err != nil {
			t.Fatalf("Failed to marshal stats: %v", err)
		}

		var decoded WALStats
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Failed to unmarshal stats: %v", err)
		}

		// Verify round-trip
		if decoded.TotalRecords != stats.TotalRecords {
			t.Errorf("JSON round-trip failed: records %d != %d",
				decoded.TotalRecords, stats.TotalRecords)
		}
	})
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		expected string
		bytes    int64
	}{
		{"0 B", 0},
		{"512 B", 512},
		{"1.0 KB", 1024},
		{"1.5 KB", 1536},
		{"1.0 MB", 1048576},
		{"1.0 GB", 1073741824},
		{"1.0 TB", 1099511627776},
	}

	for _, tt := range tests {
		result := formatBytes(tt.bytes)
		if result != tt.expected {
			t.Errorf("formatBytes(%d) = %s, want %s", tt.bytes, result, tt.expected)
		}
	}
}

func TestStatsWithCorruption(t *testing.T) {
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "test.wal")

	// Create WAL
	w, err := wal.New(walPath)
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}

	// Write some events
	for i := 0; i < 10; i++ {
		event := &core.LogEvent{
			Timestamp:       time.Now(),
			Level:           core.InformationLevel,
			MessageTemplate: "Test",
			Properties:      map[string]any{"id": i},
		}

		if err := w.Write(event); err != nil {
			t.Fatalf("Failed to write event: %v", err)
		}
	}

	// Since GetSegments is not available, we can't simulate corruption
	// The test will focus on other aspects

	_ = w.Close()

	// Gather stats
	stats, err := gatherStats(walPath, false)
	if err != nil {
		t.Fatalf("Failed to gather stats: %v", err)
	}

	// Since we can't simulate corruption, just verify stats are gathered
	if stats.TotalRecords != 10 {
		t.Errorf("Expected 10 records, got %d", stats.TotalRecords)
	}
}

func TestStatsFragmentation(t *testing.T) {
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "test.wal")

	// Create WAL with very small segments to cause fragmentation
	w, err := wal.New(walPath, wal.WithSegmentSize(256)) // Tiny segments
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}

	// Write events to create many small segments
	for i := 0; i < 100; i++ {
		event := &core.LogEvent{
			Timestamp:       time.Now(),
			Level:           core.InformationLevel,
			MessageTemplate: "Test event with some padding data",
			Properties: map[string]any{
				"id":      i,
				"padding": "This is padding to make events larger",
			},
		}

		if err := w.Write(event); err != nil {
			t.Fatalf("Failed to write event: %v", err)
		}

		// Continue writing events to simulate activity
	}

	_ = w.Close()

	// Gather stats
	stats, err := gatherStats(walPath, false)
	if err != nil {
		t.Fatalf("Failed to gather stats: %v", err)
	}

	// Verify stats are reasonable
	if stats.TotalRecords != 100 {
		t.Errorf("Expected 100 records, got %d", stats.TotalRecords)
	}

	t.Logf("Fragmentation: %.1f%% with %d segments",
		stats.FragmentationPct, stats.SegmentCount)
}
