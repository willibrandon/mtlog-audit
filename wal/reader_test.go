package wal

import (
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/willibrandon/mtlog/core"
)

func TestWALReadWrite(t *testing.T) {
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "test.wal")

	// Write events
	w, err := New(walPath)
	if err != nil {
		t.Fatal(err)
	}

	testEvents := []*core.LogEvent{
		{
			Timestamp:       time.Now(),
			Level:           core.InformationLevel,
			MessageTemplate: "First event",
			Properties:      map[string]interface{}{"id": 1, "type": "test"},
		},
		{
			Timestamp:       time.Now().Add(time.Second),
			Level:           core.ErrorLevel,
			MessageTemplate: "Error event",
			Properties:      map[string]interface{}{"id": 2, "error": "test error"},
		},
		{
			Timestamp:       time.Now().Add(2 * time.Second),
			Level:           core.WarningLevel,
			MessageTemplate: "Warning event",
			Properties:      map[string]interface{}{"id": 3, "warning": "test warning"},
		},
	}

	for _, event := range testEvents {
		if err := w.Write(event); err != nil {
			t.Fatalf("Failed to write event: %v", err)
		}
	}
	_ = w.Close()

	// Read events back
	reader, err := NewReader(walPath)
	if err != nil {
		t.Fatalf("Failed to create reader: %v", err)
	}
	defer func() { _ = reader.Close() }()

	readEvents, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("Failed to read events: %v", err)
	}

	if len(readEvents) != len(testEvents) {
		t.Errorf("Expected %d events, got %d", len(testEvents), len(readEvents))
	}

	// Verify events match
	for i, event := range readEvents {
		if event.MessageTemplate != testEvents[i].MessageTemplate {
			t.Errorf("Event %d message mismatch: got %s, want %s",
				i, event.MessageTemplate, testEvents[i].MessageTemplate)
		}
		if event.Level != testEvents[i].Level {
			t.Errorf("Event %d level mismatch: got %v, want %v",
				i, event.Level, testEvents[i].Level)
		}
	}
}

func TestWALReadRange(t *testing.T) {
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "test.wal")

	// Write events with different timestamps
	w, err := New(walPath)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	testEvents := []*core.LogEvent{
		{
			Timestamp:       now.Add(-2 * time.Hour),
			Level:           core.InformationLevel,
			MessageTemplate: "Old event",
			Properties:      map[string]interface{}{"id": 1},
		},
		{
			Timestamp:       now.Add(-1 * time.Hour),
			Level:           core.InformationLevel,
			MessageTemplate: "Middle event",
			Properties:      map[string]interface{}{"id": 2},
		},
		{
			Timestamp:       now,
			Level:           core.InformationLevel,
			MessageTemplate: "Recent event",
			Properties:      map[string]interface{}{"id": 3},
		},
	}

	for _, event := range testEvents {
		if err := w.Write(event); err != nil {
			t.Fatalf("Failed to write event: %v", err)
		}
	}
	_ = w.Close()

	// Read events in time range
	reader, err := NewReader(walPath)
	if err != nil {
		t.Fatalf("Failed to create reader: %v", err)
	}
	defer func() { _ = reader.Close() }()

	// Read only middle and recent events
	start := now.Add(-90 * time.Minute)
	end := now.Add(30 * time.Minute)

	rangeEvents, err := reader.ReadRange(start, end)
	if err != nil {
		t.Fatalf("Failed to read range: %v", err)
	}

	if len(rangeEvents) != 2 {
		t.Errorf("Expected 2 events in range, got %d", len(rangeEvents))
	}

	if len(rangeEvents) > 0 && rangeEvents[0].MessageTemplate != "Middle event" {
		t.Errorf("Expected first event to be 'Middle event', got %s", rangeEvents[0].MessageTemplate)
	}
}

func TestWALReaderCorruption(t *testing.T) {
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "test.wal")

	// Write valid events
	w, err := New(walPath)
	if err != nil {
		t.Fatal(err)
	}

	event := &core.LogEvent{
		Timestamp:       time.Now(),
		Level:           core.InformationLevel,
		MessageTemplate: "Test event",
		Properties:      map[string]interface{}{"id": 1},
	}

	if err := w.Write(event); err != nil {
		t.Fatalf("Failed to write event: %v", err)
	}
	_ = w.Close()

	// Read should succeed
	reader, err := NewReader(walPath)
	if err != nil {
		t.Fatalf("Failed to create reader: %v", err)
	}
	defer func() { _ = reader.Close() }()

	events, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("Failed to read events: %v", err)
	}

	if len(events) != 1 {
		t.Errorf("Expected 1 event, got %d", len(events))
	}
}

func TestWALReaderEmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "empty.wal")

	// Create empty WAL
	w, err := New(walPath)
	if err != nil {
		t.Fatal(err)
	}
	_ = w.Close()

	// Read from empty WAL
	reader, err := NewReader(walPath)
	if err != nil {
		t.Fatalf("Failed to create reader: %v", err)
	}
	defer func() { _ = reader.Close() }()

	events, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("Failed to read events: %v", err)
	}

	if len(events) != 0 {
		t.Errorf("Expected 0 events from empty WAL, got %d", len(events))
	}
}

func TestWALReaderSeek(t *testing.T) {
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "test.wal")

	// Write multiple events
	w, err := New(walPath)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 5; i++ {
		event := &core.LogEvent{
			Timestamp:       time.Now(),
			Level:           core.InformationLevel,
			MessageTemplate: "Test event",
			Properties:      map[string]interface{}{"id": i},
		}
		if err := w.Write(event); err != nil {
			t.Fatalf("Failed to write event: %v", err)
		}
	}
	_ = w.Close()

	// Create reader
	reader, err := NewReader(walPath)
	if err != nil {
		t.Fatalf("Failed to create reader: %v", err)
	}
	defer func() { _ = reader.Close() }()

	// Read first event and remember offset
	firstEvent, err := reader.ReadNext()
	if err != nil {
		t.Fatalf("Failed to read first event: %v", err)
	}

	offset := reader.GetOffset()

	// Read second event
	secondEvent, err := reader.ReadNext()
	if err != nil {
		t.Fatalf("Failed to read second event: %v", err)
	}

	// Seek back to after first event
	if _, err := reader.Seek(offset, io.SeekStart); err != nil {
		t.Fatalf("Failed to seek: %v", err)
	}

	// Read again, should get second event
	eventAfterSeek, err := reader.ReadNext()
	if err != nil {
		t.Fatalf("Failed to read after seek: %v", err)
	}

	// Verify we got the second event again
	if secondEvent.Properties["id"] != eventAfterSeek.Properties["id"] {
		t.Errorf("Event after seek doesn't match second event")
	}

	_ = firstEvent // silence unused variable warning
}
