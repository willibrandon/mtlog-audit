package wal

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/willibrandon/mtlog/core"
)

func TestWALIntegrity(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "wal-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	walPath := filepath.Join(tmpDir, "test.wal")

	// Create WAL
	w, err := New(walPath)
	if err != nil {
		t.Fatal(err)
	}

	// Write some events
	events := make([]*core.LogEvent, 10)
	for i := 0; i < 10; i++ {
		events[i] = &core.LogEvent{
			Timestamp:       time.Now(),
			Level:           core.InformationLevel,
			MessageTemplate: "Test event",
			Properties: map[string]interface{}{
				"Index": i,
			},
		}
		if err := w.Write(events[i]); err != nil {
			t.Fatalf("Failed to write event %d: %v", i, err)
		}
	}

	// Verify integrity
	if err := w.VerifyIntegrity(); err != nil {
		t.Fatalf("Integrity verification failed: %v", err)
	}

	// Get integrity report
	report, err := w.VerifyIntegrityReport()
	if err != nil {
		t.Fatalf("Failed to get integrity report: %v", err)
	}

	if !report.Valid {
		t.Fatal("WAL marked as invalid")
	}

	if report.TotalRecords != 10 {
		t.Errorf("Expected 10 records, got %d", report.TotalRecords)
	}

	// Close and reopen
	w.Close()

	w2, err := New(walPath)
	if err != nil {
		t.Fatal(err)
	}
	defer w2.Close()

	// Verify after recovery
	if err := w2.VerifyIntegrity(); err != nil {
		t.Fatalf("Integrity verification failed after recovery: %v", err)
	}

	// Check sequence continued
	if w2.sequence != 10 {
		t.Errorf("Expected sequence 10 after recovery, got %d", w2.sequence)
	}
}

func TestReadAllRecords(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "wal-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	walPath := filepath.Join(tmpDir, "test.wal")

	// Create WAL
	w, err := New(walPath)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	// Write some events
	for i := 0; i < 5; i++ {
		event := &core.LogEvent{
			Timestamp:       time.Now(),
			Level:           core.InformationLevel,
			MessageTemplate: "Test event",
			Properties: map[string]interface{}{
				"Index": i,
			},
		}
		if err := w.Write(event); err != nil {
			t.Fatalf("Failed to write event %d: %v", i, err)
		}
	}

	// Read all records
	records, err := w.readAllRecords()
	if err != nil {
		t.Fatalf("Failed to read records: %v", err)
	}

	if len(records) != 5 {
		t.Errorf("Expected 5 records, got %d", len(records))
	}

	// Verify each record can be unmarshaled
	for i, recordData := range records {
		record, err := UnmarshalRecord(recordData)
		if err != nil {
			t.Errorf("Failed to unmarshal record %d: %v", i, err)
		}
		if record.Sequence != uint64(i+1) {
			t.Errorf("Record %d has wrong sequence: expected %d, got %d", i, i+1, record.Sequence)
		}
	}
}
