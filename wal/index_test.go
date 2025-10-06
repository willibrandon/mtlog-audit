package wal

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/willibrandon/mtlog/core"
)

// Helper function to write real WAL records to a segment file
func writeTestRecords(t *testing.T, path string, startSeq, endSeq uint64) {
	// #nosec G304 - test file path from TempDir
	file, err := os.Create(path) // #nosec G304 - controlled path
	if err != nil {
		t.Fatalf("Failed to create segment file: %v", err)
	}
	defer func() { _ = file.Close() }()

	var prevHash [32]byte
	baseTime := time.Now().Add(-2 * time.Hour)

	for seq := startSeq; seq <= endSeq; seq++ {
		// Create a realistic log event
		event := &core.LogEvent{
			// #nosec G115 - test sequence index
			Timestamp:       baseTime.Add(time.Duration(seq) * time.Second),
			Level:           core.InformationLevel,
			MessageTemplate: fmt.Sprintf("Test log message %d", seq),
			Properties: map[string]any{
				"sequence": seq,
				"test":     true,
			},
		}

		// Create a proper WAL record
		record, err := NewRecord(event, seq, prevHash)
		if err != nil {
			t.Fatalf("Failed to create record: %v", err)
		}

		// Marshal and write the record
		data, err := record.Marshal()
		if err != nil {
			t.Fatalf("Failed to marshal record: %v", err)
		}

		n, err := file.Write(data)
		if err != nil {
			t.Fatalf("Failed to write record: %v", err)
		}
		if n != len(data) {
			t.Fatalf("Incomplete write: wrote %d of %d bytes", n, len(data))
		}

		// Update hash chain
		prevHash = record.ComputeHash()
	}

	// Sync to ensure data is on disk
	if err := file.Sync(); err != nil {
		t.Fatalf("Failed to sync file: %v", err)
	}
}

func TestIndex_Build(t *testing.T) {
	// Create temp directory
	dir := t.TempDir()

	// Create test segments with REAL WAL records
	segments := []*Segment{
		{
			Path:      filepath.Join(dir, "test-001.wal"),
			StartSeq:  1,
			EndSeq:    100,
			CreatedAt: time.Now().Add(-2 * time.Hour),
			Sealed:    true,
		},
		{
			Path:      filepath.Join(dir, "test-002.wal"),
			StartSeq:  101,
			EndSeq:    200,
			CreatedAt: time.Now().Add(-1 * time.Hour),
			Sealed:    true,
		},
	}

	// Write ACTUAL WAL records to the segments
	for _, seg := range segments {
		writeTestRecords(t, seg.Path, seg.StartSeq, seg.EndSeq)

		// Update size after writing
		stat, err := os.Stat(seg.Path)
		if err != nil {
			t.Fatalf("Failed to stat segment: %v", err)
		}
		seg.Size = stat.Size()
	}

	// Create index and build
	idx := NewIndex(filepath.Join(dir, "test"))
	err := idx.Build(segments)
	if err != nil {
		t.Fatalf("Failed to build index: %v", err)
	}

	// Verify segment info
	segInfo := idx.GetSegmentInfo()
	if len(segInfo) != 2 {
		t.Errorf("Expected 2 segments, got %d", len(segInfo))
	}

	// Verify ACTUAL sequence range from REAL records
	minSeq, maxSeq := idx.GetSequenceRange()
	if minSeq != 1 || maxSeq != 200 {
		t.Errorf("Expected sequence range 1-200, got %d-%d", minSeq, maxSeq)
	}

	// Verify we can find specific sequences
	entry, err := idx.FindBySequence(50)
	if err != nil {
		t.Errorf("Failed to find sequence 50: %v", err)
	}
	if entry.Sequence != 50 {
		t.Errorf("Found wrong sequence: expected 50, got %d", entry.Sequence)
	}

	// Verify sequence 150 is in second segment
	entry, err = idx.FindBySequence(150)
	if err != nil {
		t.Errorf("Failed to find sequence 150: %v", err)
	}
	if entry.Segment != segments[1].Path {
		t.Errorf("Sequence 150 should be in segment 2")
	}
}

func TestIndex_FindBySequence(t *testing.T) {
	dir := t.TempDir()
	idx := NewIndex(filepath.Join(dir, "test"))

	// Add test entries
	entries := []IndexEntry{
		{
			Sequence:  1,
			Segment:   "test-001.wal",
			Offset:    0,
			Size:      100,
			Timestamp: time.Now().Add(-1 * time.Hour),
			Checksum:  0x12345678,
		},
		{
			Sequence:  2,
			Segment:   "test-001.wal",
			Offset:    100,
			Size:      150,
			Timestamp: time.Now().Add(-30 * time.Minute),
			Checksum:  0x87654321,
		},
	}

	for _, entry := range entries {
		idx.AddEntry(entry)
	}

	// Test finding existing sequence
	found, err := idx.FindBySequence(1)
	if err != nil {
		t.Fatalf("Failed to find sequence 1: %v", err)
	}
	if found.Sequence != 1 {
		t.Errorf("Expected sequence 1, got %d", found.Sequence)
	}

	// Test finding non-existent sequence
	_, err = idx.FindBySequence(999)
	if err == nil {
		t.Error("Expected error for non-existent sequence")
	}
}

func TestIndex_FindByTimeRange(t *testing.T) {
	dir := t.TempDir()

	// Create a segment with REAL WAL records
	segmentPath := filepath.Join(dir, "test-001.wal")
	segment := &Segment{
		Path:      segmentPath,
		StartSeq:  1,
		EndSeq:    100,
		CreatedAt: time.Now().Add(-2 * time.Hour),
		Sealed:    true,
	}

	// Write REAL records with specific timestamps
	writeTestRecords(t, segmentPath, 1, 100)

	// Build index from the real segment
	idx := NewIndex(filepath.Join(dir, "test"))
	err := idx.Build([]*Segment{segment})
	if err != nil {
		t.Fatalf("Failed to build index: %v", err)
	}

	// The records are written with timestamps based on sequence
	// Each record is 1 second after the previous, starting 2 hours ago
	baseTime := time.Now().Add(-2 * time.Hour)

	// Find entries in a specific time range (sequences 30-60 should be 30-60 seconds after base)
	start := baseTime.Add(30 * time.Second)
	end := baseTime.Add(60 * time.Second)

	results, err := idx.FindByTimeRange(start, end)
	if err != nil {
		t.Fatalf("Failed to find entries in time range: %v", err)
	}

	// Should find approximately 31 records (30-60 inclusive)
	if len(results) < 20 || len(results) > 40 {
		t.Errorf("Expected ~31 entries in range, got %d", len(results))
	}

	// Verify all results are within the time range
	for _, entry := range results {
		if entry.Timestamp.Before(start) || entry.Timestamp.After(end) {
			t.Errorf("Entry timestamp %v outside range [%v, %v]",
				entry.Timestamp, start, end)
		}

		// Verify sequence numbers are in expected range (approximately 30-60)
		if entry.Sequence < 25 || entry.Sequence > 65 {
			t.Errorf("Unexpected sequence %d for time range", entry.Sequence)
		}
	}
}

func TestIndex_Persistence(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "test")

	// Create and populate index with REAL data
	idx1 := NewIndex(indexPath)
	segments := []*Segment{
		{
			Path:     filepath.Join(dir, "test-001.wal"),
			StartSeq: 1,
			EndSeq:   100,
		},
	}

	// Write REAL WAL records
	writeTestRecords(t, segments[0].Path, 1, 100)

	// Update size
	stat, err := os.Stat(segments[0].Path)
	if err != nil {
		t.Fatalf("Failed to stat segment: %v", err)
	}
	segments[0].Size = stat.Size()

	err = idx1.Build(segments)
	if err != nil {
		t.Fatalf("Failed to build index: %v", err)
	}

	// Verify index file was created
	indexFile := indexPath + ".idx"
	if _, err := os.Stat(indexFile); err != nil {
		t.Fatalf("Index file not created: %v", err)
	}

	// Load index in new instance
	idx2 := NewIndex(indexPath)
	err = idx2.Load()
	if err != nil {
		t.Fatalf("Failed to load index: %v", err)
	}

	// Verify loaded data matches
	if len(idx2.segments) != len(idx1.segments) {
		t.Errorf("Segment count mismatch: %d vs %d",
			len(idx2.segments), len(idx1.segments))
	}

	// Verify sequence range is preserved
	min1, max1 := idx1.GetSequenceRange()
	min2, max2 := idx2.GetSequenceRange()
	if min1 != min2 || max1 != max2 {
		t.Errorf("Sequence range mismatch: [%d-%d] vs [%d-%d]", min1, max1, min2, max2)
	}

	// Verify we can still find sequences after reload
	entry, err := idx2.FindBySequence(50)
	if err != nil {
		t.Errorf("Failed to find sequence 50 after reload: %v", err)
	}
	if entry.Sequence != 50 {
		t.Errorf("Found wrong sequence after reload: expected 50, got %d", entry.Sequence)
	}
}

func TestIndex_RemoveSegment(t *testing.T) {
	dir := t.TempDir()
	idx := NewIndex(filepath.Join(dir, "test"))

	// Add segments
	segments := []SegmentInfo{
		{
			Path:     "test-001.wal",
			StartSeq: 1,
			EndSeq:   100,
		},
		{
			Path:     "test-002.wal",
			StartSeq: 101,
			EndSeq:   200,
		},
	}
	idx.segments = segments

	// Add entries for both segments
	for i := uint64(1); i <= 200; i++ {
		segment := "test-001.wal"
		if i > 100 {
			segment = "test-002.wal"
		}
		idx.AddEntry(IndexEntry{
			Sequence: i,
			Segment:  segment,
		})
	}

	// Remove first segment
	idx.RemoveSegment("test-001.wal")

	// Verify segment removed
	if len(idx.segments) != 1 {
		t.Errorf("Expected 1 segment after removal, got %d", len(idx.segments))
	}

	// Verify entries removed
	_, err := idx.FindBySequence(50)
	if err == nil {
		t.Error("Expected error for removed sequence")
	}

	// Verify remaining entries intact
	_, err = idx.FindBySequence(150)
	if err != nil {
		t.Errorf("Failed to find remaining sequence: %v", err)
	}
}

func TestIndex_DeletedRecordHandling(t *testing.T) {
	dir := t.TempDir()

	// Create a segment with both normal and deleted records
	segmentPath := filepath.Join(dir, "test-001.wal")
	segment := &Segment{
		Path:      segmentPath,
		StartSeq:  1,
		EndSeq:    10,
		CreatedAt: time.Now().Add(-1 * time.Hour),
		Sealed:    true,
	}

	// Write records with different flags
	// #nosec G304 - test file path from TempDir
	file, err := os.Create(segmentPath)
	if err != nil {
		t.Fatalf("Failed to create segment file: %v", err)
	}
	defer func() { _ = file.Close() }()

	var prevHash [32]byte
	baseTime := time.Now().Add(-1 * time.Hour)

	for seq := uint64(1); seq <= 10; seq++ {
		event := &core.LogEvent{
			// #nosec G115 - test sequence index
			Timestamp:       baseTime.Add(time.Duration(seq) * time.Second),
			Level:           core.InformationLevel,
			MessageTemplate: fmt.Sprintf("Test log message %d", seq),
			Properties: map[string]any{
				"sequence": seq,
				"test":     true,
			},
		}

		record, err := NewRecord(event, seq, prevHash)
		if err != nil {
			t.Fatalf("Failed to create record: %v", err)
		}

		// Mark sequences 3, 5, 7 as deleted
		if seq == 3 || seq == 5 || seq == 7 {
			record.Flags |= RecordFlagDeleted
		}

		data, err := record.Marshal()
		if err != nil {
			t.Fatalf("Failed to marshal record: %v", err)
		}

		if _, err := file.Write(data); err != nil {
			t.Fatalf("Failed to write record: %v", err)
		}

		prevHash = record.ComputeHash()
	}

	if err := file.Sync(); err != nil {
		t.Fatalf("Failed to sync file: %v", err)
	}

	// Build index from the segment
	idx := NewIndex(filepath.Join(dir, "test"))
	err = idx.Build([]*Segment{segment})
	if err != nil {
		t.Fatalf("Failed to build index: %v", err)
	}

	// Test FindBySequence - should find deleted records
	entry, err := idx.FindBySequence(3)
	if err != nil {
		t.Errorf("FindBySequence failed for deleted record: %v", err)
	}
	if entry.Flags&RecordFlagDeleted == 0 {
		t.Error("Expected deleted flag to be set for sequence 3")
	}

	// Test FindBySequenceExcludeDeleted - should not find deleted records
	_, err = idx.FindBySequenceExcludeDeleted(3)
	if err == nil {
		t.Error("Expected error when finding deleted record with exclude flag")
	}

	// Should find non-deleted record
	entry, err = idx.FindBySequenceExcludeDeleted(4)
	if err != nil {
		t.Errorf("Failed to find non-deleted record: %v", err)
	}
	if entry.Sequence != 4 {
		t.Errorf("Expected sequence 4, got %d", entry.Sequence)
	}

	// Test time range queries
	start := baseTime
	end := baseTime.Add(11 * time.Second)

	// FindByTimeRange should include all records
	allResults, err := idx.FindByTimeRange(start, end)
	if err != nil {
		t.Fatalf("FindByTimeRange failed: %v", err)
	}
	if len(allResults) != 10 {
		t.Errorf("Expected 10 records (including deleted), got %d", len(allResults))
	}

	// FindByTimeRangeExcludeDeleted should exclude deleted records
	activeResults, err := idx.FindByTimeRangeExcludeDeleted(start, end)
	if err != nil {
		t.Fatalf("FindByTimeRangeExcludeDeleted failed: %v", err)
	}
	if len(activeResults) != 7 {
		t.Errorf("Expected 7 non-deleted records, got %d", len(activeResults))
	}

	// Verify no deleted records in active results
	for _, entry := range activeResults {
		if entry.Flags&RecordFlagDeleted != 0 {
			t.Errorf("Found deleted record %d in exclude-deleted results", entry.Sequence)
		}
	}

	// Verify specific sequences are excluded
	deletedSeqs := map[uint64]bool{3: true, 5: true, 7: true}
	for _, entry := range activeResults {
		if deletedSeqs[entry.Sequence] {
			t.Errorf("Deleted sequence %d should not be in results", entry.Sequence)
		}
	}
}

func TestIndex_VersionValidation(t *testing.T) {
	dir := t.TempDir()
	segmentPath := filepath.Join(dir, "test-001.wal")

	// Create a segment with an incompatible version
	// #nosec G304 - test file path from TempDir
	file, err := os.Create(segmentPath)
	if err != nil {
		t.Fatalf("Failed to create segment file: %v", err)
	}

	// Write a record with wrong version
	headerBuf := make([]byte, 24)
	binary.LittleEndian.PutUint32(headerBuf[0:4], MagicHeader)
	binary.LittleEndian.PutUint16(headerBuf[4:6], 999)  // Wrong version!
	binary.LittleEndian.PutUint16(headerBuf[6:8], 0)    // Flags
	binary.LittleEndian.PutUint32(headerBuf[8:12], 100) // Length
	// #nosec G115 - timestamp conversion for test
	binary.LittleEndian.PutUint64(headerBuf[12:20], uint64(time.Now().UnixNano()))
	binary.LittleEndian.PutUint32(headerBuf[20:24], 0x12345678) // CRC

	if _, err := file.Write(headerBuf); err != nil {
		t.Fatalf("Failed to write header: %v", err)
	}

	// Write sequence and some data
	_ = binary.Write(file, binary.LittleEndian, uint64(1))
	_, _ = file.Write(make([]byte, 32))  // prev hash
	_, _ = file.Write(make([]byte, 100)) // data
	_, _ = file.Write(make([]byte, 8))   // crc + footer

	_ = file.Close()

	// Try to build index with incompatible version
	segment := &Segment{
		Path:      segmentPath,
		StartSeq:  1,
		EndSeq:    1,
		CreatedAt: time.Now(),
		Sealed:    true,
	}

	idx := NewIndex(filepath.Join(dir, "test"))
	err = idx.Build([]*Segment{segment})
	if err != nil {
		t.Fatalf("Failed to build index: %v", err)
	}

	// Check that the segment is marked as corrupted due to version mismatch
	segInfo := idx.GetSegmentInfo()
	if len(segInfo) != 1 {
		t.Fatalf("Expected 1 segment, got %d", len(segInfo))
	}

	if !segInfo[0].Corrupted {
		t.Error("Expected segment to be marked as corrupted due to version mismatch")
	}

	if segInfo[0].RecordCount != 0 {
		t.Errorf("Expected 0 records due to version mismatch, got %d", segInfo[0].RecordCount)
	}
}

func BenchmarkIndex_FindBySequence(b *testing.B) {
	dir := b.TempDir()
	idx := NewIndex(filepath.Join(dir, "test"))

	// Add many entries
	for i := uint64(1); i <= 10000; i++ {
		idx.AddEntry(IndexEntry{
			Sequence: i,
			Segment:  "test.wal",
			// #nosec G115 - test offset
			Offset: int64(i * 100),
		})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// #nosec G115 - test sequence
		seq := uint64(i%10000) + 1
		_, _ = idx.FindBySequence(seq)
	}
}
