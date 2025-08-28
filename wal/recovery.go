package wal

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"time"

	"github.com/willibrandon/mtlog-audit/internal/logger"
	"github.com/willibrandon/mtlog/core"
)

// RecoveryEngine handles WAL recovery after corruption or crashes.
type RecoveryEngine struct {
	path           string
	maxRecordSize  int64
	skipCorrupted  bool
	verifyChecksum bool
}

// RecoveredRecord contains data from a recovered WAL record.
type RecoveredRecord struct {
	EventData []byte
	Sequence  uint64
	BytesRead int
}

// RecoveryReport contains the results of a recovery operation.
type RecoveryReport struct {
	TotalRecords      int
	RecoveredRecords  int
	CorruptedRecords  int
	SkippedBytes      int64
	LastGoodSequence  uint64
	RecoveredSegments []string
	Errors            []error
}

// RecoveryOption configures the recovery engine.
type RecoveryOption func(*RecoveryEngine)

// WithMaxRecordSize sets the maximum expected record size.
func WithMaxRecordSize(size int64) RecoveryOption {
	return func(r *RecoveryEngine) {
		r.maxRecordSize = size
	}
}

// WithSkipCorrupted allows recovery to continue past corrupted records.
func WithSkipCorrupted(skip bool) RecoveryOption {
	return func(r *RecoveryEngine) {
		r.skipCorrupted = skip
	}
}

// WithChecksumVerification enables CRC32 verification during recovery.
func WithChecksumVerification(verify bool) RecoveryOption {
	return func(r *RecoveryEngine) {
		r.verifyChecksum = verify
	}
}

// NewRecoveryEngine creates a new recovery engine for a WAL file.
func NewRecoveryEngine(path string, opts ...RecoveryOption) *RecoveryEngine {
	engine := &RecoveryEngine{
		path:           path,
		maxRecordSize:  10 * 1024 * 1024, // 10MB default
		skipCorrupted:  true,
		verifyChecksum: true,
	}

	for _, opt := range opts {
		opt(engine)
	}

	return engine
}

// Recover attempts to recover all valid records from the WAL.
func (r *RecoveryEngine) Recover() (*RecoveryReport, [][]byte, error) {
	report := &RecoveryReport{
		Errors: make([]error, 0),
	}

	file, err := os.Open(r.path)
	if err != nil {
		return report, nil, fmt.Errorf("failed to open WAL for recovery: %w", err)
	}
	defer file.Close()

	var records [][]byte
	offset := int64(0)

	// Get file size
	stat, err := file.Stat()
	if err != nil {
		return report, nil, err
	}
	fileSize := stat.Size()

	// Debug: log file size
	if fileSize == 0 {
		logger.Log.Warn("WAL file is empty")
		return report, records, nil
	}

	for offset < fileSize {
		record, err := r.readNextRecord(file, offset)
		if err != nil {
			if err == io.EOF {
				break
			}

			report.Errors = append(report.Errors, fmt.Errorf("at offset %d: %w", offset, err))
			
			if !r.skipCorrupted {
				return report, records, err
			}

			// Try to find next valid record
			skipBytes := r.findNextRecord(file, offset)
			if skipBytes == 0 {
				break // No more valid records found
			}
			
			report.SkippedBytes += skipBytes
			report.CorruptedRecords++
			offset += skipBytes
			continue
		}

		if record != nil {
			records = append(records, record.EventData)
			report.RecoveredRecords++
			report.LastGoodSequence = record.Sequence
		}

		report.TotalRecords++
		offset += int64(record.BytesRead)
	}

	logger.Log.Info("Recovery complete: {recovered}/{total} records recovered, {corrupted} corrupted, {skipped} bytes skipped",
		report.RecoveredRecords, report.TotalRecords, report.CorruptedRecords, report.SkippedBytes)

	return report, records, nil
}

// readNextRecord reads and validates the next record from the file.
// Returns a RecoveredRecord with EventData, sequence, and bytes read.
func (r *RecoveryEngine) readNextRecord(file *os.File, offset int64) (*RecoveredRecord, error) {
	// Seek to offset
	if _, err := file.Seek(offset, 0); err != nil {
		return nil, err
	}

	// We'll read the entire record into memory to verify CRCs
	// First, read the fixed header to get the data length
	headerSize := 4 + 2 + 2 + 4 + 8 + 4 // Magic through CRC32Header = 24 bytes
	header := make([]byte, headerSize)
	if _, err := io.ReadFull(file, header); err != nil {
		return nil, err
	}

	// Parse header fields
	buf := bytes.NewReader(header)
	
	// 1. Magic (4 bytes)
	var magic uint32
	if err := binary.Read(buf, binary.LittleEndian, &magic); err != nil {
		return nil, err
	}
	if magic != MagicHeader {
		return nil, fmt.Errorf("invalid magic header: %x", magic)
	}

	// 2. Version (2 bytes)
	var version uint16
	if err := binary.Read(buf, binary.LittleEndian, &version); err != nil {
		return nil, err
	}
	if version != Version {
		logger.Log.Warn("Unexpected version {version} at offset {offset}", version, offset)
	}

	// 3. Flags (2 bytes)
	var flags uint16
	if err := binary.Read(buf, binary.LittleEndian, &flags); err != nil {
		return nil, err
	}

	// 4. Length (4 bytes) - This is the EventData length
	var dataLength uint32
	if err := binary.Read(buf, binary.LittleEndian, &dataLength); err != nil {
		return nil, err
	}

	// Sanity check data length
	if dataLength > uint32(r.maxRecordSize) {
		return nil, fmt.Errorf("data length %d exceeds max %d", dataLength, r.maxRecordSize)
	}

	// 5. Timestamp (8 bytes)
	var timestamp int64
	if err := binary.Read(buf, binary.LittleEndian, &timestamp); err != nil {
		return nil, err
	}

	// 6. CRC32Header (4 bytes)
	var crc32Header uint32
	if err := binary.Read(buf, binary.LittleEndian, &crc32Header); err != nil {
		return nil, err
	}

	// Verify header CRC if requested
	if r.verifyChecksum {
		// CRC is calculated over the first 20 bytes (Magic through Timestamp)
		headerBytes := header[:20]
		expectedCRC := crc32.ChecksumIEEE(headerBytes)
		if crc32Header != expectedCRC {
			return nil, fmt.Errorf("header CRC mismatch: expected %x, got %x", expectedCRC, crc32Header)
		}
	}

	// Now read the rest of the record
	// Remaining: Sequence(8) + PrevHash(32) + EventData(dataLength) + CRC32Data(4) + MagicEnd(4)
	remainingSize := 8 + 32 + int(dataLength) + 4 + 4
	remaining := make([]byte, remainingSize)
	if _, err := io.ReadFull(file, remaining); err != nil {
		return nil, err
	}

	remainBuf := bytes.NewReader(remaining)

	// 7. Sequence (8 bytes)
	var sequence uint64
	if err := binary.Read(remainBuf, binary.LittleEndian, &sequence); err != nil {
		return nil, err
	}

	// 8. PrevHash (32 bytes)
	prevHash := make([]byte, 32)
	if _, err := io.ReadFull(remainBuf, prevHash); err != nil {
		return nil, err
	}

	// 9. EventData (variable)
	eventData := make([]byte, dataLength)
	if _, err := io.ReadFull(remainBuf, eventData); err != nil {
		return nil, err
	}

	// 10. CRC32Data (4 bytes)
	var crc32Data uint32
	if err := binary.Read(remainBuf, binary.LittleEndian, &crc32Data); err != nil {
		return nil, err
	}

	// 11. MagicEnd (4 bytes)
	var magicEnd uint32
	if err := binary.Read(remainBuf, binary.LittleEndian, &magicEnd); err != nil {
		return nil, err
	}

	if magicEnd != MagicFooter {
		return nil, fmt.Errorf("invalid magic footer: %x", magicEnd)
	}

	// Verify data CRC if requested
	if r.verifyChecksum {
		// CRC32Data is calculated over everything except itself and MagicEnd
		// Reconstruct the full record up to (but not including) CRC32Data
		fullRecord := make([]byte, 0, len(header)+len(remaining)-8)
		fullRecord = append(fullRecord, header...)
		fullRecord = append(fullRecord, remaining[:len(remaining)-8]...) // Exclude CRC32Data and MagicEnd
		
		expectedDataCRC := crc32.ChecksumIEEE(fullRecord)
		if crc32Data != expectedDataCRC {
			return nil, fmt.Errorf("data CRC mismatch: expected %x, got %x", expectedDataCRC, crc32Data)
		}
	}

	// Calculate total bytes read
	totalBytesRead := headerSize + remainingSize

	return &RecoveredRecord{
		EventData: eventData,
		Sequence:  sequence,
		BytesRead: totalBytesRead,
	}, nil
}

// findNextRecord attempts to find the next valid record by scanning for magic header.
func (r *RecoveryEngine) findNextRecord(file *os.File, startOffset int64) int64 {
	const scanWindow = 4096 // Scan in 4KB chunks
	buffer := make([]byte, scanWindow)
	
	// Get file size to avoid infinite loop
	stat, err := file.Stat()
	if err != nil {
		return 0
	}
	fileSize := stat.Size()
	
	offset := startOffset + 1 // Start from next byte
	
	for offset < fileSize {
		if _, err := file.Seek(offset, 0); err != nil {
			return 0
		}
		
		n, err := file.Read(buffer)
		if err != nil || n == 0 {
			return 0
		}
		
		// Look for magic header
		for i := 0; i <= n-4 && offset+int64(i) < fileSize; i++ {
			if binary.LittleEndian.Uint32(buffer[i:i+4]) == MagicHeader {
				// Found potential record start
				foundOffset := offset + int64(i)
				
				// Verify it's actually a valid record
				if _, err := r.readNextRecord(file, foundOffset); err == nil {
					return foundOffset - startOffset
				}
			}
		}
		
		// Move forward, overlapping by 4 bytes to catch boundaries
		if n < scanWindow {
			break // Reached end of file
		}
		offset += int64(n) - 4
	}
	
	return 0 // No valid record found
}

// RecoverSegments recovers records from multiple WAL segments.
func (r *RecoveryEngine) RecoverSegments(segments []*Segment) (*RecoveryReport, [][]byte, error) {
	report := &RecoveryReport{
		Errors:            make([]error, 0),
		RecoveredSegments: make([]string, 0),
	}
	
	var allRecords [][]byte
	
	for _, segment := range segments {
		logger.Log.Info("Recovering segment {path}", segment.Path)
		
		engine := NewRecoveryEngine(segment.Path, 
			WithSkipCorrupted(r.skipCorrupted),
			WithChecksumVerification(r.verifyChecksum),
			WithMaxRecordSize(r.maxRecordSize),
		)
		
		segReport, records, err := engine.Recover()
		if err != nil {
			report.Errors = append(report.Errors, fmt.Errorf("segment %s: %w", segment.Path, err))
			if !r.skipCorrupted {
				return report, allRecords, err
			}
			continue
		}
		
		// Merge results
		report.TotalRecords += segReport.TotalRecords
		report.RecoveredRecords += segReport.RecoveredRecords
		report.CorruptedRecords += segReport.CorruptedRecords
		report.SkippedBytes += segReport.SkippedBytes
		
		if segReport.LastGoodSequence > report.LastGoodSequence {
			report.LastGoodSequence = segReport.LastGoodSequence
		}
		
		if segReport.RecoveredRecords > 0 {
			report.RecoveredSegments = append(report.RecoveredSegments, segment.Path)
			allRecords = append(allRecords, records...)
		}
		
		report.Errors = append(report.Errors, segReport.Errors...)
	}
	
	return report, allRecords, nil
}

// RepairWAL attempts to repair a corrupted WAL by recovering valid records
// and writing them to a new file.
func (r *RecoveryEngine) RepairWAL(outputPath string) error {
	logger.Log.Info("Starting WAL repair from {source} to {dest}", r.path, outputPath)
	
	// Recover all valid records
	report, records, err := r.Recover()
	if err != nil && !r.skipCorrupted {
		return fmt.Errorf("recovery failed: %w", err)
	}
	
	if report.RecoveredRecords == 0 {
		return errors.New("no records could be recovered")
	}
	
	logger.Log.Info("Recovered {count} records, writing to new WAL", report.RecoveredRecords)
	
	// Create new WAL file
	output, err := os.OpenFile(outputPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC|os.O_SYNC, 0600)
	if err != nil {
		return fmt.Errorf("failed to create output WAL: %w", err)
	}
	defer output.Close()
	
	// Write recovered records to new WAL
	var lastHash [32]byte
	for i, recordData := range records {
		// Deserialize the event to get its original timestamp
		var event core.LogEvent
		timestamp := time.Now().UnixNano() // Default to now if deserialization fails
		
		if err := json.Unmarshal(recordData, &event); err == nil && event.Timestamp.Unix() > 0 {
			timestamp = event.Timestamp.UnixNano()
		}
		
		// Properly reconstruct the record with correct metadata
		record := &Record{
			Magic:     MagicHeader,
			Version:   Version,
			Sequence:  uint64(i + 1),
			PrevHash:  lastHash,
			EventData: recordData,
			MagicEnd:  MagicFooter,
			Length:    uint32(len(recordData)),
			Timestamp: timestamp,
			Flags:     0, // Reset flags for repaired records
		}
		
		// Marshal and write
		data, err := record.Marshal()
		if err != nil {
			return fmt.Errorf("failed to marshal record %d: %w", i, err)
		}
		
		if _, err := output.Write(data); err != nil {
			return fmt.Errorf("failed to write record %d: %w", i, err)
		}
		
		lastHash = record.ComputeHash()
	}
	
	logger.Log.Info("WAL repair complete: {recovered} records written to {path}", 
		report.RecoveredRecords, outputPath)
	
	return nil
}