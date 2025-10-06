package wal

import (
	"bytes"
	"crypto/sha256"
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
	hashChains     map[uint64][32]byte
	path           string
	maxRecordSize  int64
	skipCorrupted  bool
	verifyChecksum bool
	enableForensic bool
}

// RecoveredRecord contains data from a recovered WAL record.
type RecoveredRecord struct {
	EventData      []byte
	Sequence       uint64
	Timestamp      uint64
	BytesRead      int
	HashChainValid bool
}

// RecoveryReport contains the results of a recovery operation.
type RecoveryReport struct {
	RecoveredSegments   []string
	Errors              []error
	RecoveryMethods     []string
	TotalRecords        int
	RecoveredRecords    int
	CorruptedRecords    int
	SkippedBytes        int64
	LastGoodSequence    uint64
	HashChainBreaks     int
	ReconstructedChains int
	PartialRecords      int
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

// WithForensicRecovery enables advanced forensic recovery techniques
func WithForensicRecovery(enable bool) RecoveryOption {
	return func(r *RecoveryEngine) {
		r.enableForensic = enable
		if enable {
			r.hashChains = make(map[uint64][32]byte)
		}
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
	defer func() { _ = file.Close() }()

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
	// #nosec G115 - comparison with validated max size
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
	// #nosec G304 - output path from user-specified recovery destination
	output, err := os.OpenFile(outputPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC|os.O_SYNC, 0o600)
	if err != nil {
		return fmt.Errorf("failed to create output WAL: %w", err)
	}
	defer func() { _ = output.Close() }()

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
			Magic:   MagicHeader,
			Version: Version,
			// #nosec G115 - loop index bounded
			Sequence:  uint64(i + 1),
			PrevHash:  lastHash,
			EventData: recordData,
			MagicEnd:  MagicFooter,
			// #nosec G115 - record data length validated
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

// ForensicRecover performs advanced forensic recovery with hash chain reconstruction
func (r *RecoveryEngine) ForensicRecover() (*RecoveryReport, [][]byte, error) {
	report := &RecoveryReport{
		Errors:          make([]error, 0),
		RecoveryMethods: make([]string, 0),
	}

	// First try standard recovery
	standardReport, records, err := r.Recover()
	if err != nil && !r.skipCorrupted {
		return report, nil, err
	}

	// Merge standard report
	*report = *standardReport

	if !r.enableForensic {
		return report, records, nil
	}

	logger.Log.Info("Starting forensic recovery")
	report.RecoveryMethods = append(report.RecoveryMethods, "forensic_recovery")

	// Build hash chain map
	r.buildHashChainMap(records)

	// Attempt to recover broken chains
	additionalRecords := r.reconstructHashChains(report)
	records = append(records, additionalRecords...)

	// Try advanced recovery techniques
	if partialRecords := r.recoverPartialRecords(); len(partialRecords) > 0 {
		report.RecoveryMethods = append(report.RecoveryMethods, "partial_record_recovery")
		report.PartialRecords += len(partialRecords)
		records = append(records, partialRecords...)
	}

	// Attempt shadow recovery
	if shadowRecords := r.recoverFromShadow(); len(shadowRecords) > 0 {
		report.RecoveryMethods = append(report.RecoveryMethods, "shadow_recovery")
		records = append(records, shadowRecords...)
	}

	return report, records, nil
}

// buildHashChainMap builds a map of sequence numbers to hashes
func (r *RecoveryEngine) buildHashChainMap(records [][]byte) {
	for i, recordData := range records {
		// Try to extract sequence and hash from recovered data
		if len(recordData) < 40 {
			continue
		}

		// Create a minimal record just for hash computation
		record := &Record{
			// #nosec G115 - loop index bounded
			Sequence:  uint64(i + 1),
			EventData: recordData,
		}
		hash := record.ComputeHash()
		r.hashChains[record.Sequence] = hash
	}
}

// reconstructHashChains attempts to reconstruct broken hash chains
func (r *RecoveryEngine) reconstructHashChains(report *RecoveryReport) [][]byte {
	var reconstructed [][]byte

	// Scan for hash chain breaks
	file, err := os.Open(r.path)
	if err != nil {
		return reconstructed
	}
	defer func() { _ = file.Close() }()

	offset := int64(0)
	stat, _ := file.Stat()
	fileSize := stat.Size()

	var lastGoodHash [32]byte
	chainBroken := false

	for offset < fileSize {
		// Try to read a record
		record, err := r.readRecordWithHashRecovery(file, offset, lastGoodHash)
		if err != nil {
			if !chainBroken {
				report.HashChainBreaks++
				chainBroken = true
			}

			// Skip and continue
			offset++
			continue
		}

		if record != nil {
			if chainBroken {
				report.ReconstructedChains++
				chainBroken = false
			}

			reconstructed = append(reconstructed, record.EventData)
			lastGoodHash = sha256.Sum256(record.EventData)
			offset += int64(record.BytesRead)
		} else {
			offset++
		}
	}

	return reconstructed
}

// readRecordWithHashRecovery attempts to read a record with hash chain recovery
func (r *RecoveryEngine) readRecordWithHashRecovery(file *os.File, offset int64, lastGoodHash [32]byte) (*RecoveredRecord, error) {
	// First try normal read
	record, err := r.readNextRecord(file, offset)
	if err == nil {
		return record, nil
	}

	// If hash chain is broken, try to reconstruct
	if r.enableForensic {
		// Try using last good hash
		if record := r.attemptHashReconstruction(file, offset, lastGoodHash); record != nil {
			return record, nil
		}
	}

	return nil, err
}

// attemptHashReconstruction tries to reconstruct a record using hash chain forensics
func (r *RecoveryEngine) attemptHashReconstruction(file *os.File, offset int64, lastGoodHash [32]byte) *RecoveredRecord {
	_, err := file.Seek(offset, 0)
	if err != nil {
		return nil
	}

	// Read a larger buffer for comprehensive analysis
	buffer := make([]byte, min(int(r.maxRecordSize), 64*1024))
	n, err := file.Read(buffer)
	if err != nil || n < 100 {
		return nil
	}

	// Strategy 1: Find records with matching hash chains
	for i := 0; i < n-100; i++ {
		if binary.LittleEndian.Uint32(buffer[i:]) != MagicHeader {
			continue
		}

		// Found potential header
		if i+72 > n {
			continue
		}

		// Extract header fields
		version := binary.LittleEndian.Uint16(buffer[i+4:])
		flags := binary.LittleEndian.Uint16(buffer[i+6:])
		length := binary.LittleEndian.Uint32(buffer[i+8:])
		timestamp := binary.LittleEndian.Uint64(buffer[i+12:])
		headerCRC := binary.LittleEndian.Uint32(buffer[i+20:])

		// Validate header structure
		// #nosec G115 - validated max size
		if version != Version || length > uint32(r.maxRecordSize) {
			continue
		}

		// Calculate expected record size
		recordSize := 24 + 8 + 32 + int(length) + 4 + 4
		if i+recordSize > n {
			continue
		}

		// Extract the full potential record
		potentialRecord := buffer[i : i+recordSize]

		// Try to validate hash chain
		if len(potentialRecord) > 32 {
			// Extract previous hash from record
			var prevHash [32]byte
			copy(prevHash[:], potentialRecord[32:64])

			// Check if this record chains from our last good hash
			if prevHash == lastGoodHash {
				// Perfect match! This record continues our chain
				return r.reconstructRecord(potentialRecord, true)
			}

			// Check if this record could be part of a fork
			if r.isValidHashChainFork(prevHash, lastGoodHash) {
				// This could be a valid fork in the chain
				return r.reconstructRecord(potentialRecord, false)
			}
		}

		// Strategy 2: CRC-based reconstruction
		if r.attemptCRCReconstruction(potentialRecord, headerCRC) {
			return r.reconstructRecord(potentialRecord, false)
		}

		// Strategy 3: Pattern-based reconstruction
		if r.attemptPatternReconstruction(potentialRecord, timestamp, flags) {
			return r.reconstructRecord(potentialRecord, false)
		}
	}

	// Strategy 4: Deep forensic recovery - scan for valid JSON events
	return r.attemptDeepForensicRecovery(buffer, n)
}

// reconstructRecord creates a RecoveredRecord from raw bytes
func (r *RecoveryEngine) reconstructRecord(data []byte, chainValid bool) *RecoveredRecord {
	if len(data) < 72 {
		return nil
	}

	// Extract fields
	length := binary.LittleEndian.Uint32(data[8:12])
	timestamp := binary.LittleEndian.Uint64(data[12:20])

	// Extract sequence if possible
	var sequence uint64
	if len(data) > 32 {
		sequence = binary.LittleEndian.Uint64(data[24:32])
	}

	// Extract event data
	eventDataStart := 24 + 8 + 32 // header + sequence + prevhash
	eventDataEnd := eventDataStart + int(length)
	if eventDataEnd > len(data) {
		eventDataEnd = len(data)
	}

	eventData := data[eventDataStart:eventDataEnd]

	return &RecoveredRecord{
		EventData:      eventData,
		Sequence:       sequence,
		Timestamp:      timestamp,
		BytesRead:      len(data),
		HashChainValid: chainValid,
	}
}

// isValidHashChainFork checks if a hash could be part of a valid fork
func (r *RecoveryEngine) isValidHashChainFork(prevHash, lastGoodHash [32]byte) bool {
	// Check if we've seen this hash before in our chain map
	for _, knownHash := range r.hashChains {
		if knownHash == prevHash {
			return true
		}
	}

	// Check if the hashes are related (share common prefix - indicating possible fork)
	commonPrefix := 0
	for i := 0; i < 32; i++ {
		if prevHash[i] == lastGoodHash[i] {
			commonPrefix++
		} else {
			break
		}
	}

	// If hashes share significant prefix, could be a fork
	return commonPrefix >= 8
}

// attemptCRCReconstruction validates record using CRC checks
func (r *RecoveryEngine) attemptCRCReconstruction(data []byte, expectedHeaderCRC uint32) bool {
	if len(data) < 24 {
		return false
	}

	// Verify header CRC
	headerBytes := data[:20]
	actualCRC := crc32.ChecksumIEEE(headerBytes)

	if actualCRC != expectedHeaderCRC {
		// Try to fix single-bit errors
		for i := 0; i < 20; i++ {
			for bit := 0; bit < 8; bit++ {
				// Flip bit
				testData := make([]byte, 20)
				copy(testData, headerBytes)
				testData[i] ^= (1 << bit)

				if crc32.ChecksumIEEE(testData) == expectedHeaderCRC {
					// Found the error! Fix it
					copy(data[:20], testData)
					return true
				}
			}
		}
		return false
	}

	// Also check data CRC if available
	if len(data) > 28 {
		length := binary.LittleEndian.Uint32(data[8:12])
		expectedEnd := 24 + 8 + 32 + int(length) + 4 + 4
		if expectedEnd <= len(data) {
			// Check for valid footer
			footerOffset := expectedEnd - 4
			if footerOffset > 0 && footerOffset < len(data)-3 {
				footer := binary.LittleEndian.Uint32(data[footerOffset:])
				return footer == MagicFooter
			}
		}
	}

	return true
}

// attemptPatternReconstruction uses pattern matching to validate records
func (r *RecoveryEngine) attemptPatternReconstruction(data []byte, timestamp uint64, flags uint16) bool {
	// Check timestamp is reasonable (within last 10 years)
	// #nosec G115 - timestamp conversion
	now := uint64(time.Now().UnixNano())
	// #nosec G115 - time duration
	tenYearsAgo := now - uint64(10*365*24*time.Hour.Nanoseconds())

	// #nosec G115 - time duration conversion for validation
	if timestamp < tenYearsAgo || timestamp > now+uint64(24*time.Hour.Nanoseconds()) {
		return false
	}

	// Check flags are valid
	validFlags := RecordFlagDeleted | RecordFlagCompacted
	if flags & ^uint16(validFlags) != 0 {
		return false
	}

	// Check if event data looks like JSON
	if len(data) > 72 {
		length := binary.LittleEndian.Uint32(data[8:12])
		eventStart := 24 + 8 + 32
		if eventStart+int(length) <= len(data) {
			eventData := data[eventStart : eventStart+int(length)]
			if len(eventData) > 0 {
				// Check for JSON structure
				firstChar := eventData[0]
				lastChar := eventData[len(eventData)-1]
				isJSON := (firstChar == '{' && lastChar == '}') ||
					(firstChar == '[' && lastChar == ']')

				if isJSON {
					// Try to validate JSON
					var test interface{}
					if json.Unmarshal(eventData, &test) == nil {
						return true
					}
				}
			}
		}
	}

	return false
}

// attemptDeepForensicRecovery performs deep analysis to recover records
func (r *RecoveryEngine) attemptDeepForensicRecovery(buffer []byte, n int) *RecoveredRecord {
	// Scan for JSON patterns that might be event data
	for i := 0; i < n-10; i++ {
		if buffer[i] == '{' {
			// Found potential JSON start
			depth := 1
			j := i + 1

			for j < n && depth > 0 {
				switch buffer[j] {
				case '{':
					depth++
				case '}':
					depth--
				}
				j++
			}

			if depth == 0 && j-i > 10 {
				// Found complete JSON object
				jsonData := buffer[i:j]

				// Validate it's a log event
				var event core.LogEvent
				if err := json.Unmarshal(jsonData, &event); err == nil {
					// Successfully parsed as log event!
					// Reconstruct a minimal record
					return &RecoveredRecord{
						EventData: jsonData,
						Sequence:  0, // Unknown
						// #nosec G115 - timestamp conversion
						Timestamp:      uint64(event.Timestamp.UnixNano()),
						BytesRead:      j - i,
						HashChainValid: false,
					}
				}
			}
		}
	}

	return nil
}

// recoverPartialRecords attempts to recover partially written records
func (r *RecoveryEngine) recoverPartialRecords() [][]byte {
	var partialRecords [][]byte

	file, err := os.Open(r.path)
	if err != nil {
		return partialRecords
	}
	defer func() { _ = file.Close() }()

	// Read file in chunks and look for partial records at boundaries
	stat, _ := file.Stat()
	fileSize := stat.Size()

	// Check last 10KB for partial records
	if fileSize > 10240 {
		_, _ = file.Seek(fileSize-10240, 0)
		buffer := make([]byte, 10240)
		n, _ := file.Read(buffer)

		// Scan for incomplete records
		for i := 0; i < n-100; i++ {
			if r.looksLikePartialRecord(buffer[i:n]) {
				if data := r.extractPartialData(buffer[i:n]); data != nil {
					partialRecords = append(partialRecords, data)
				}
			}
		}
	}

	return partialRecords
}

// looksLikePartialRecord checks for partial record patterns
func (r *RecoveryEngine) looksLikePartialRecord(data []byte) bool {
	// Check for record patterns that are incomplete
	if len(data) < 24 {
		return false
	}

	// Has valid header but no footer
	hasHeader := binary.LittleEndian.Uint32(data) == MagicHeader
	hasFooter := false

	if len(data) > 28 {
		// Check if there's a footer where expected
		length := binary.LittleEndian.Uint32(data[8:])
		expectedFooterPos := 24 + 8 + 32 + int(length) + 4
		if expectedFooterPos+4 <= len(data) {
			hasFooter = binary.LittleEndian.Uint32(data[expectedFooterPos:]) == MagicFooter
		}
	}

	return hasHeader && !hasFooter
}

// extractPartialData attempts to extract usable data from a partial record
func (r *RecoveryEngine) extractPartialData(data []byte) []byte {
	if len(data) < 72 {
		return nil
	}

	// Try to extract event data portion
	length := binary.LittleEndian.Uint32(data[8:])
	// #nosec G115 - data length check
	if length > 0 && length < uint32(len(data)-72) {
		eventData := data[72 : 72+length]

		// Validate it looks like JSON
		if len(eventData) > 0 && (eventData[0] == '{' || eventData[0] == '[') {
			return eventData
		}
	}

	return nil
}

// recoverFromShadow attempts to recover from shadow copies
func (r *RecoveryEngine) recoverFromShadow() [][]byte {
	var shadowRecords [][]byte

	shadowPath := r.path + ".shadow"
	if _, err := os.Stat(shadowPath); err != nil {
		// No shadow file
		return shadowRecords
	}

	// Create recovery engine for shadow file
	shadowEngine := NewRecoveryEngine(shadowPath,
		WithSkipCorrupted(true),
		WithChecksumVerification(false), // Shadow might have relaxed checksums
	)

	_, records, err := shadowEngine.Recover()
	if err == nil {
		shadowRecords = records
	}

	return shadowRecords
}
