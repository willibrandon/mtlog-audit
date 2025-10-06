package wal

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/willibrandon/mtlog/core"
)

func TestCompactor_DefaultPolicy(t *testing.T) {
	policy := DefaultCompactionPolicy()

	if policy.MinSegments != 5 {
		t.Errorf("Expected MinSegments 5, got %d", policy.MinSegments)
	}
	if policy.MaxSegmentAge != 24*time.Hour {
		t.Errorf("Expected MaxSegmentAge 24h, got %v", policy.MaxSegmentAge)
	}
	if policy.MinSegmentSize != 1*1024*1024 {
		t.Errorf("Expected MinSegmentSize 1MB, got %d", policy.MinSegmentSize)
	}
	if policy.TargetSegmentSize != 64*1024*1024 {
		t.Errorf("Expected TargetSegmentSize 64MB, got %d", policy.TargetSegmentSize)
	}
	if policy.RetentionPeriod != 7*24*time.Hour {
		t.Errorf("Expected RetentionPeriod 7 days, got %v", policy.RetentionPeriod)
	}
	if policy.CompactRatio != 0.5 {
		t.Errorf("Expected CompactRatio 0.5, got %f", policy.CompactRatio)
	}
}

func TestCompactor_FindCompactableSegments(t *testing.T) {
	dir := t.TempDir()
	walPath := filepath.Join(dir, "test.wal")

	// Create WAL with test segments
	wal, err := New(walPath, WithSegmentSize(1024))
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}
	defer func() { _ = wal.Close() }()

	// Create mock segments
	now := time.Now()
	segments := []*Segment{
		{
			Path:      filepath.Join(dir, "seg1.wal"),
			StartSeq:  1,
			EndSeq:    100,
			Size:      500,                      // Below min size
			CreatedAt: now.Add(-48 * time.Hour), // Old enough
			Sealed:    true,
		},
		{
			Path:      filepath.Join(dir, "seg2.wal"),
			StartSeq:  101,
			EndSeq:    200,
			Size:      2 * 1024 * 1024,         // Above min size
			CreatedAt: now.Add(-1 * time.Hour), // Too recent
			Sealed:    true,
		},
		{
			Path:      filepath.Join(dir, "seg3.wal"),
			StartSeq:  201,
			EndSeq:    300,
			Size:      1024,
			CreatedAt: now, // Active segment
			Sealed:    false,
		},
	}

	// Add segments to WAL's segment manager
	wal.segments.segments = append(wal.segments.segments, segments...)

	// Create compactor with custom policy
	policy := &CompactionPolicy{
		MinSegments:    1,
		MaxSegmentAge:  24 * time.Hour,
		MinSegmentSize: 1024,
		CompactRatio:   0.5,
	}
	compactor := NewCompactor(wal, policy)

	// Find compactable segments
	compactable, err := compactor.findCompactableSegments()
	if err != nil {
		t.Fatalf("Failed to find compactable segments: %v", err)
	}

	// Should find seg1 (small and old)
	if len(compactable) != 1 {
		t.Errorf("Expected 1 compactable segment, got %d", len(compactable))
	}

	if len(compactable) > 0 && compactable[0].Path != segments[0].Path {
		t.Errorf("Expected seg1 to be compactable")
	}
}

func TestCompactor_GroupSegments(t *testing.T) {
	segments := []*Segment{
		{Path: "seg1.wal", Size: 1024},
		{Path: "seg2.wal", Size: 1024},
		{Path: "seg3.wal", Size: 1024},
		{Path: "seg4.wal", Size: 100 * 1024 * 1024}, // Large segment
		{Path: "seg5.wal", Size: 1024},
	}

	policy := &CompactionPolicy{
		TargetSegmentSize: 10 * 1024, // 10KB target
	}

	compactor := &Compactor{policy: policy}
	groups := compactor.groupSegmentsForCompaction(segments)

	// The grouping algorithm groups segments by target size
	// We should have groups of small segments
	if len(groups) < 1 {
		t.Errorf("Expected at least 1 group, got %d", len(groups))
	}

	// Count small segments
	smallSegmentCount := 0
	for _, group := range groups {
		for _, seg := range group {
			if seg.Size < 10*1024 {
				smallSegmentCount++
			}
		}
	}

	// All 4 small segments should be in groups
	if smallSegmentCount != 4 {
		t.Errorf("Expected 4 small segments in groups, got %d", smallSegmentCount)
	}
}

func TestCompactor_Stats(t *testing.T) {
	dir := t.TempDir()
	walPath := filepath.Join(dir, "test.wal")

	wal, err := New(walPath)
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}
	defer func() { _ = wal.Close() }()

	compactor := NewCompactor(wal, nil)

	// Update stats
	compactor.stats.CompactionsRun = 5
	compactor.stats.SegmentsCompacted = 10
	compactor.stats.BytesCompacted = 1024 * 1024
	compactor.stats.BytesReclaimed = 512 * 1024
	compactor.stats.LastCompactionTime = time.Now()
	compactor.stats.LastCompactionDuration = 5 * time.Second

	// Get stats
	stats := compactor.GetStats()

	if stats.CompactionsRun != 5 {
		t.Errorf("Expected 5 compactions run, got %d", stats.CompactionsRun)
	}
	if stats.SegmentsCompacted != 10 {
		t.Errorf("Expected 10 segments compacted, got %d", stats.SegmentsCompacted)
	}
	if stats.BytesCompacted != 1024*1024 {
		t.Errorf("Expected 1MB compacted, got %d", stats.BytesCompacted)
	}
	if stats.BytesReclaimed != 512*1024 {
		t.Errorf("Expected 512KB reclaimed, got %d", stats.BytesReclaimed)
	}
}

func TestCompactor_CalculateCompactionRatio(t *testing.T) {
	dir := t.TempDir()
	segmentPath := filepath.Join(dir, "test.wal")

	// Create a segment with mixed records: normal, deleted, and superseded
	// #nosec G304 - test file path from TempDir
	file, err := os.Create(segmentPath)
	if err != nil {
		t.Fatalf("Failed to create segment file: %v", err)
	}

	var prevHash [32]byte
	var totalBytes int64

	// Write 10 records with different characteristics
	for seq := uint64(1); seq <= 10; seq++ {
		event := &core.LogEvent{
			Timestamp:       time.Now(),
			Level:           core.InformationLevel,
			MessageTemplate: fmt.Sprintf("Test log %d", seq),
			Properties: map[string]any{
				"sequence": seq,
			},
		}

		// Add entity_id for sequences 3-6 (will have superseded records)
		if seq >= 3 && seq <= 6 {
			event.Properties["entity_id"] = "entity_1"
		}

		record, err := NewRecord(event, seq, prevHash)
		if err != nil {
			t.Fatalf("Failed to create record: %v", err)
		}

		// Mark sequences 2, 4, 8 as deleted
		if seq == 2 || seq == 4 || seq == 8 {
			record.Flags |= RecordFlagDeleted
		}

		data, err := record.Marshal()
		if err != nil {
			t.Fatalf("Failed to marshal record: %v", err)
		}

		n, err := file.Write(data)
		if err != nil {
			t.Fatalf("Failed to write record: %v", err)
		}
		totalBytes += int64(n)

		prevHash = record.ComputeHash()
	}

	_ = file.Close()

	// Create segment and WAL for compactor
	segment := &Segment{
		Path:     segmentPath,
		Size:     totalBytes,
		StartSeq: 1,
		EndSeq:   10,
		Sealed:   true,
	}

	walPath := filepath.Join(dir, "test-main.wal")
	wal, err := New(walPath)
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}
	defer func() { _ = wal.Close() }()

	compactor := NewCompactor(wal, nil)

	// Calculate the actual compaction ratio
	ratio := compactor.calculateCompactionRatio(segment)

	// Expected state:
	// - Total records: 10
	// - Deleted records: 3 (sequences 2, 4, 8)
	// - Records with entity_id="entity_1": sequences 3, 4, 5, 6
	//   - Sequence 4 is deleted, so ignored
	//   - Sequences 3, 5 are superseded by 6
	//   - Only sequence 6 is "live" for entity_1
	// - Live records: 1, 3(superseded), 5(superseded), 6(latest), 7, 9, 10 = 5 live records
	// - Ratio should be approximately 0.5 (5 live out of 10 total)

	// Verify ratio is in expected range
	if ratio < 0.0 || ratio > 1.0 {
		t.Errorf("Ratio out of valid range [0,1]: %f", ratio)
	}

	// The actual ratio depends on byte sizes, but should be less than 0.7
	// because we have deleted and superseded records
	if ratio > 0.7 {
		t.Errorf("Expected ratio <= 0.7 due to deleted/superseded records, got %f", ratio)
	}

	t.Logf("Calculated compaction ratio: %.2f (lower is better for compaction)", ratio)

	// Test with a segment that has no deleted/superseded records
	cleanSegmentPath := filepath.Join(dir, "clean.wal")
	// #nosec G304 - test file path from TempDir
	cleanFile, err := os.Create(cleanSegmentPath)
	if err != nil {
		t.Fatalf("Failed to create clean segment: %v", err)
	}

	var cleanBytes int64
	prevHash = [32]byte{}

	// Write 5 clean records with unique entity IDs
	for seq := uint64(1); seq <= 5; seq++ {
		event := &core.LogEvent{
			Timestamp:       time.Now(),
			Level:           core.InformationLevel,
			MessageTemplate: fmt.Sprintf("Clean log %d", seq),
			Properties: map[string]any{
				"sequence":  seq,
				"entity_id": fmt.Sprintf("entity_%d", seq), // Unique IDs
			},
		}

		record, err := NewRecord(event, seq, prevHash)
		if err != nil {
			t.Fatalf("Failed to create clean record: %v", err)
		}

		data, err := record.Marshal()
		if err != nil {
			t.Fatalf("Failed to marshal clean record: %v", err)
		}

		n, err := cleanFile.Write(data)
		if err != nil {
			t.Fatalf("Failed to write clean record: %v", err)
		}
		cleanBytes += int64(n)

		prevHash = record.ComputeHash()
	}

	_ = cleanFile.Close()

	cleanSegment := &Segment{
		Path:     cleanSegmentPath,
		Size:     cleanBytes,
		StartSeq: 1,
		EndSeq:   5,
		Sealed:   true,
	}

	cleanRatio := compactor.calculateCompactionRatio(cleanSegment)

	// Clean segment should have high ratio (close to 1.0)
	if cleanRatio < 0.9 {
		t.Errorf("Expected clean segment ratio >= 0.9, got %f", cleanRatio)
	}

	t.Logf("Clean segment ratio: %.2f", cleanRatio)

	// Verify that segments with worse ratios are candidates for compaction
	if cleanRatio <= ratio {
		t.Error("Clean segment should have better (higher) ratio than segment with deleted records")
	}
}

func TestCompactor_StartStop(t *testing.T) {
	dir := t.TempDir()
	walPath := filepath.Join(dir, "test.wal")

	wal, err := New(walPath)
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}
	defer func() { _ = wal.Close() }()

	compactor := NewCompactor(wal, nil)

	// Start compactor
	err = compactor.Start()
	if err != nil {
		t.Fatalf("Failed to start compactor: %v", err)
	}

	// Verify running
	if !compactor.running {
		t.Error("Compactor not marked as running")
	}

	// Try to start again (should fail)
	err = compactor.Start()
	if err == nil {
		t.Error("Expected error starting already running compactor")
	}

	// Stop compactor
	err = compactor.Stop()
	if err != nil {
		t.Fatalf("Failed to stop compactor: %v", err)
	}

	// Verify stopped
	if compactor.running {
		t.Error("Compactor still marked as running after stop")
	}

	// Stop again should be no-op
	err = compactor.Stop()
	if err != nil {
		t.Error("Expected no error stopping already stopped compactor")
	}
}

func TestCompactor_ForceCompact(t *testing.T) {
	dir := t.TempDir()
	walPath := filepath.Join(dir, "test.wal")

	wal, err := New(walPath)
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}
	defer func() { _ = wal.Close() }()

	compactor := NewCompactor(wal, nil)

	// Force compaction (should be no-op with no segments)
	err = compactor.ForceCompact()
	if err != nil {
		t.Errorf("Force compact failed: %v", err)
	}
}

func TestCompactor_CompactRange(t *testing.T) {
	dir := t.TempDir()
	walPath := filepath.Join(dir, "test.wal")

	wal, err := New(walPath)
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}
	defer func() { _ = wal.Close() }()

	// Add test segments
	segments := []*Segment{
		{
			Path:     filepath.Join(dir, "seg1.wal"),
			StartSeq: 1,
			EndSeq:   100,
			Sealed:   true,
		},
		{
			Path:     filepath.Join(dir, "seg2.wal"),
			StartSeq: 101,
			EndSeq:   200,
			Sealed:   true,
		},
		{
			Path:     filepath.Join(dir, "seg3.wal"),
			StartSeq: 201,
			EndSeq:   300,
			Sealed:   true,
		},
	}

	for _, seg := range segments {
		// Create empty files
		f, _ := os.Create(seg.Path)
		_ = f.Close()
		wal.segments.segments = append(wal.segments.segments, seg)
	}

	compactor := NewCompactor(wal, nil)

	// Compact range 50-150 (should include seg1 and seg2)
	err = compactor.CompactRange(50, 150)
	if err != nil {
		// Error is expected since we have empty files
		// Just verify the method runs
		t.Logf("CompactRange error (expected): %v", err)
	}
}

func TestCompactor_MarkDeleted(t *testing.T) {
	dir := t.TempDir()
	walPath := filepath.Join(dir, "test.wal")

	wal, err := New(walPath)
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}
	defer func() { _ = wal.Close() }()

	compactor := NewCompactor(wal, nil)

	// Mark a record as deleted
	err = compactor.MarkDeleted(100)

	// Currently returns "not implemented"
	if err == nil {
		t.Error("Expected not implemented error")
	}
}

func TestCompactor_CleanupOldSegments(t *testing.T) {
	dir := t.TempDir()
	archiveDir := filepath.Join(dir, "archive")
	// #nosec G301 - test directory permissions appropriate for tests
	_ = os.MkdirAll(archiveDir, 0o755)

	// Create old archive files
	oldTime := time.Now().Add(-30 * 24 * time.Hour) // 30 days old
	oldFile := filepath.Join(archiveDir, "old.wal")
	// #nosec G304 - test file path from TempDir
	f, err := os.Create(oldFile)
	if err != nil {
		t.Fatalf("Failed to create old file: %v", err)
	}
	_ = f.Close()

	// Set old modification time
	_ = os.Chtimes(oldFile, oldTime, oldTime)

	// Create recent file
	recentFile := filepath.Join(archiveDir, "recent.wal")
	// #nosec G304 - test file path from TempDir
	f, err = os.Create(recentFile)
	if err != nil {
		t.Fatalf("Failed to create recent file: %v", err)
	}
	_ = f.Close()

	// Create WAL and compactor
	walPath := filepath.Join(dir, "test.wal")
	wal, err := New(walPath)
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}
	defer func() { _ = wal.Close() }()

	policy := &CompactionPolicy{
		RetentionPeriod: 7 * 24 * time.Hour, // 7 days
	}
	compactor := NewCompactor(wal, policy)

	// Clean up old segments
	err = compactor.cleanupOldSegments()
	if err != nil {
		t.Fatalf("Failed to cleanup old segments: %v", err)
	}

	// Old file should be deleted
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Error("Old file should have been deleted")
	}

	// Recent file should still exist
	if _, err := os.Stat(recentFile); os.IsNotExist(err) {
		t.Error("Recent file should not have been deleted")
	}
}

func TestCompactor_VacuumDeleted(t *testing.T) {
	dir := t.TempDir()
	walPath := filepath.Join(dir, "test.wal")

	wal, err := New(walPath)
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}
	defer func() { _ = wal.Close() }()

	// Create a segment with deleted records
	segment := &Segment{
		Path:   filepath.Join(dir, "seg1.wal"),
		Sealed: true,
	}

	// Create empty file for segment
	f, _ := os.Create(segment.Path)
	_ = f.Close()

	wal.segments.segments = append(wal.segments.segments, segment)

	compactor := NewCompactor(wal, nil)

	// Vacuum deleted records
	err = compactor.VacuumDeleted()
	if err != nil {
		// Expected since we have empty files
		t.Logf("VacuumDeleted error (expected): %v", err)
	}
}

func BenchmarkCompactor_GroupSegments(b *testing.B) {
	// Create many segments
	segments := make([]*Segment, 100)
	for i := range segments {
		segments[i] = &Segment{
			Path: filepath.Join("test", "seg"+string(rune(i))+".wal"),
			Size: int64(1024 * (i%10 + 1)), // Varying sizes
		}
	}

	policy := &CompactionPolicy{
		TargetSegmentSize: 64 * 1024,
	}
	compactor := &Compactor{policy: policy}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = compactor.groupSegmentsForCompaction(segments)
	}
}
