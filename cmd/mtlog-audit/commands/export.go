// Package commands implements CLI commands for mtlog-audit.
package commands

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/willibrandon/mtlog-audit/internal/logger"
	"github.com/willibrandon/mtlog-audit/wal"
	"github.com/willibrandon/mtlog/core"
)

// exportCmd creates the export command.
func exportCmd() *cobra.Command {
	var (
		walPath  string
		output   string
		format   string
		startStr string
		endStr   string
		pretty   bool
	)

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export WAL events to various formats",
		Long: `Export WAL events to JSON, CSV, or other formats for analysis.
		
Examples:
  # Export all events to JSON
  mtlog-audit export --wal /var/audit/app.wal --output events.json
  
  # Export events from last 24 hours to CSV
  mtlog-audit export --wal /var/audit/app.wal --output events.csv --format csv --start "24h ago"
  
  # Export events in time range with pretty JSON
  mtlog-audit export --wal /var/audit/app.wal --output events.json --pretty \
    --start "2024-01-01T00:00:00Z" --end "2024-01-31T23:59:59Z"`,
		RunE: func(_ *cobra.Command, _ []string) error {
			// Parse time range
			start, end, err := parseTimeRange(startStr, endStr)
			if err != nil {
				return fmt.Errorf("invalid time range: %w", err)
			}

			// Open WAL reader
			reader, err := wal.NewReader(walPath)
			if err != nil {
				return fmt.Errorf("failed to open WAL: %w", err)
			}
			defer func() { _ = reader.Close() }()

			// Read events in range
			logger.Log.Info("Reading events from WAL...")
			events, err := reader.ReadRange(start, end)
			if err != nil {
				return fmt.Errorf("failed to read events: %w", err)
			}

			logger.Log.Info("Exporting {count} events to {format} format...", len(events), format)

			// Export based on format
			switch format {
			case "json":
				return exportJSON(events, output, pretty)
			case "csv":
				return exportCSV(events, output)
			case "jsonl":
				return exportJSONL(events, output)
			default:
				return fmt.Errorf("unsupported format: %s", format)
			}
		},
	}

	cmd.Flags().StringVar(&walPath, "wal", "", "Path to WAL file (required)")
	cmd.Flags().StringVar(&output, "output", "", "Output file path (required)")
	cmd.Flags().StringVar(&format, "format", "json", "Export format (json, jsonl, csv)")
	cmd.Flags().StringVar(&startStr, "start", "", "Start time (RFC3339 or relative like '1h ago')")
	cmd.Flags().StringVar(&endStr, "end", "", "End time (RFC3339 or relative like 'now')")
	cmd.Flags().BoolVar(&pretty, "pretty", false, "Pretty print JSON output")

	_ = cmd.MarkFlagRequired("wal")
	_ = cmd.MarkFlagRequired("output")

	return cmd
}

// parseTimeRange parses start and end time strings into time.Time values.
func parseTimeRange(startStr, endStr string) (start, end time.Time, err error) {
	// Parse start time
	if startStr != "" {
		start, err = parseTime(startStr)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid start time: %w", err)
		}
	}

	// Parse end time
	if endStr != "" {
		end, err = parseTime(endStr)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid end time: %w", err)
		}
	}

	return start, end, nil
}

// parseTime parses a time string that can be either RFC3339 or relative.
func parseTime(s string) (time.Time, error) {
	// Try RFC3339 first
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}

	// Handle relative times
	now := time.Now()
	switch s {
	case "now":
		return now, nil
	case "1h ago":
		return now.Add(-time.Hour), nil
	case "24h ago":
		return now.Add(-24 * time.Hour), nil
	case "7d ago":
		return now.Add(-7 * 24 * time.Hour), nil
	case "30d ago":
		return now.Add(-30 * 24 * time.Hour), nil
	default:
		// Try parsing as duration
		if d, err := time.ParseDuration(s); err == nil {
			return now.Add(d), nil
		}
		return time.Time{}, fmt.Errorf("unrecognized time format: %s", s)
	}
}

// exportJSON exports events to JSON format.
func exportJSON(events []*core.LogEvent, output string, pretty bool) error {
	file, err := os.Create(output) // #nosec G304 - user-specified output path
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer func() { _ = file.Close() }()

	encoder := json.NewEncoder(file)
	if pretty {
		encoder.SetIndent("", "  ")
	}

	// Export as array
	if err := encoder.Encode(events); err != nil {
		return fmt.Errorf("failed to encode events: %w", err)
	}

	logger.Log.Info("Exported {count} events to {file}", len(events), output)
	return nil
}

// exportJSONL exports events to JSON Lines format (one JSON object per line).
func exportJSONL(events []*core.LogEvent, output string) error {
	file, err := os.Create(output) // #nosec G304 - user-specified output path
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer func() { _ = file.Close() }()

	encoder := json.NewEncoder(file)

	// Write each event on a separate line
	for _, event := range events {
		if err := encoder.Encode(event); err != nil {
			return fmt.Errorf("failed to encode event: %w", err)
		}
	}

	logger.Log.Info("Exported {count} events to {file}", len(events), output)
	return nil
}

// exportCSV exports events to CSV format.
func exportCSV(events []*core.LogEvent, output string) error {
	file, err := os.Create(output) // #nosec G304 - user-specified output path
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer func() { _ = file.Close() }()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Write header
	header := []string{
		"Timestamp",
		"Level",
		"MessageTemplate",
		"RenderedMessage",
		"Properties",
		"Exception",
	}
	if err := writer.Write(header); err != nil {
		return fmt.Errorf("failed to write CSV header: %w", err)
	}

	// Write events
	for _, event := range events {
		// Marshal properties to JSON string
		propsJSON, _ := json.Marshal(event.Properties)

		// Format exception
		exceptionStr := ""
		if event.Exception != nil {
			exceptionStr = event.Exception.Error()
		}

		record := []string{
			event.Timestamp.Format(time.RFC3339Nano),
			formatLevel(event.Level),
			event.MessageTemplate,
			event.RenderMessage(), // Properly render message with properties
			string(propsJSON),
			exceptionStr,
		}

		if err := writer.Write(record); err != nil {
			return fmt.Errorf("failed to write CSV record: %w", err)
		}
	}

	logger.Log.Info("Exported {count} events to {file}", len(events), output)
	return nil
}
