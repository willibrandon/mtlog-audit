package audit

import (
	"os"
	"testing"
	"time"

	"github.com/willibrandon/mtlog/core"
)

func TestSinkBasic(t *testing.T) {
	// Create temp directory for WAL
	tmpDir, err := os.MkdirTemp("", "mtlog-audit-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	walPath := tmpDir + "/test.wal"

	// Create sink
	sink, err := New(
		WithWAL(walPath),
		WithPanicOnFailure(),
	)
	if err != nil {
		t.Fatalf("Failed to create sink: %v", err)
	}
	defer sink.Close()

	// Create a test event
	event := &core.LogEvent{
		Timestamp:       time.Now(),
		Level:           core.InformationLevel,
		MessageTemplate: "Test message",
		Properties:      make(map[string]interface{}),
	}

	// Emit the event
	sink.Emit(event)

	// Verify integrity
	report, err := sink.VerifyIntegrity()
	if err != nil {
		t.Fatalf("Integrity verification failed: %v", err)
	}

	if !report.Valid {
		t.Error("Integrity check failed")
	}
}

func TestSinkMultipleEvents(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mtlog-audit-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	walPath := tmpDir + "/test.wal"

	sink, err := New(WithWAL(walPath))
	if err != nil {
		t.Fatalf("Failed to create sink: %v", err)
	}

	// Write multiple events
	for i := 0; i < 100; i++ {
		event := &core.LogEvent{
			Timestamp:       time.Now(),
			Level:           core.InformationLevel,
			MessageTemplate: "Test message",
			Properties: map[string]interface{}{
				"Index": i,
			},
		}
		sink.Emit(event)
	}

	// Close and reopen to test recovery
	if err := sink.Close(); err != nil {
		t.Fatalf("Failed to close sink: %v", err)
	}

	// Reopen sink
	sink2, err := New(WithWAL(walPath))
	if err != nil {
		t.Fatalf("Failed to reopen sink: %v", err)
	}
	defer sink2.Close()

	// Verify integrity
	report, err := sink2.VerifyIntegrity()
	if err != nil {
		t.Fatalf("Integrity verification failed: %v", err)
	}

	if !report.Valid {
		t.Error("Integrity check failed after recovery")
	}
}
