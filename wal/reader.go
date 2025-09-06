package wal

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"time"

	"github.com/willibrandon/mtlog/core"
)

// Reader reads events from a WAL file
type Reader struct {
	file   *os.File
	offset int64
}

// NewReader creates a new WAL reader
func NewReader(path string) (*Reader, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open WAL: %w", err)
	}

	return &Reader{
		file: file,
	}, nil
}

// ReadAll reads all events from the WAL
func (r *Reader) ReadAll() ([]*core.LogEvent, error) {
	var events []*core.LogEvent
	var skippedCount int

	for {
		event, err := r.ReadNext()
		if err == io.EOF {
			break
		}
		if err != nil {
			// Skip corrupted records but count them
			skippedCount++
			// If we get too many errors in a row at the start, return the error
			if len(events) == 0 && skippedCount > 10 {
				return nil, fmt.Errorf("too many read errors: %w", err)
			}
			continue
		}
		events = append(events, event)
	}

	return events, nil
}

// ReadNext reads the next event from the WAL
func (r *Reader) ReadNext() (*core.LogEvent, error) {
	// Read magic number
	var magic uint32
	if err := binary.Read(r.file, binary.LittleEndian, &magic); err != nil {
		if err == io.EOF {
			return nil, io.EOF
		}
		return nil, fmt.Errorf("failed to read magic: %w", err)
	}

	// Check for end marker
	if magic == MagicFooter {
		return nil, io.EOF
	}

	// Verify magic number
	if magic != MagicHeader {
		return nil, fmt.Errorf("invalid magic number: %x", magic)
	}

	// Read rest of header
	var version uint16
	var flags uint16
	var length uint32
	var timestamp int64
	var crc32Header uint32

	if err := binary.Read(r.file, binary.LittleEndian, &version); err != nil {
		return nil, fmt.Errorf("failed to read version: %w", err)
	}
	if err := binary.Read(r.file, binary.LittleEndian, &flags); err != nil {
		return nil, fmt.Errorf("failed to read flags: %w", err)
	}
	if err := binary.Read(r.file, binary.LittleEndian, &length); err != nil {
		return nil, fmt.Errorf("failed to read length: %w", err)
	}
	if err := binary.Read(r.file, binary.LittleEndian, &timestamp); err != nil {
		return nil, fmt.Errorf("failed to read timestamp: %w", err)
	}
	if err := binary.Read(r.file, binary.LittleEndian, &crc32Header); err != nil {
		return nil, fmt.Errorf("failed to read header CRC: %w", err)
	}

	// Sanity check
	if length > 1024*1024 { // Max 1MB per record
		return nil, fmt.Errorf("invalid record length: %d", length)
	}

	// Read sequence and prevHash
	var sequence uint64
	if err := binary.Read(r.file, binary.LittleEndian, &sequence); err != nil {
		return nil, fmt.Errorf("failed to read sequence: %w", err)
	}

	var prevHash [32]byte
	if _, err := io.ReadFull(r.file, prevHash[:]); err != nil {
		return nil, fmt.Errorf("failed to read prevHash: %w", err)
	}

	// Read event data
	eventData := make([]byte, length)
	if _, err := io.ReadFull(r.file, eventData); err != nil {
		return nil, fmt.Errorf("failed to read event data: %w", err)
	}

	// Read data CRC and magic end
	var crc32Data uint32
	var magicEnd uint32
	if err := binary.Read(r.file, binary.LittleEndian, &crc32Data); err != nil {
		return nil, fmt.Errorf("failed to read data CRC: %w", err)
	}
	if err := binary.Read(r.file, binary.LittleEndian, &magicEnd); err != nil {
		return nil, fmt.Errorf("failed to read magic end: %w", err)
	}

	// Build the same byte sequence that was used to calculate CRC in the writer
	// The writer calculates CRC over the entire record up to the CRC field
	var crcBuf []byte
	// Magic header
	magicBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(magicBytes, magic)
	crcBuf = append(crcBuf, magicBytes...)
	// Version
	versionBytes := make([]byte, 2)
	binary.LittleEndian.PutUint16(versionBytes, version)
	crcBuf = append(crcBuf, versionBytes...)
	// Flags
	flagsBytes := make([]byte, 2)
	binary.LittleEndian.PutUint16(flagsBytes, flags)
	crcBuf = append(crcBuf, flagsBytes...)
	// Length
	lengthBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(lengthBytes, length)
	crcBuf = append(crcBuf, lengthBytes...)
	// Timestamp
	timestampBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(timestampBytes, uint64(timestamp))
	crcBuf = append(crcBuf, timestampBytes...)
	// CRC32Header
	crc32HeaderBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(crc32HeaderBytes, crc32Header)
	crcBuf = append(crcBuf, crc32HeaderBytes...)
	// Sequence
	sequenceBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(sequenceBytes, sequence)
	crcBuf = append(crcBuf, sequenceBytes...)
	// PrevHash
	crcBuf = append(crcBuf, prevHash[:]...)
	// EventData
	crcBuf = append(crcBuf, eventData...)

	// Verify data CRC
	calculatedCRC := crc32.ChecksumIEEE(crcBuf)
	if calculatedCRC != crc32Data {
		return nil, fmt.Errorf("data CRC mismatch: expected %x, got %x", crc32Data, calculatedCRC)
	}

	// Verify magic end
	if magicEnd != MagicFooter {
		return nil, fmt.Errorf("invalid magic footer: %x", magicEnd)
	}

	// Parse event
	var event core.LogEvent
	if err := json.Unmarshal(eventData, &event); err != nil {
		return nil, fmt.Errorf("failed to unmarshal event: %w", err)
	}

	// Update offset
	headerSize := 4 + 2 + 2 + 4 + 8 + 4 // magic + version + flags + length + timestamp + crc32
	payloadSize := 8 + 32 + int(length) + 4 + 4 // sequence + prevHash + eventData + crc32Data + magicEnd
	r.offset += int64(headerSize + payloadSize)
	
	return &event, nil
}

// ReadRange reads events within a time range
func (r *Reader) ReadRange(start, end time.Time) ([]*core.LogEvent, error) {
	var events []*core.LogEvent

	for {
		event, err := r.ReadNext()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue // Skip bad records
		}

		// Filter by time range
		if !start.IsZero() && event.Timestamp.Before(start) {
			continue
		}
		if !end.IsZero() && event.Timestamp.After(end) {
			continue
		}

		events = append(events, event)
	}

	return events, nil
}

// Seek seeks to a specific offset in the WAL
func (r *Reader) Seek(offset int64, whence int) (int64, error) {
	newOffset, err := r.file.Seek(offset, whence)
	if err != nil {
		return 0, fmt.Errorf("failed to seek: %w", err)
	}
	r.offset = newOffset
	return newOffset, nil
}

// Close closes the reader
func (r *Reader) Close() error {
	return r.file.Close()
}

// GetOffset returns the current offset
func (r *Reader) GetOffset() int64 {
	return r.offset
}