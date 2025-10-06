package wal

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestDoubleWriteBuffer_WriteAndRecover(t *testing.T) {
	dir := t.TempDir()
	journalPath := filepath.Join(dir, "test.journal")

	// Create journal file
	// #nosec G304 - test file path from TempDir
	journal, err := os.OpenFile(journalPath, os.O_CREATE|os.O_RDWR|os.O_SYNC, 0o600)
	if err != nil {
		t.Fatalf("Failed to create journal: %v", err)
	}
	defer func() { _ = journal.Close() }()

	// Create double-write buffer
	dwb, err := NewDoubleWriteBuffer(journal, 4096)
	if err != nil {
		t.Fatalf("Failed to create double-write buffer: %v", err)
	}

	// Write test data
	testData := []byte("This is test data for torn-write protection")
	position := int64(1000)

	err = dwb.WriteToJournal(testData, position, true)
	if err != nil {
		t.Fatalf("Failed to write to journal: %v", err)
	}

	// Mark as complete
	err = dwb.MarkComplete(true)
	if err != nil {
		t.Fatalf("Failed to mark complete: %v", err)
	}

	// Simulate recovery - read incomplete entries
	incomplete, err := dwb.RecoverIncomplete()
	if err != nil {
		t.Fatalf("Failed to recover: %v", err)
	}

	// Should have no incomplete entries since we marked complete
	if len(incomplete) != 0 {
		t.Errorf("Expected 0 incomplete entries, got %d", len(incomplete))
	}
}

func TestDoubleWriteBuffer_RecoverIncomplete(t *testing.T) {
	dir := t.TempDir()
	journalPath := filepath.Join(dir, "test.journal")

	// Create and write without marking complete
	// #nosec G304 - test file path from TempDir
	journal, err := os.OpenFile(journalPath, os.O_CREATE|os.O_RDWR|os.O_SYNC, 0o600)
	if err != nil {
		t.Fatalf("Failed to create journal: %v", err)
	}

	dwb, err := NewDoubleWriteBuffer(journal, 4096)
	if err != nil {
		t.Fatalf("Failed to create double-write buffer: %v", err)
	}

	// Write multiple entries
	entries := []struct {
		data     []byte
		position int64
	}{
		{[]byte("First entry"), 0},
		{[]byte("Second entry"), 100},
		{[]byte("Third entry"), 200},
	}

	for _, entry := range entries {
		err = dwb.WriteToJournal(entry.data, entry.position, true)
		if err != nil {
			t.Fatalf("Failed to write entry: %v", err)
		}
	}

	// Don't mark complete to simulate crash
	_ = journal.Close()

	// Reopen and recover
	// #nosec G304 - test file path from TempDir
	journal2, err := os.OpenFile(journalPath, os.O_RDWR|os.O_SYNC, 0o600)
	if err != nil {
		t.Fatalf("Failed to reopen journal: %v", err)
	}
	defer func() { _ = journal2.Close() }()

	dwb2, err := NewDoubleWriteBuffer(journal2, 4096)
	if err != nil {
		t.Fatalf("Failed to create recovery buffer: %v", err)
	}

	// Recover incomplete entries
	incomplete, err := dwb2.RecoverIncomplete()
	if err != nil {
		t.Fatalf("Failed to recover incomplete: %v", err)
	}

	// Should have all 3 incomplete entries
	if len(incomplete) != 3 {
		t.Errorf("Expected 3 incomplete entries, got %d", len(incomplete))
	}

	// Verify recovered data
	for i, entry := range incomplete {
		expectedData := entries[i].data
		if !bytes.Equal(entry.Data, expectedData) {
			t.Errorf("Entry %d: expected %q, got %q", i, expectedData, entry.Data)
		}
		if entry.Position != entries[i].position {
			t.Errorf("Entry %d: expected position %d, got %d",
				i, entries[i].position, entry.Position)
		}
	}
}

func TestDoubleWriteBuffer_Clear(t *testing.T) {
	dir := t.TempDir()
	journalPath := filepath.Join(dir, "test.journal")

	// #nosec G304 - test file path from TempDir
	journal, err := os.OpenFile(journalPath, os.O_CREATE|os.O_RDWR|os.O_SYNC, 0o600)
	if err != nil {
		t.Fatalf("Failed to create journal: %v", err)
	}
	defer func() { _ = journal.Close() }()

	dwb, err := NewDoubleWriteBuffer(journal, 4096)
	if err != nil {
		t.Fatalf("Failed to create double-write buffer: %v", err)
	}

	// Write some data
	err = dwb.WriteToJournal([]byte("Test data"), 0, true)
	if err != nil {
		t.Fatalf("Failed to write: %v", err)
	}

	// Clear the journal
	err = dwb.Clear()
	if err != nil {
		t.Fatalf("Failed to clear: %v", err)
	}

	// Verify journal is empty
	incomplete, err := dwb.RecoverIncomplete()
	if err != nil {
		t.Fatalf("Failed to recover after clear: %v", err)
	}

	if len(incomplete) != 0 {
		t.Errorf("Expected empty journal after clear, got %d entries", len(incomplete))
	}
}

func TestDoubleWriteBuffer_Compact(t *testing.T) {
	dir := t.TempDir()
	journalPath := filepath.Join(dir, "test.journal")

	// #nosec G304 - test file path from TempDir
	journal, err := os.OpenFile(journalPath, os.O_CREATE|os.O_RDWR|os.O_SYNC, 0o600)
	if err != nil {
		t.Fatalf("Failed to create journal: %v", err)
	}
	defer func() { _ = journal.Close() }()

	dwb, err := NewDoubleWriteBuffer(journal, 4096)
	if err != nil {
		t.Fatalf("Failed to create double-write buffer: %v", err)
	}

	// Write and complete some entries
	for i := 0; i < 3; i++ {
		data := []byte("Entry " + string(rune('A'+i)))
		err = dwb.WriteToJournal(data, int64(i*100), true)
		if err != nil {
			t.Fatalf("Failed to write entry %d: %v", i, err)
		}
		if i < 2 {
			// Mark first two as complete
			err = dwb.MarkComplete(true)
			if err != nil {
				t.Fatalf("Failed to mark complete: %v", err)
			}
		}
	}

	// Compact should remove completed entries
	err = dwb.Compact()
	if err != nil {
		t.Fatalf("Failed to compact: %v", err)
	}

	// Should only have the last incomplete entry
	incomplete, err := dwb.RecoverIncomplete()
	if err != nil {
		t.Fatalf("Failed to recover after compact: %v", err)
	}

	if len(incomplete) != 1 {
		t.Errorf("Expected 1 incomplete entry after compact, got %d", len(incomplete))
	}
}

func TestDoubleWriteBuffer_CRCValidation(t *testing.T) {
	dir := t.TempDir()
	journalPath := filepath.Join(dir, "test.journal")

	// Create journal and write data
	// #nosec G304 - test file path from TempDir
	journal, err := os.OpenFile(journalPath, os.O_CREATE|os.O_RDWR|os.O_SYNC, 0o600)
	if err != nil {
		t.Fatalf("Failed to create journal: %v", err)
	}

	dwb, err := NewDoubleWriteBuffer(journal, 4096)
	if err != nil {
		t.Fatalf("Failed to create double-write buffer: %v", err)
	}

	testData := []byte("Data with CRC protection")
	err = dwb.WriteToJournal(testData, 0, true)
	if err != nil {
		t.Fatalf("Failed to write: %v", err)
	}
	_ = journal.Close()

	// Corrupt the journal file
	// #nosec G304 - test file path from TempDir
	journalData, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatalf("Failed to read journal: %v", err)
	}

	// Corrupt data portion (after header)
	if len(journalData) > 30 {
		journalData[30] ^= 0xFF // Flip bits
	}

	err = os.WriteFile(journalPath, journalData, 0o600)
	if err != nil {
		t.Fatalf("Failed to write corrupted journal: %v", err)
	}

	// Try to recover
	// #nosec G304 - test file path from TempDir
	journal2, err := os.OpenFile(journalPath, os.O_RDWR|os.O_SYNC, 0o600)
	if err != nil {
		t.Fatalf("Failed to reopen journal: %v", err)
	}
	defer func() { _ = journal2.Close() }()

	dwb2, err := NewDoubleWriteBuffer(journal2, 4096)
	if err != nil {
		t.Fatalf("Failed to create recovery buffer: %v", err)
	}

	// Should detect CRC mismatch
	_, err = dwb2.RecoverIncomplete()
	if err == nil {
		t.Error("Expected CRC validation to fail")
	}
}

func TestDoubleWriteBuffer_LargeData(t *testing.T) {
	dir := t.TempDir()
	journalPath := filepath.Join(dir, "test.journal")

	// #nosec G304 - test file path from TempDir
	journal, err := os.OpenFile(journalPath, os.O_CREATE|os.O_RDWR|os.O_SYNC, 0o600)
	if err != nil {
		t.Fatalf("Failed to create journal: %v", err)
	}
	defer func() { _ = journal.Close() }()

	// Use larger buffer size
	dwb, err := NewDoubleWriteBuffer(journal, 1024*1024) // 1MB
	if err != nil {
		t.Fatalf("Failed to create double-write buffer: %v", err)
	}

	// Write large data (100KB)
	largeData := make([]byte, 100*1024)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	err = dwb.WriteToJournal(largeData, 0, true)
	if err != nil {
		t.Fatalf("Failed to write large data: %v", err)
	}

	// Recover and verify
	incomplete, err := dwb.RecoverIncomplete()
	if err != nil {
		t.Fatalf("Failed to recover large data: %v", err)
	}

	if len(incomplete) != 1 {
		t.Fatalf("Expected 1 entry, got %d", len(incomplete))
	}

	if len(incomplete[0].Data) != len(largeData) {
		t.Errorf("Data size mismatch: expected %d, got %d",
			len(largeData), len(incomplete[0].Data))
	}

	// Verify data integrity
	for i, b := range incomplete[0].Data {
		if b != largeData[i] {
			t.Errorf("Data mismatch at position %d", i)
			break
		}
	}
}

func BenchmarkDoubleWriteBuffer_Write(b *testing.B) {
	dir := b.TempDir()
	journalPath := filepath.Join(dir, "bench.journal")

	// #nosec G304 - test file path from TempDir
	journal, err := os.OpenFile(journalPath, os.O_CREATE|os.O_RDWR|os.O_SYNC, 0o600)
	if err != nil {
		b.Fatalf("Failed to create journal: %v", err)
	}
	defer func() { _ = journal.Close() }()

	dwb, err := NewDoubleWriteBuffer(journal, 64*1024)
	if err != nil {
		b.Fatalf("Failed to create double-write buffer: %v", err)
	}

	testData := make([]byte, 1024) // 1KB entries
	for i := range testData {
		testData[i] = byte(i % 256)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err = dwb.WriteToJournal(testData, int64(i*1024), true)
		if err != nil {
			b.Fatalf("Write failed: %v", err)
		}
		_ = dwb.MarkComplete(true)
	}
	b.SetBytes(int64(len(testData)))
}
