// Package wal implements a bulletproof Write-Ahead Log for audit logging.
package wal

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"

	"github.com/willibrandon/mtlog/core"
)

const (
	// MagicHeader identifies the start of a WAL record
	MagicHeader = 0x4D544C47 // "MTLG" in hex
	// MagicFooter identifies the end of a WAL record
	MagicFooter = 0x454E4452 // "ENDR" in hex
	// Version is the WAL format version
	Version = 1

	// RecordFlagDeleted marks a record as deleted.
	RecordFlagDeleted = 1 << 0 // Record has been marked for deletion
)

// Record represents a single entry in the WAL.
type Record struct {
	// Header
	Magic       uint32
	Version     uint16
	Flags       uint16
	Length      uint32
	Timestamp   int64
	CRC32Header uint32

	// Payload
	Sequence  uint64
	PrevHash  [32]byte
	EventData []byte

	// Footer
	CRC32Data uint32
	MagicEnd  uint32
}

// NewRecord creates a new WAL record from a log event.
func NewRecord(event *core.LogEvent, sequence uint64, prevHash [32]byte) (*Record, error) {
	// Serialize the event to JSON
	eventData, err := json.Marshal(event)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal event: %w", err)
	}

	r := &Record{
		Magic:   MagicHeader,
		Version: Version,
		Flags:   0,
		// #nosec G115 - event data length validated against max record size
		Length:    uint32(len(eventData)),
		Timestamp: event.Timestamp.UnixNano(),
		Sequence:  sequence,
		PrevHash:  prevHash,
		EventData: eventData,
		MagicEnd:  MagicFooter,
	}

	return r, nil
}

// Marshal serializes the record to bytes with CRC protection.
func (r *Record) Marshal() ([]byte, error) {
	buf := new(bytes.Buffer)

	// Write header
	if err := binary.Write(buf, binary.LittleEndian, r.Magic); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.LittleEndian, r.Version); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.LittleEndian, r.Flags); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.LittleEndian, r.Length); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.LittleEndian, r.Timestamp); err != nil {
		return nil, err
	}

	// Calculate header CRC (excluding the CRC field itself)
	headerBytes := buf.Bytes()
	r.CRC32Header = crc32.ChecksumIEEE(headerBytes)
	if err := binary.Write(buf, binary.LittleEndian, r.CRC32Header); err != nil {
		return nil, err
	}

	// Write payload
	if err := binary.Write(buf, binary.LittleEndian, r.Sequence); err != nil {
		return nil, err
	}
	if _, err := buf.Write(r.PrevHash[:]); err != nil {
		return nil, err
	}
	if _, err := buf.Write(r.EventData); err != nil {
		return nil, err
	}

	// Calculate data CRC (entire record so far)
	allBytes := buf.Bytes()
	r.CRC32Data = crc32.ChecksumIEEE(allBytes)
	if err := binary.Write(buf, binary.LittleEndian, r.CRC32Data); err != nil {
		return nil, err
	}

	// Write footer magic
	if err := binary.Write(buf, binary.LittleEndian, r.MagicEnd); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// UnmarshalRecord deserializes a record from bytes with CRC verification.
func UnmarshalRecord(data []byte) (*Record, error) {
	if len(data) < 32 { // Minimum size for header
		return nil, fmt.Errorf("data too short for valid record")
	}

	r := &Record{}
	buf := bytes.NewReader(data)

	// Read header
	if err := binary.Read(buf, binary.LittleEndian, &r.Magic); err != nil {
		return nil, err
	}
	if r.Magic != MagicHeader {
		return nil, fmt.Errorf("invalid magic header: %x", r.Magic)
	}

	if err := binary.Read(buf, binary.LittleEndian, &r.Version); err != nil {
		return nil, err
	}
	if err := binary.Read(buf, binary.LittleEndian, &r.Flags); err != nil {
		return nil, err
	}
	if err := binary.Read(buf, binary.LittleEndian, &r.Length); err != nil {
		return nil, err
	}
	if err := binary.Read(buf, binary.LittleEndian, &r.Timestamp); err != nil {
		return nil, err
	}
	if err := binary.Read(buf, binary.LittleEndian, &r.CRC32Header); err != nil {
		return nil, err
	}

	// Verify header CRC
	headerBytes := data[:20] // Header without CRC field
	expectedCRC := crc32.ChecksumIEEE(headerBytes)
	if r.CRC32Header != expectedCRC {
		return nil, fmt.Errorf("header CRC mismatch: expected %x, got %x", expectedCRC, r.CRC32Header)
	}

	// Read payload
	if err := binary.Read(buf, binary.LittleEndian, &r.Sequence); err != nil {
		return nil, err
	}
	if _, err := buf.Read(r.PrevHash[:]); err != nil {
		return nil, err
	}

	r.EventData = make([]byte, r.Length)
	if _, err := buf.Read(r.EventData); err != nil {
		return nil, err
	}

	// Read footer
	if err := binary.Read(buf, binary.LittleEndian, &r.CRC32Data); err != nil {
		return nil, err
	}
	if err := binary.Read(buf, binary.LittleEndian, &r.MagicEnd); err != nil {
		return nil, err
	}

	if r.MagicEnd != MagicFooter {
		return nil, fmt.Errorf("invalid magic footer: %x", r.MagicEnd)
	}

	// Verify data CRC
	endOfData := len(data) - 8 // Exclude CRC32Data and MagicEnd
	dataBytes := data[:endOfData]
	expectedDataCRC := crc32.ChecksumIEEE(dataBytes)
	if r.CRC32Data != expectedDataCRC {
		return nil, fmt.Errorf("data CRC mismatch: expected %x, got %x", expectedDataCRC, r.CRC32Data)
	}

	return r, nil
}

// ComputeHash calculates the SHA256 hash of the record for chaining.
func (r *Record) ComputeHash() [32]byte {
	data, _ := r.Marshal()
	return sha256.Sum256(data)
}

// GetEvent deserializes the event data back to a LogEvent.
func (r *Record) GetEvent() (*core.LogEvent, error) {
	var event core.LogEvent
	if err := json.Unmarshal(r.EventData, &event); err != nil {
		return nil, fmt.Errorf("failed to unmarshal event: %w", err)
	}
	return &event, nil
}

// UnmarshalRecordFromBytes unmarshals a record from bytes and returns bytes read
func UnmarshalRecordFromBytes(data []byte) (*Record, int, error) {
	if len(data) < 24 {
		return nil, 0, fmt.Errorf("insufficient data for record header")
	}

	// Check magic header
	magic := binary.LittleEndian.Uint32(data)
	if magic != MagicHeader {
		return nil, 0, fmt.Errorf("invalid magic header")
	}

	// Read length to determine total size
	length := binary.LittleEndian.Uint32(data[8:12])
	totalSize := 24 + 8 + 32 + int(length) + 4 + 4 // header + sequence + hash + data + crc + footer

	if len(data) < totalSize {
		return nil, 0, fmt.Errorf("insufficient data for full record")
	}

	recordData := data[:totalSize]
	record, err := UnmarshalRecord(recordData)
	if err != nil {
		return nil, 0, err
	}

	return record, totalSize, nil
}
