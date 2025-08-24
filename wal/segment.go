package wal

import (
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
	
	// Simple reading - in production, we'd parse the actual record format
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	
	if len(data) > 0 {
		// For now, treat the entire file as one record
		// TODO: Implement proper record parsing
		records = append(records, data)
	}
	
	return records, nil
}

// fileExists checks if a file exists.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}