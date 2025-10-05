package wal

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/willibrandon/mtlog/core"
)

func TestRecoveryEngine_RecoverValidWAL(t *testing.T) {
	// Create a temporary WAL with valid records
	dir := t.TempDir()
	walPath := filepath.Join(dir, "test.wal")

	wal, err := New(walPath)
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}

	// Write some test events
	events := []string{"event1", "event2", "event3"}
	for i, msg := range events {
		event := &core.LogEvent{
			Level:           core.InformationLevel,
			MessageTemplate: msg,
			Properties:      map[string]interface{}{"index": i},
		}
		if err := wal.Write(event); err != nil {
			t.Fatalf("Failed to write event: %v", err)
		}
	}
	wal.Close()

	// Now recover
	engine := NewRecoveryEngine(walPath)
	report, records, err := engine.Recover()

	if err != nil {
		t.Fatalf("Recovery failed: %v", err)
	}

	if report.RecoveredRecords != len(events) {
		t.Errorf("Expected %d recovered records, got %d", len(events), report.RecoveredRecords)
	}

	if report.CorruptedRecords != 0 {
		t.Errorf("Expected 0 corrupted records, got %d", report.CorruptedRecords)
	}

	if len(records) != len(events) {
		t.Errorf("Expected %d records, got %d", len(events), len(records))
	}
}

func TestRecoveryEngine_RecoverCorruptedWAL(t *testing.T) {
	// Create a WAL with some valid records and corruption
	dir := t.TempDir()
	walPath := filepath.Join(dir, "corrupted.wal")

	file, err := os.Create(walPath)
	if err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	// Write a valid record
	validRecord := createValidRecord(1, []byte("valid event"))
	t.Logf("Valid record 1 size: %d bytes", len(validRecord))
	t.Logf("First 4 bytes of record 1: %x", validRecord[:4])
	n, err := file.Write(validRecord)
	if err != nil || n != len(validRecord) {
		t.Fatalf("Failed to write valid record 1: %v", err)
	}

	// Write garbage (corruption)
	corruption := []byte("CORRUPTED DATA HERE!!!")
	n, err = file.Write(corruption)
	if err != nil || n != len(corruption) {
		t.Fatalf("Failed to write corruption: %v", err)
	}

	// Write another valid record
	validRecord2 := createValidRecord(2, []byte("another valid event"))
	n, err = file.Write(validRecord2)
	if err != nil || n != len(validRecord2) {
		t.Fatalf("Failed to write valid record 2: %v", err)
	}

	// Sync and close
	file.Sync()
	file.Close()

	// Verify file was written
	stat, err := os.Stat(walPath)
	if err != nil {
		t.Fatalf("Failed to stat file: %v", err)
	}
	t.Logf("Created corrupted WAL file with %d bytes", stat.Size())

	// Recover with skip corrupted enabled
	engine := NewRecoveryEngine(walPath, WithSkipCorrupted(true))
	report, records, err := engine.Recover()

	// Log errors for debugging
	for _, e := range report.Errors {
		t.Logf("Recovery error: %v", e)
	}

	if err != nil {
		t.Fatalf("Recovery failed: %v", err)
	}

	// Should recover at least 1 record (first valid one)
	if report.RecoveredRecords < 1 {
		t.Errorf("Expected at least 1 recovered record, got %d", report.RecoveredRecords)
	}

	if report.CorruptedRecords == 0 {
		t.Errorf("Expected corrupted records to be detected")
	}

	if report.SkippedBytes == 0 {
		t.Errorf("Expected some bytes to be skipped")
	}

	t.Logf("Recovery report: recovered=%d, corrupted=%d, skipped=%d bytes",
		report.RecoveredRecords, report.CorruptedRecords, report.SkippedBytes)

	// Verify we got some valid data back
	if len(records) == 0 {
		t.Errorf("Expected to recover some records")
	}
}

func TestRecoveryEngine_RepairWAL(t *testing.T) {
	// Create a corrupted WAL
	dir := t.TempDir()
	corruptedPath := filepath.Join(dir, "corrupted.wal")
	repairedPath := filepath.Join(dir, "repaired.wal")

	file, err := os.Create(corruptedPath)
	if err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	// Write valid records with corruption in between
	validRecord1 := createValidRecord(1, []byte(`{"message":"event1"}`))
	n, err := file.Write(validRecord1)
	if err != nil || n != len(validRecord1) {
		t.Fatalf("Failed to write valid record 1: %v", err)
	}

	// Corruption
	corruption := []byte("XXXCORRUPTEDXXX")
	n, err = file.Write(corruption)
	if err != nil || n != len(corruption) {
		t.Fatalf("Failed to write corruption: %v", err)
	}

	validRecord2 := createValidRecord(2, []byte(`{"message":"event2"}`))
	n, err = file.Write(validRecord2)
	if err != nil || n != len(validRecord2) {
		t.Fatalf("Failed to write valid record 2: %v", err)
	}

	// Sync and close
	file.Sync()
	file.Close()

	// Verify file was written
	stat, err := os.Stat(corruptedPath)
	if err != nil {
		t.Fatalf("Failed to stat file: %v", err)
	}
	t.Logf("Created corrupted WAL file with %d bytes", stat.Size())

	// Repair the WAL
	engine := NewRecoveryEngine(corruptedPath, WithSkipCorrupted(true))
	err = engine.RepairWAL(repairedPath)

	if err != nil {
		t.Fatalf("Repair failed: %v", err)
	}

	// Verify repaired WAL exists
	if _, err := os.Stat(repairedPath); os.IsNotExist(err) {
		t.Errorf("Repaired WAL was not created")
	}

	// Try to read from repaired WAL
	repairedEngine := NewRecoveryEngine(repairedPath)
	report, records, err := repairedEngine.Recover()

	if err != nil {
		t.Fatalf("Failed to read repaired WAL: %v", err)
	}

	if report.CorruptedRecords != 0 {
		t.Errorf("Repaired WAL should have no corruption")
	}

	if len(records) == 0 {
		t.Errorf("Repaired WAL should contain recovered records")
	}

	t.Logf("Repaired WAL contains %d valid records", len(records))
}

func TestRecoveryEngine_MarshalUnmarshalRoundtrip(t *testing.T) {
	// Test that a record created with Marshal can be read by recovery
	eventData := []byte(`{"test":"data","value":123}`)
	record := &Record{
		Magic:     MagicHeader,
		Version:   Version,
		Flags:     0,
		Length:    uint32(len(eventData)),
		Timestamp: time.Now().UnixNano(),
		Sequence:  42,
		PrevHash:  [32]byte{}, // zeros
		EventData: eventData,
		MagicEnd:  MagicFooter,
	}

	// Marshal the record
	marshaled, err := record.Marshal()
	if err != nil {
		t.Fatalf("Failed to marshal record: %v", err)
	}

	// Write to a temp file
	dir := t.TempDir()
	walPath := filepath.Join(dir, "roundtrip.wal")
	if err := os.WriteFile(walPath, marshaled, 0600); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	// Try to recover it
	engine := NewRecoveryEngine(walPath)
	report, records, err := engine.Recover()

	if err != nil {
		t.Fatalf("Recovery failed: %v", err)
	}

	if report.RecoveredRecords != 1 {
		t.Errorf("Expected 1 recovered record, got %d", report.RecoveredRecords)
	}

	if len(records) != 1 {
		t.Fatalf("Expected 1 record, got %d", len(records))
	}

	// Verify the data matches
	if !bytes.Equal(records[0], eventData) {
		t.Errorf("Recovered data doesn't match original.\nExpected: %s\nGot: %s",
			string(eventData), string(records[0]))
	}

	if report.LastGoodSequence != 42 {
		t.Errorf("Expected sequence 42, got %d", report.LastGoodSequence)
	}
}

// Helper function to create a valid WAL record matching the EXACT format from record.go
func createValidRecord(sequence uint64, eventData []byte) []byte {
	var buf bytes.Buffer

	timestamp := time.Now().UnixNano()

	// Write in EXACT order from Marshal()
	// 1. Magic (4 bytes)
	binary.Write(&buf, binary.LittleEndian, uint32(MagicHeader))

	// 2. Version (2 bytes)
	binary.Write(&buf, binary.LittleEndian, uint16(Version))

	// 3. Flags (2 bytes)
	binary.Write(&buf, binary.LittleEndian, uint16(0))

	// 4. Length (4 bytes) - This is EventData length, NOT total!
	binary.Write(&buf, binary.LittleEndian, uint32(len(eventData)))

	// 5. Timestamp (8 bytes)
	binary.Write(&buf, binary.LittleEndian, timestamp)

	// Calculate header CRC over first 20 bytes (Magic through Timestamp)
	headerBytes := buf.Bytes()
	crc32Header := crc32.ChecksumIEEE(headerBytes)

	// 6. CRC32Header (4 bytes)
	binary.Write(&buf, binary.LittleEndian, crc32Header)

	// 7. Sequence (8 bytes)
	binary.Write(&buf, binary.LittleEndian, sequence)

	// 8. PrevHash (32 bytes of zeros)
	var prevHash [32]byte
	buf.Write(prevHash[:])

	// 9. EventData (variable)
	buf.Write(eventData)

	// Calculate data CRC over everything so far
	allBytes := buf.Bytes()
	crc32Data := crc32.ChecksumIEEE(allBytes)

	// 10. CRC32Data (4 bytes)
	binary.Write(&buf, binary.LittleEndian, crc32Data)

	// 11. MagicEnd (4 bytes)
	binary.Write(&buf, binary.LittleEndian, uint32(MagicFooter))

	return buf.Bytes()
}
