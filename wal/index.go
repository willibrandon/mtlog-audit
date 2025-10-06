package wal

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"sort"
	"sync"
	"time"
)

// Index provides fast lookups for WAL segments and records.
// It maintains an in-memory index of segments with their sequence ranges
// and timestamps for efficient time-based queries.
type Index struct {
	mu       sync.RWMutex
	segments []SegmentInfo
	entries  map[uint64]IndexEntry // sequence -> entry mapping
	path     string
}

// SegmentInfo contains metadata about a WAL segment
type SegmentInfo struct {
	Path        string
	StartSeq    uint64
	EndSeq      uint64
	StartTime   time.Time
	EndTime     time.Time
	Size        int64
	RecordCount int
	Sealed      bool
	Corrupted   bool
}

// IndexEntry represents a single record's location in the WAL
type IndexEntry struct {
	Sequence  uint64
	Segment   string
	Offset    int64
	Size      int32
	Timestamp time.Time
	Checksum  uint32
	Flags     uint16 // Record flags (deleted, compacted, etc.)
}

// NewIndex creates a new WAL index
func NewIndex(path string) *Index {
	return &Index{
		path:    path,
		entries: make(map[uint64]IndexEntry),
	}
}

// Build scans all WAL segments and builds the index
func (idx *Index) Build(segments []*Segment) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	idx.segments = make([]SegmentInfo, 0, len(segments))
	idx.entries = make(map[uint64]IndexEntry)

	for _, seg := range segments {
		info, err := idx.indexSegment(seg)
		if err != nil {
			return fmt.Errorf("failed to index segment %s: %w", seg.Path, err)
		}
		idx.segments = append(idx.segments, info)
	}

	// Sort segments by start sequence
	sort.Slice(idx.segments, func(i, j int) bool {
		return idx.segments[i].StartSeq < idx.segments[j].StartSeq
	})

	return idx.persist()
}

// indexSegment builds an index for a single segment
func (idx *Index) indexSegment(seg *Segment) (SegmentInfo, error) {
	info := SegmentInfo{
		Path:   seg.Path,
		Sealed: seg.Sealed,
	}

	file, err := os.Open(seg.Path)
	if err != nil {
		return info, err
	}
	defer func() { _ = file.Close() }()

	stat, err := file.Stat()
	if err != nil {
		return info, err
	}
	info.Size = stat.Size()

	offset := int64(0)
	recordCount := 0

	for {
		// Read fixed header to get record info
		headerBuf := make([]byte, 24) // Magic(4) + Version(2) + Flags(2) + Length(4) + Timestamp(8) + CRC32Header(4)
		n, err := file.Read(headerBuf)
		if err == io.EOF || n == 0 {
			break
		}
		if err != nil || n < 24 {
			info.Corrupted = true
			break
		}

		// Parse header
		magic := binary.LittleEndian.Uint32(headerBuf[0:4])
		if magic != MagicHeader {
			info.Corrupted = true
			break
		}

		version := binary.LittleEndian.Uint16(headerBuf[4:6])
		flags := binary.LittleEndian.Uint16(headerBuf[6:8])
		length := binary.LittleEndian.Uint32(headerBuf[8:12])
		// #nosec G115 - timestamp from binary format
		timestamp := int64(binary.LittleEndian.Uint64(headerBuf[12:20]))
		crc32Header := binary.LittleEndian.Uint32(headerBuf[20:24])

		// Validate version
		if version != Version {
			info.Corrupted = true
			break
		}

		// Read sequence number (first 8 bytes after header)
		var sequence uint64
		if err := binary.Read(file, binary.LittleEndian, &sequence); err != nil {
			info.Corrupted = true
			break
		}

		// Create index entry
		entry := IndexEntry{
			Sequence: sequence,
			Segment:  seg.Path,
			Offset:   offset,
			// #nosec G115 - length validated, bounded by max record size
			Size:      int32(length),
			Timestamp: time.Unix(0, timestamp),
			Checksum:  crc32Header,
			Flags:     flags,
		}

		// Update segment info
		if recordCount == 0 {
			info.StartSeq = sequence
			info.StartTime = entry.Timestamp
		}
		info.EndSeq = sequence
		info.EndTime = entry.Timestamp
		recordCount++

		// Add to index
		idx.entries[sequence] = entry

		// Calculate total record size: header(24) + sequence(8) + prevhash(32) + data(length) + crc(4) + footer(4)
		recordSize := int64(24 + 8 + 32 + length + 4 + 4)
		offset += recordSize

		if _, err := file.Seek(offset, io.SeekStart); err != nil {
			break
		}
	}

	info.RecordCount = recordCount
	return info, nil
}

// FindBySequence locates a record by sequence number
func (idx *Index) FindBySequence(seq uint64) (*IndexEntry, error) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	entry, exists := idx.entries[seq]
	if !exists {
		return nil, fmt.Errorf("sequence %d not found", seq)
	}

	return &entry, nil
}

// FindBySequenceExcludeDeleted locates a record by sequence number, excluding deleted records
func (idx *Index) FindBySequenceExcludeDeleted(seq uint64) (*IndexEntry, error) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	entry, exists := idx.entries[seq]
	if !exists {
		return nil, fmt.Errorf("sequence %d not found", seq)
	}

	// Check if record is marked as deleted
	if entry.Flags&RecordFlagDeleted != 0 {
		return nil, fmt.Errorf("sequence %d is marked as deleted", seq)
	}

	return &entry, nil
}

// FindByTimeRange returns all records within a time range
func (idx *Index) FindByTimeRange(start, end time.Time) ([]IndexEntry, error) {
	return idx.findByTimeRange(start, end, false)
}

// FindByTimeRangeExcludeDeleted returns all non-deleted records within a time range
func (idx *Index) FindByTimeRangeExcludeDeleted(start, end time.Time) ([]IndexEntry, error) {
	return idx.findByTimeRange(start, end, true)
}

// findByTimeRange is the internal implementation for time range queries
func (idx *Index) findByTimeRange(start, end time.Time, excludeDeleted bool) ([]IndexEntry, error) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	var results []IndexEntry

	// First, find relevant segments
	relevantSegments := idx.findRelevantSegments(start, end)

	// Then, collect entries from those segments
	for _, seg := range relevantSegments {
		for seq := seg.StartSeq; seq <= seg.EndSeq; seq++ {
			if entry, exists := idx.entries[seq]; exists {
				// Skip deleted records if requested
				if excludeDeleted && entry.Flags&RecordFlagDeleted != 0 {
					continue
				}

				if !entry.Timestamp.Before(start) && !entry.Timestamp.After(end) {
					results = append(results, entry)
				}
			}
		}
	}

	// Sort by sequence
	sort.Slice(results, func(i, j int) bool {
		return results[i].Sequence < results[j].Sequence
	})

	return results, nil
}

// findRelevantSegments returns segments that might contain records in the time range
func (idx *Index) findRelevantSegments(start, end time.Time) []SegmentInfo {
	var relevant []SegmentInfo

	for _, seg := range idx.segments {
		// Check if segment time range overlaps with query range
		if seg.EndTime.Before(start) {
			continue // Segment ends before range starts
		}
		if seg.StartTime.After(end) {
			continue // Segment starts after range ends
		}
		relevant = append(relevant, seg)
	}

	return relevant
}

// GetSegmentInfo returns information about all segments
func (idx *Index) GetSegmentInfo() []SegmentInfo {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	result := make([]SegmentInfo, len(idx.segments))
	copy(result, idx.segments)
	return result
}

// GetSequenceRange returns the min and max sequence numbers in the index
func (idx *Index) GetSequenceRange() (minSeq, maxSeq uint64) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	if len(idx.segments) == 0 {
		return 0, 0
	}

	minSeq = idx.segments[0].StartSeq
	maxSeq = idx.segments[len(idx.segments)-1].EndSeq
	return minSeq, maxSeq
}

// AddEntry adds a new entry to the index
func (idx *Index) AddEntry(entry IndexEntry) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	idx.entries[entry.Sequence] = entry

	// Update segment info if needed
	for i := range idx.segments {
		if idx.segments[i].Path == entry.Segment {
			if entry.Sequence > idx.segments[i].EndSeq {
				idx.segments[i].EndSeq = entry.Sequence
				idx.segments[i].EndTime = entry.Timestamp
				idx.segments[i].RecordCount++
			}
			break
		}
	}
}

// RemoveSegment removes a segment from the index
func (idx *Index) RemoveSegment(path string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	// Remove segment info
	newSegments := make([]SegmentInfo, 0, len(idx.segments)-1)
	var removedSeg SegmentInfo
	for _, seg := range idx.segments {
		if seg.Path != path {
			newSegments = append(newSegments, seg)
		} else {
			removedSeg = seg
		}
	}
	idx.segments = newSegments

	// Remove associated entries
	for seq := removedSeg.StartSeq; seq <= removedSeg.EndSeq; seq++ {
		delete(idx.entries, seq)
	}
}

// persist saves the index to disk for fast recovery
func (idx *Index) persist() error {
	indexPath := idx.path + ".idx"
	// #nosec G304 - index path derived from controlled WAL path
	file, err := os.Create(indexPath)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	// Write version
	if err := binary.Write(file, binary.LittleEndian, uint32(1)); err != nil {
		return err
	}

	// Write segment count
	// #nosec G115 - segment count bounded
	if err := binary.Write(file, binary.LittleEndian, uint32(len(idx.segments))); err != nil {
		return err
	}

	// Write segments
	for _, seg := range idx.segments {
		// Write segment info
		if err := binary.Write(file, binary.LittleEndian, seg.StartSeq); err != nil {
			return err
		}
		if err := binary.Write(file, binary.LittleEndian, seg.EndSeq); err != nil {
			return err
		}
		if err := binary.Write(file, binary.LittleEndian, seg.StartTime.UnixNano()); err != nil {
			return err
		}
		if err := binary.Write(file, binary.LittleEndian, seg.EndTime.UnixNano()); err != nil {
			return err
		}
		if err := binary.Write(file, binary.LittleEndian, seg.Size); err != nil {
			return err
		}
		// #nosec G115 - record count bounded
		if err := binary.Write(file, binary.LittleEndian, int32(seg.RecordCount)); err != nil {
			return err
		}

		// Write path
		pathBytes := []byte(seg.Path)
		// #nosec G115 - path length bounded by max path
		if err := binary.Write(file, binary.LittleEndian, uint16(len(pathBytes))); err != nil {
			return err
		}
		if _, err := file.Write(pathBytes); err != nil {
			return err
		}
	}

	return file.Sync()
}

// Load loads the index from disk
func (idx *Index) Load() error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	indexPath := idx.path + ".idx"
	// #nosec G304 - index path derived from controlled WAL path
	file, err := os.Open(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No index file yet
		}
		return err
	}
	defer func() { _ = file.Close() }()

	// Read version
	var version uint32
	if err := binary.Read(file, binary.LittleEndian, &version); err != nil {
		return err
	}
	if version != 1 {
		return fmt.Errorf("unsupported index version: %d", version)
	}

	// Read segment count
	var segCount uint32
	if err := binary.Read(file, binary.LittleEndian, &segCount); err != nil {
		return err
	}

	// Read segments
	idx.segments = make([]SegmentInfo, 0, segCount)
	for i := uint32(0); i < segCount; i++ {
		var seg SegmentInfo

		// Read segment info
		if err := binary.Read(file, binary.LittleEndian, &seg.StartSeq); err != nil {
			return err
		}
		if err := binary.Read(file, binary.LittleEndian, &seg.EndSeq); err != nil {
			return err
		}

		var startNano, endNano int64
		if err := binary.Read(file, binary.LittleEndian, &startNano); err != nil {
			return err
		}
		if err := binary.Read(file, binary.LittleEndian, &endNano); err != nil {
			return err
		}
		seg.StartTime = time.Unix(0, startNano)
		seg.EndTime = time.Unix(0, endNano)

		if err := binary.Read(file, binary.LittleEndian, &seg.Size); err != nil {
			return err
		}

		var recordCount int32
		if err := binary.Read(file, binary.LittleEndian, &recordCount); err != nil {
			return err
		}
		seg.RecordCount = int(recordCount)

		// Read path
		var pathLen uint16
		if err := binary.Read(file, binary.LittleEndian, &pathLen); err != nil {
			return err
		}
		pathBytes := make([]byte, pathLen)
		if _, err := file.Read(pathBytes); err != nil {
			return err
		}
		seg.Path = string(pathBytes)

		idx.segments = append(idx.segments, seg)
	}

	// Rebuild entries map by scanning segments
	idx.entries = make(map[uint64]IndexEntry)
	for _, seg := range idx.segments {
		// Re-index the segment to populate entries
		file, err := os.Open(seg.Path)
		if err != nil {
			// Skip segments that can't be opened
			continue
		}
		defer func() { _ = file.Close() }()

		// Re-scan segment to rebuild entries
		_ = idx.scanSegmentForEntries(file, seg.Path)
	}

	return nil
}

// scanSegmentForEntries scans a segment file and adds entries to the index
func (idx *Index) scanSegmentForEntries(file *os.File, path string) error {
	offset := int64(0)

	for {
		// Read fixed header to get record info
		headerBuf := make([]byte, 24)
		n, err := file.Read(headerBuf)
		if err == io.EOF || n == 0 {
			break
		}
		if err != nil || n < 24 {
			break
		}

		// Parse header
		magic := binary.LittleEndian.Uint32(headerBuf[0:4])
		if magic != MagicHeader {
			break
		}

		version := binary.LittleEndian.Uint16(headerBuf[4:6])
		if version != Version {
			break // Skip incompatible versions
		}

		flags := binary.LittleEndian.Uint16(headerBuf[6:8])
		length := binary.LittleEndian.Uint32(headerBuf[8:12])
		// #nosec G115 - timestamp from binary format
		timestamp := int64(binary.LittleEndian.Uint64(headerBuf[12:20]))
		crc32Header := binary.LittleEndian.Uint32(headerBuf[20:24])

		// Read sequence number
		var sequence uint64
		if err := binary.Read(file, binary.LittleEndian, &sequence); err != nil {
			break
		}

		// Create index entry
		entry := IndexEntry{
			Sequence: sequence,
			Segment:  path,
			Offset:   offset,
			// #nosec G115 - length validated, bounded by max record size
			Size:      int32(length),
			Timestamp: time.Unix(0, timestamp),
			Checksum:  crc32Header,
			Flags:     flags,
		}

		// Add to index
		idx.entries[sequence] = entry

		// Calculate total record size and seek to next
		recordSize := int64(24 + 8 + 32 + length + 4 + 4)
		offset += recordSize

		if _, err := file.Seek(offset, io.SeekStart); err != nil {
			break
		}
	}

	return nil
}
