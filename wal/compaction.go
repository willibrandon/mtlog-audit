package wal

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/willibrandon/mtlog/core"
)

// CompactionPolicy defines when and how to compact segments
type CompactionPolicy struct {
	// MinSegments is the minimum number of segments to trigger compaction
	MinSegments int
	// MaxSegmentAge is the maximum age before a segment is compacted
	MaxSegmentAge time.Duration
	// MinSegmentSize is the minimum size for a segment to be considered for compaction
	MinSegmentSize int64
	// TargetSegmentSize is the target size for compacted segments
	TargetSegmentSize int64
	// RetentionPeriod is how long to keep segments before deletion
	RetentionPeriod time.Duration
	// CompactRatio is the minimum ratio of live/total data to trigger compaction
	CompactRatio float64
}

// DefaultCompactionPolicy returns a sensible default compaction policy
func DefaultCompactionPolicy() *CompactionPolicy {
	return &CompactionPolicy{
		MinSegments:       5,
		MaxSegmentAge:     24 * time.Hour,
		MinSegmentSize:    1 * 1024 * 1024,  // 1MB
		TargetSegmentSize: 64 * 1024 * 1024, // 64MB
		RetentionPeriod:   7 * 24 * time.Hour,
		CompactRatio:      0.5,
	}
}

// Compactor handles segment compaction for the WAL
type Compactor struct {
	mu            sync.RWMutex
	wal           *WAL
	policy        *CompactionPolicy
	running       bool
	stopChan      chan struct{}
	lastCompacted time.Time
	stats         CompactionStats
}

// CompactionStats tracks compaction statistics
type CompactionStats struct {
	CompactionsRun         int
	SegmentsCompacted      int
	BytesCompacted         int64
	BytesReclaimed         int64
	LastCompactionTime     time.Time
	LastCompactionDuration time.Duration
	Errors                 []error

	// Analysis metrics for monitoring
	LastAnalyzedSegment    string
	LastAnalyzedRatio      float64
	LastAnalyzedDeleted    int
	LastAnalyzedSuperseded int
	LastAnalyzedTime       time.Time
}

// NewCompactor creates a new segment compactor
func NewCompactor(wal *WAL, policy *CompactionPolicy) *Compactor {
	if policy == nil {
		policy = DefaultCompactionPolicy()
	}

	return &Compactor{
		wal:      wal,
		policy:   policy,
		stopChan: make(chan struct{}),
	}
}

// Start begins the background compaction process
func (c *Compactor) Start() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.running {
		return fmt.Errorf("compactor already running")
	}

	c.running = true
	go c.runCompactionLoop()

	return nil
}

// Stop halts the background compaction process
func (c *Compactor) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running {
		return nil
	}

	close(c.stopChan)
	c.running = false

	return nil
}

// runCompactionLoop runs the background compaction loop
func (c *Compactor) runCompactionLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopChan:
			return
		case <-ticker.C:
			if err := c.Compact(); err != nil {
				c.mu.Lock()
				c.stats.Errors = append(c.stats.Errors, err)
				c.mu.Unlock()
			}
		}
	}
}

// Compact performs a compaction cycle
func (c *Compactor) Compact() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	start := time.Now()
	c.stats.CompactionsRun++

	// Get segments eligible for compaction
	segments, err := c.findCompactableSegments()
	if err != nil {
		return fmt.Errorf("failed to find compactable segments: %w", err)
	}

	if len(segments) == 0 {
		return nil // Nothing to compact
	}

	// Group segments for compaction
	groups := c.groupSegmentsForCompaction(segments)

	var totalBytesReclaimed int64
	var segmentsCompacted int

	// Compact each group
	for _, group := range groups {
		bytesReclaimed, err := c.compactSegmentGroup(group)
		if err != nil {
			c.stats.Errors = append(c.stats.Errors, err)
			continue
		}

		totalBytesReclaimed += bytesReclaimed
		segmentsCompacted += len(group)
	}

	// Update statistics
	c.stats.SegmentsCompacted += segmentsCompacted
	c.stats.BytesReclaimed += totalBytesReclaimed
	c.stats.LastCompactionTime = start
	c.stats.LastCompactionDuration = time.Since(start)
	c.lastCompacted = time.Now()

	// Clean up old segments
	if err := c.cleanupOldSegments(); err != nil {
		c.stats.Errors = append(c.stats.Errors, err)
	}

	return nil
}

// findCompactableSegments identifies segments eligible for compaction
func (c *Compactor) findCompactableSegments() ([]*Segment, error) {
	segments := c.wal.segments.GetAllSegments()

	var compactable []*Segment
	now := time.Now()

	for _, seg := range segments {
		// Skip active segment
		if !seg.Sealed {
			continue
		}

		// Check age
		age := now.Sub(seg.CreatedAt)
		if age < c.policy.MaxSegmentAge {
			continue
		}

		// Check size
		if seg.Size < c.policy.MinSegmentSize {
			compactable = append(compactable, seg)
			continue
		}

		// Check compaction ratio
		ratio := c.calculateCompactionRatio(seg)
		if ratio < c.policy.CompactRatio {
			compactable = append(compactable, seg)
		}
	}

	return compactable, nil
}

// calculateCompactionRatio calculates the ratio of live data in a segment
func (c *Compactor) calculateCompactionRatio(seg *Segment) float64 {
	// Read the segment to count live vs deleted/superseded records
	records, err := c.readSegmentRecords(seg)
	if err != nil {
		// If we can't read the segment, assume it needs compaction
		return 0.5
	}

	if len(records) == 0 {
		return 1.0 // Empty segment, no compaction needed
	}

	// Track sequence numbers to detect superseded records
	latestSeqMap := make(map[string]uint64) // key -> latest sequence
	deletedCount := 0
	supersededCount := 0
	totalSize := int64(0)
	liveSize := int64(0)

	// First pass: identify latest sequence for each key
	for _, record := range records {
		// Check if marked as deleted
		if record.Flags&RecordFlagDeleted != 0 {
			deletedCount++
			continue
		}

		// Parse event to get key if available
		event, err := record.GetEvent()
		if err == nil && event.Properties != nil {
			// Use a combination of properties as a unique key
			// For audit logs, this might be entity ID, user ID, etc.
			if entityID, ok := event.Properties["entity_id"]; ok {
				key := fmt.Sprintf("%v", entityID)
				if existingSeq, exists := latestSeqMap[key]; !exists || record.Sequence > existingSeq {
					latestSeqMap[key] = record.Sequence
				}
			}
		}
	}

	// Second pass: calculate live data size
	for _, record := range records {
		recordSize := int64(24 + 8 + 32 + record.Length + 4 + 4) // Header + seq + hash + data + crc + footer
		totalSize += recordSize

		// Skip deleted records
		if record.Flags&RecordFlagDeleted != 0 {
			continue
		}

		// Check if this record is superseded
		isSuperseded := false
		event, err := record.GetEvent()
		if err == nil && event.Properties != nil {
			if entityID, ok := event.Properties["entity_id"]; ok {
				key := fmt.Sprintf("%v", entityID)
				if latestSeq, exists := latestSeqMap[key]; exists && record.Sequence < latestSeq {
					isSuperseded = true
					supersededCount++
				}
			}
		}

		if !isSuperseded {
			liveSize += recordSize
		}
	}

	// Calculate ratio of live data
	if totalSize == 0 {
		return 1.0
	}

	ratio := float64(liveSize) / float64(totalSize)

	// Track compaction opportunity in stats for monitoring
	if ratio < 0.7 && (deletedCount > 0 || supersededCount > 0) {
		// Store metrics about compaction opportunity
		c.stats.LastAnalyzedSegment = seg.Path
		c.stats.LastAnalyzedRatio = ratio
		c.stats.LastAnalyzedDeleted = deletedCount
		c.stats.LastAnalyzedSuperseded = supersededCount
		c.stats.LastAnalyzedTime = time.Now()
	}

	return ratio
}

// groupSegmentsForCompaction groups segments for efficient compaction
func (c *Compactor) groupSegmentsForCompaction(segments []*Segment) [][]*Segment {
	// Sort segments by sequence range
	sort.Slice(segments, func(i, j int) bool {
		return segments[i].StartSeq < segments[j].StartSeq
	})

	var groups [][]*Segment
	var currentGroup []*Segment
	var currentSize int64

	for _, seg := range segments {
		if currentSize+seg.Size > c.policy.TargetSegmentSize && len(currentGroup) > 0 {
			// Start a new group
			groups = append(groups, currentGroup)
			currentGroup = []*Segment{seg}
			currentSize = seg.Size
		} else {
			currentGroup = append(currentGroup, seg)
			currentSize += seg.Size
		}
	}

	// Add the last group if it meets minimum requirements
	if len(currentGroup) >= c.policy.MinSegments || currentSize >= c.policy.MinSegmentSize {
		groups = append(groups, currentGroup)
	}

	return groups
}

// compactSegmentGroup compacts a group of segments into a new segment
func (c *Compactor) compactSegmentGroup(segments []*Segment) (int64, error) {
	if len(segments) == 0 {
		return 0, nil
	}

	// Calculate total size before compaction
	var totalSizeBefore int64
	for _, seg := range segments {
		totalSizeBefore += seg.Size
	}

	// Create new compacted segment
	compactedPath := c.generateCompactedSegmentPath(segments[0].StartSeq, segments[len(segments)-1].EndSeq)
	compactedFile, err := os.OpenFile(compactedPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL|os.O_SYNC, 0600)
	if err != nil {
		return 0, fmt.Errorf("failed to create compacted segment: %w", err)
	}
	defer compactedFile.Close()

	// Track written sequences to eliminate duplicates
	writtenSeqs := make(map[uint64]bool)
	var bytesWritten int64
	var lastHash [32]byte

	// Merge segments in sequence order
	for _, seg := range segments {
		records, err := c.readSegmentRecords(seg)
		if err != nil {
			os.Remove(compactedPath)
			return 0, fmt.Errorf("failed to read segment %s: %w", seg.Path, err)
		}

		for _, record := range records {
			// Skip duplicates
			if writtenSeqs[record.Sequence] {
				continue
			}

			// Skip deleted records (marked with special flag)
			if record.Flags&RecordFlagDeleted != 0 {
				continue
			}

			// Update hash chain
			record.PrevHash = lastHash

			// Write record to compacted segment
			data, err := record.Marshal()
			if err != nil {
				os.Remove(compactedPath)
				return 0, fmt.Errorf("failed to marshal record: %w", err)
			}

			n, err := compactedFile.Write(data)
			if err != nil {
				os.Remove(compactedPath)
				return 0, fmt.Errorf("failed to write record: %w", err)
			}

			bytesWritten += int64(n)
			writtenSeqs[record.Sequence] = true
			lastHash = record.ComputeHash()
		}
	}

	// Sync and close the compacted file
	if err := compactedFile.Sync(); err != nil {
		os.Remove(compactedPath)
		return 0, fmt.Errorf("failed to sync compacted segment: %w", err)
	}

	// Update segment manager with new segment
	compactedSeg := &Segment{
		Path:      compactedPath,
		StartSeq:  segments[0].StartSeq,
		EndSeq:    segments[len(segments)-1].EndSeq,
		Size:      bytesWritten,
		CreatedAt: time.Now(),
		Sealed:    true,
	}

	if err := c.wal.segments.AddCompactedSegment(compactedSeg); err != nil {
		os.Remove(compactedPath)
		return 0, fmt.Errorf("failed to register compacted segment: %w", err)
	}

	// Remove old segments
	for _, seg := range segments {
		if err := c.wal.segments.RemoveSegment(seg); err != nil {
			// Log error but continue
			c.stats.Errors = append(c.stats.Errors, fmt.Errorf("failed to remove segment %s: %w", seg.Path, err))
		}

		// Archive or delete the old segment
		if err := c.archiveSegment(seg); err != nil {
			c.stats.Errors = append(c.stats.Errors, fmt.Errorf("failed to archive segment %s: %w", seg.Path, err))
		}
	}

	// Calculate bytes reclaimed
	bytesReclaimed := totalSizeBefore - bytesWritten
	c.stats.BytesCompacted += totalSizeBefore

	return bytesReclaimed, nil
}

// readSegmentRecords reads all records from a segment
func (c *Compactor) readSegmentRecords(seg *Segment) ([]*Record, error) {
	file, err := os.Open(seg.Path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var records []*Record

	// Read file content
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}

	offset := 0
	for offset < len(data) {
		// Try to unmarshal record from data
		record, bytesRead, err := UnmarshalRecordFromBytes(data[offset:])
		if err != nil {
			if err == io.EOF {
				break
			}
			// Skip corrupted records during compaction
			offset++
			continue
		}
		records = append(records, record)
		offset += bytesRead
	}

	return records, nil
}

// generateCompactedSegmentPath generates a path for a compacted segment
func (c *Compactor) generateCompactedSegmentPath(startSeq, endSeq uint64) string {
	timestamp := time.Now().Format("20060102-150405")
	filename := fmt.Sprintf("compacted-%016x-%016x-%s.wal", startSeq, endSeq, timestamp)
	return filepath.Join(filepath.Dir(c.wal.path), filename)
}

// archiveSegment archives or deletes an old segment
func (c *Compactor) archiveSegment(seg *Segment) error {
	archiveDir := filepath.Join(filepath.Dir(c.wal.path), "archive")

	// Create archive directory if it doesn't exist
	if err := os.MkdirAll(archiveDir, 0700); err != nil {
		return err
	}

	// Move segment to archive
	archivePath := filepath.Join(archiveDir, filepath.Base(seg.Path))
	if err := os.Rename(seg.Path, archivePath); err != nil {
		// If rename fails, try copying then deleting
		if err := c.copyFile(seg.Path, archivePath); err != nil {
			return err
		}
		if err := os.Remove(seg.Path); err != nil {
			return err
		}
	}

	return nil
}

// copyFile copies a file from src to dst
func (c *Compactor) copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

// cleanupOldSegments removes segments older than the retention period
func (c *Compactor) cleanupOldSegments() error {
	if c.policy.RetentionPeriod == 0 {
		return nil // No retention policy
	}

	archiveDir := filepath.Join(filepath.Dir(c.wal.path), "archive")
	if _, err := os.Stat(archiveDir); os.IsNotExist(err) {
		return nil // No archive directory
	}

	entries, err := os.ReadDir(archiveDir)
	if err != nil {
		return fmt.Errorf("failed to read archive directory: %w", err)
	}

	cutoff := time.Now().Add(-c.policy.RetentionPeriod)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		if info.ModTime().Before(cutoff) {
			path := filepath.Join(archiveDir, entry.Name())
			if err := os.Remove(path); err != nil {
				c.stats.Errors = append(c.stats.Errors, fmt.Errorf("failed to remove old segment %s: %w", path, err))
			}
		}
	}

	return nil
}

// GetStats returns compaction statistics
func (c *Compactor) GetStats() CompactionStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Create a copy of stats
	stats := c.stats
	stats.Errors = make([]error, len(c.stats.Errors))
	copy(stats.Errors, c.stats.Errors)

	return stats
}

// ForceCompact triggers an immediate compaction
func (c *Compactor) ForceCompact() error {
	return c.Compact()
}

// CompactRange compacts segments within a sequence range
func (c *Compactor) CompactRange(startSeq, endSeq uint64) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	segments := c.wal.segments.GetSegmentsInRange(startSeq, endSeq)
	if len(segments) < 2 {
		return nil // Nothing to compact
	}

	_, err := c.compactSegmentGroup(segments)
	return err
}

// VacuumDeleted removes deleted records from segments
func (c *Compactor) VacuumDeleted() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	segments := c.wal.segments.GetAllSegments()

	for _, seg := range segments {
		if !seg.Sealed {
			continue // Skip active segment
		}

		if err := c.vacuumSegment(seg); err != nil {
			c.stats.Errors = append(c.stats.Errors, err)
		}
	}

	return nil
}

// vacuumSegment removes deleted records from a single segment
func (c *Compactor) vacuumSegment(seg *Segment) error {
	records, err := c.readSegmentRecords(seg)
	if err != nil {
		return err
	}

	// Count deleted records
	deletedCount := 0
	for _, record := range records {
		if record.Flags&RecordFlagDeleted != 0 {
			deletedCount++
		}
	}

	// Only vacuum if significant number of deleted records
	if float64(deletedCount)/float64(len(records)) < 0.1 {
		return nil // Less than 10% deleted
	}

	// Rewrite segment without deleted records
	_, err2 := c.compactSegmentGroup([]*Segment{seg})
	return err2
}

// RecordFlagCompacted marks a record as part of a compacted segment
const RecordFlagCompacted = 1 << 1

// MarkDeleted marks a record as deleted for later compaction
func (c *Compactor) MarkDeleted(sequence uint64) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Find the segment containing this sequence by checking all segments
	segments := c.wal.segments.GetAllSegments()
	var targetSegment *Segment
	for _, seg := range segments {
		if sequence >= seg.StartSeq && sequence <= seg.EndSeq {
			targetSegment = seg
			break
		}
	}

	if targetSegment == nil {
		return fmt.Errorf("sequence %d not found in any segment", sequence)
	}

	// For active segments, we need to write a deletion marker
	if !targetSegment.Sealed {
		// Write a special deletion record to mark this sequence as deleted
		deletionEvent := &core.LogEvent{
			Timestamp:       time.Now(),
			Level:           core.InformationLevel,
			MessageTemplate: "DELETION_MARKER",
			Properties: map[string]any{
				"deleted_sequence": sequence,
				"deleted_at":       time.Now().Unix(),
			},
		}

		// Get next sequence for deletion marker
		c.wal.mu.Lock()
		nextSeq := c.wal.sequence + 1
		c.wal.sequence = nextSeq
		lastHash := c.wal.lastHash
		c.wal.mu.Unlock()

		// Create deletion marker record with special flag
		record, err := NewRecord(deletionEvent, nextSeq, lastHash)
		if err != nil {
			return fmt.Errorf("failed to create deletion marker: %w", err)
		}
		record.Flags |= RecordFlagDeleted

		// Marshal and write the deletion marker
		data, err := record.Marshal()
		if err != nil {
			return fmt.Errorf("failed to marshal deletion marker: %w", err)
		}

		c.wal.mu.Lock()
		_, err = c.wal.file.Write(data)
		if err == nil {
			c.wal.lastHash = record.ComputeHash()
		}
		c.wal.mu.Unlock()

		if err != nil {
			return fmt.Errorf("failed to write deletion marker: %w", err)
		}

		return nil
	}

	// For sealed segments, we need to rewrite the segment with the flag updated
	records, err := c.readSegmentRecords(targetSegment)
	if err != nil {
		return fmt.Errorf("failed to read segment: %w", err)
	}

	// Find and mark the record as deleted
	found := false
	for _, record := range records {
		if record.Sequence == sequence {
			record.Flags |= RecordFlagDeleted
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("sequence %d not found in segment %s", sequence, targetSegment.Path)
	}

	// Create a temporary file for the updated segment
	tempPath := targetSegment.Path + ".tmp"
	tempFile, err := os.OpenFile(tempPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL|os.O_SYNC, 0600)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer tempFile.Close()

	// Write all records with the updated flags
	for _, record := range records {
		data, err := record.Marshal()
		if err != nil {
			os.Remove(tempPath)
			return fmt.Errorf("failed to marshal record: %w", err)
		}

		if _, err := tempFile.Write(data); err != nil {
			os.Remove(tempPath)
			return fmt.Errorf("failed to write record: %w", err)
		}
	}

	// Sync and close temp file
	if err := tempFile.Sync(); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("failed to sync temp file: %w", err)
	}
	tempFile.Close()

	// Atomically replace the original segment
	if err := os.Rename(tempPath, targetSegment.Path); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("failed to replace segment: %w", err)
	}

	// Update stats
	c.stats.CompactionsRun++

	return nil
}

// EstimateCompactionGain estimates bytes that would be reclaimed by compaction
func (c *Compactor) EstimateCompactionGain() (int64, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	segments, err := c.findCompactableSegments()
	if err != nil {
		return 0, err
	}

	var estimatedGain int64
	for _, seg := range segments {
		ratio := c.calculateCompactionRatio(seg)
		estimatedGain += int64(float64(seg.Size) * (1 - ratio))
	}

	return estimatedGain, nil
}
