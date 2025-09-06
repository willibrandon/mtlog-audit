package wal

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/willibrandon/mtlog-audit/internal/logger"
)

// Segment represents a single WAL segment file.
type Segment struct {
	Path      string
	StartSeq  uint64
	EndSeq    uint64
	Size      int64
	CreatedAt time.Time
	Sealed    bool
	Version   uint16
	Corrupted bool
}

// SegmentManager handles multiple WAL segments.
type SegmentManager struct {
	baseDir     string
	baseName    string
	segments    []*Segment
	activeIndex int
	maxSize     int64
	maxAge      time.Duration
	maxSegments int
}

// NewSegmentManager creates a new segment manager.
func NewSegmentManager(walPath string, maxSize int64) (*SegmentManager, error) {
	dir := filepath.Dir(walPath)
	base := filepath.Base(walPath)
	
	// Remove .wal extension if present
	base = strings.TrimSuffix(base, ".wal")
	
	sm := &SegmentManager{
		baseDir:     dir,
		baseName:    base,
		maxSize:     maxSize,
		maxSegments: 10, // Keep last 10 segments by default
		segments:    make([]*Segment, 0),
		activeIndex: -1,
	}
	
	// Scan for existing segments
	if err := sm.scanSegments(); err != nil {
		return nil, fmt.Errorf("failed to scan segments: %w", err)
	}
	
	return sm, nil
}

// GetActivePath returns the path to the active segment.
func (sm *SegmentManager) GetActivePath() string {
	if sm.activeIndex >= 0 && sm.activeIndex < len(sm.segments) {
		return sm.segments[sm.activeIndex].Path
	}
	// Default path if no segments exist
	return filepath.Join(sm.baseDir, sm.baseName+".wal")
}

// ShouldRotate checks if the current segment should be rotated.
func (sm *SegmentManager) ShouldRotate(currentSize int64) bool {
	return currentSize >= sm.maxSize
}

// Rotate creates a new segment and seals the current one.
func (sm *SegmentManager) Rotate(currentSeq uint64) (string, error) {
	// Seal current segment if exists
	if sm.activeIndex >= 0 && sm.activeIndex < len(sm.segments) {
		sm.segments[sm.activeIndex].Sealed = true
		sm.segments[sm.activeIndex].EndSeq = currentSeq
	}
	
	// Create new segment with timestamp
	timestamp := time.Now().Format("20060102-150405")
	segmentName := fmt.Sprintf("%s-%s.wal", sm.baseName, timestamp)
	segmentPath := filepath.Join(sm.baseDir, segmentName)
	
	// Ensure unique filename
	counter := 1
	for fileExists(segmentPath) {
		segmentName = fmt.Sprintf("%s-%s-%d.wal", sm.baseName, timestamp, counter)
		segmentPath = filepath.Join(sm.baseDir, segmentName)
		counter++
	}
	
	// Create new segment
	segment := &Segment{
		Path:      segmentPath,
		StartSeq:  currentSeq + 1,
		CreatedAt: time.Now(),
		Sealed:    false,
	}
	
	sm.segments = append(sm.segments, segment)
	sm.activeIndex = len(sm.segments) - 1
	
	// Clean up old segments if needed
	if err := sm.cleanupOldSegments(); err != nil {
		// Log error but don't fail rotation
		logger.Log.Warn("Failed to cleanup old segments: {error}", err)
	}
	
	return segmentPath, nil
}

// scanSegments discovers existing WAL segments.
func (sm *SegmentManager) scanSegments() error {
	pattern := filepath.Join(sm.baseDir, sm.baseName+"*.wal")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return err
	}
	
	for _, path := range matches {
		stat, err := os.Stat(path)
		if err != nil {
			continue
		}
		
		segment := &Segment{
			Path:      path,
			Size:      stat.Size(),
			CreatedAt: stat.ModTime(),
		}
		
		// Try to parse sequence numbers from filename
		sm.parseSequenceFromName(segment)
		
		sm.segments = append(sm.segments, segment)
	}
	
	// Sort segments by creation time
	sort.Slice(sm.segments, func(i, j int) bool {
		return sm.segments[i].CreatedAt.Before(sm.segments[j].CreatedAt)
	})
	
	// Set the last segment as active if it exists and is not sealed
	if len(sm.segments) > 0 {
		sm.activeIndex = len(sm.segments) - 1
		// Check if the last segment is sealed (has a newer segment after it)
		if sm.activeIndex > 0 {
			sm.segments[sm.activeIndex-1].Sealed = true
		}
	}
	
	return nil
}

// parseSequenceFromName attempts to extract sequence numbers from segment filename.
func (sm *SegmentManager) parseSequenceFromName(segment *Segment) {
	base := filepath.Base(segment.Path)
	// Remove extension
	base = strings.TrimSuffix(base, ".wal")
	
	// Try to find sequence numbers in format: name-startSeq-endSeq-timestamp
	parts := strings.Split(base, "-")
	if len(parts) >= 3 {
		// Try to parse sequence numbers
		if start, err := strconv.ParseUint(parts[len(parts)-2], 10, 64); err == nil {
			segment.StartSeq = start
		}
		if end, err := strconv.ParseUint(parts[len(parts)-1], 10, 64); err == nil {
			segment.EndSeq = end
		}
	}
}

// cleanupOldSegments removes old segments based on retention policy.
func (sm *SegmentManager) cleanupOldSegments() error {
	if len(sm.segments) <= sm.maxSegments {
		return nil
	}
	
	// Keep only the last maxSegments
	toDelete := len(sm.segments) - sm.maxSegments
	
	for i := 0; i < toDelete; i++ {
		if sm.segments[i].Sealed {
			if err := os.Remove(sm.segments[i].Path); err != nil {
				return fmt.Errorf("failed to remove old segment %s: %w", sm.segments[i].Path, err)
			}
		}
	}
	
	// Remove deleted segments from the list
	sm.segments = sm.segments[toDelete:]
	sm.activeIndex = len(sm.segments) - 1
	
	return nil
}

// GetSegments returns all known segments.
func (sm *SegmentManager) GetSegments() []*Segment {
	return sm.segments
}

// ReadAllSegments reads all events from all segments in order.
func (sm *SegmentManager) ReadAllSegments() ([][]byte, error) {
	var allRecords [][]byte
	
	for _, segment := range sm.segments {
		records, err := sm.readSegment(segment.Path)
		if err != nil {
			return nil, fmt.Errorf("failed to read segment %s: %w", segment.Path, err)
		}
		allRecords = append(allRecords, records...)
	}
	
	return allRecords, nil
}

// readSegment reads all records from a single segment file.
func (sm *SegmentManager) readSegment(path string) ([][]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	
	var records [][]byte
	
	// Read entire file
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	
	if len(data) == 0 {
		return nil, nil
	}
	
	// Parse records from binary format
	offset := 0
	for offset < len(data) {
		// Check if we have enough data for header
		if offset+24 > len(data) { // 24 is minimum header size
			break
		}
		
		// Read magic number
		magic := binary.LittleEndian.Uint32(data[offset:])
		if magic != MagicHeader {
			break // End of valid records
		}
		
		// Read record length from header (offset 8, 4 bytes)
		length := binary.LittleEndian.Uint32(data[offset+8:offset+12])
		
		// Calculate total record size:
		// header(24) + sequence(8) + prevhash(32) + data(length) + crc(4) + footer(4)
		totalSize := 24 + 8 + 32 + int(length) + 4 + 4
		
		if offset+totalSize > len(data) {
			break // Incomplete record
		}
		
		// Extract complete record
		record := make([]byte, totalSize)
		copy(record, data[offset:offset+totalSize])
		records = append(records, record)
		offset += totalSize
	}
	
	return records, nil
}

// fileExists checks if a file exists.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// GetAllSegments returns all segments managed by the segment manager
func (sm *SegmentManager) GetAllSegments() []*Segment {
	return sm.segments
}

// AddCompactedSegment adds a compacted segment to the manager
func (sm *SegmentManager) AddCompactedSegment(seg *Segment) error {
	sm.segments = append(sm.segments, seg)
	// Re-sort segments by start sequence
	sort.Slice(sm.segments, func(i, j int) bool {
		return sm.segments[i].StartSeq < sm.segments[j].StartSeq
	})
	return nil
}

// RemoveSegment removes a segment from the manager
func (sm *SegmentManager) RemoveSegment(seg *Segment) error {
	for i, s := range sm.segments {
		if s.Path == seg.Path {
			// Remove from slice
			sm.segments = append(sm.segments[:i], sm.segments[i+1:]...)
			// Adjust active index if needed
			if i == sm.activeIndex {
				sm.activeIndex = -1
			} else if i < sm.activeIndex {
				sm.activeIndex--
			}
			// Delete the file
			return os.Remove(seg.Path)
		}
	}
	return nil
}

// GetSegmentsInRange returns segments within a sequence range
func (sm *SegmentManager) GetSegmentsInRange(startSeq, endSeq uint64) []*Segment {
	var result []*Segment
	for _, seg := range sm.segments {
		// Check if segment overlaps with the range
		if seg.EndSeq >= startSeq && seg.StartSeq <= endSeq {
			result = append(result, seg)
		}
	}
	return result
}

