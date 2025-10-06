package commands

import (
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/willibrandon/mtlog-audit/wal"
	"github.com/willibrandon/mtlog/core"
)

func TestExportCommand(t *testing.T) {
	// Create test WAL with events
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "test.wal")

	w, err := wal.New(walPath)
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}

	// Write test events
	testEvents := []*core.LogEvent{
		{
			Timestamp:       time.Now().Add(-2 * time.Hour),
			Level:           core.InformationLevel,
			MessageTemplate: "User {UserId} logged in",
			Properties: map[string]any{
				"UserId": "123",
				"Action": "login",
			},
		},
		{
			Timestamp:       time.Now().Add(-1 * time.Hour),
			Level:           core.WarningLevel,
			MessageTemplate: "Failed login attempt for {UserId}",
			Properties: map[string]any{
				"UserId": "456",
				"Action": "failed_login",
			},
		},
		{
			Timestamp:       time.Now(),
			Level:           core.ErrorLevel,
			MessageTemplate: "Database connection failed",
			Properties: map[string]any{
				"Database": "audit_db",
				"Error":    "connection timeout",
			},
		},
	}

	for _, event := range testEvents {
		if err := w.Write(event); err != nil {
			t.Fatalf("Failed to write event: %v", err)
		}
	}
	_ = w.Close()

	// Test JSON export
	t.Run("ExportJSON", func(t *testing.T) {
		outputPath := filepath.Join(tmpDir, "events.json")

		err := testExportJSON(walPath, outputPath, false)
		if err != nil {
			t.Fatalf("Failed to export JSON: %v", err)
		}

		// Verify output
		data, err := os.ReadFile(outputPath) // #nosec G304 - test file path
		if err != nil {
			t.Fatalf("Failed to read output file: %v", err)
		}

		var events []*core.LogEvent
		if err := json.Unmarshal(data, &events); err != nil {
			t.Fatalf("Failed to parse JSON output: %v", err)
		}

		if len(events) != 3 {
			t.Errorf("Expected 3 events, got %d", len(events))
		}
	})

	// Test CSV export
	t.Run("ExportCSV", func(t *testing.T) {
		outputPath := filepath.Join(tmpDir, "events.csv")

		err := testExportCSV(walPath, outputPath)
		if err != nil {
			t.Fatalf("Failed to export CSV: %v", err)
		}

		// Verify output
		file, err := os.Open(outputPath) // #nosec G304 - test file path
		if err != nil {
			t.Fatalf("Failed to open CSV file: %v", err)
		}
		defer func() { _ = file.Close() }()

		reader := csv.NewReader(file)
		records, err := reader.ReadAll()
		if err != nil {
			t.Fatalf("Failed to read CSV: %v", err)
		}

		// Should have header + 3 events
		if len(records) != 4 {
			t.Errorf("Expected 4 CSV records (header + 3 events), got %d", len(records))
		}

		// Verify header
		expectedHeader := []string{
			"Timestamp",
			"Level",
			"MessageTemplate",
			"RenderedMessage",
			"Properties",
			"Exception",
		}

		for i, col := range expectedHeader {
			if records[0][i] != col {
				t.Errorf("Expected header column %d to be %s, got %s", i, col, records[0][i])
			}
		}
	})

	// Test JSONL export
	t.Run("ExportJSONL", func(t *testing.T) {
		outputPath := filepath.Join(tmpDir, "events.jsonl")

		err := testExportJSONL(walPath, outputPath)
		if err != nil {
			t.Fatalf("Failed to export JSONL: %v", err)
		}

		// Verify output
		data, err := os.ReadFile(outputPath) // #nosec G304 - test file path
		if err != nil {
			t.Fatalf("Failed to read output file: %v", err)
		}

		// Count lines
		lineCount := 0
		for i := 0; i < len(data); i++ {
			if data[i] == '\n' {
				lineCount++
			}
		}

		if lineCount != 3 {
			t.Errorf("Expected 3 lines in JSONL, got %d", lineCount)
		}
	})
}

func TestParseTimeRange(t *testing.T) {
	tests := []struct {
		name      string
		startStr  string
		endStr    string
		wantError bool
	}{
		{
			name:      "Empty range",
			startStr:  "",
			endStr:    "",
			wantError: false,
		},
		{
			name:      "RFC3339 format",
			startStr:  "2024-01-01T00:00:00Z",
			endStr:    "2024-01-31T23:59:59Z",
			wantError: false,
		},
		{
			name:      "Relative times",
			startStr:  "24h ago",
			endStr:    "now",
			wantError: false,
		},
		{
			name:      "Invalid format",
			startStr:  "invalid",
			endStr:    "",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end, err := parseTimeRange(tt.startStr, tt.endStr)

			if tt.wantError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.wantError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			// Verify start is before end if both are set
			if !tt.wantError && !start.IsZero() && !end.IsZero() {
				if start.After(end) {
					t.Error("Start time is after end time")
				}
			}
		})
	}
}

func TestParseTime(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name      string
		input     string
		wantError bool
		check     func(time.Time) bool
	}{
		{
			name:      "RFC3339",
			input:     "2024-01-01T12:00:00Z",
			wantError: false,
			check: func(t time.Time) bool {
				return t.Year() == 2024 && t.Month() == 1 && t.Day() == 1
			},
		},
		{
			name:      "now",
			input:     "now",
			wantError: false,
			check: func(t time.Time) bool {
				return t.Sub(now) < time.Second
			},
		},
		{
			name:      "1h ago",
			input:     "1h ago",
			wantError: false,
			check: func(t time.Time) bool {
				diff := now.Sub(t)
				return diff > 59*time.Minute && diff < 61*time.Minute
			},
		},
		{
			name:      "24h ago",
			input:     "24h ago",
			wantError: false,
			check: func(t time.Time) bool {
				diff := now.Sub(t)
				return diff > 23*time.Hour && diff < 25*time.Hour
			},
		},
		{
			name:      "7d ago",
			input:     "7d ago",
			wantError: false,
			check: func(t time.Time) bool {
				diff := now.Sub(t)
				return diff > 6*24*time.Hour && diff < 8*24*time.Hour
			},
		},
		{
			name:      "Duration format",
			input:     "-2h30m",
			wantError: false,
			check: func(t time.Time) bool {
				diff := now.Sub(t)
				return diff > 2*time.Hour && diff < 3*time.Hour
			},
		},
		{
			name:      "Invalid",
			input:     "invalid format",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseTime(tt.input)

			if tt.wantError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.wantError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if !tt.wantError && tt.check != nil && !tt.check(result) {
				t.Errorf("Time check failed for input %s, got %v", tt.input, result)
			}
		})
	}
}

// Helper functions for testing export formats
func testExportJSON(walPath, outputPath string, pretty bool) error {
	reader, err := wal.NewReader(walPath)
	if err != nil {
		return err
	}
	defer func() { _ = reader.Close() }()

	events, err := reader.ReadAll()
	if err != nil {
		return err
	}

	return exportJSON(events, outputPath, pretty)
}

func testExportCSV(walPath, outputPath string) error {
	reader, err := wal.NewReader(walPath)
	if err != nil {
		return err
	}
	defer func() { _ = reader.Close() }()

	events, err := reader.ReadAll()
	if err != nil {
		return err
	}

	return exportCSV(events, outputPath)
}

func testExportJSONL(walPath, outputPath string) error {
	reader, err := wal.NewReader(walPath)
	if err != nil {
		return err
	}
	defer func() { _ = reader.Close() }()

	events, err := reader.ReadAll()
	if err != nil {
		return err
	}

	return exportJSONL(events, outputPath)
}
