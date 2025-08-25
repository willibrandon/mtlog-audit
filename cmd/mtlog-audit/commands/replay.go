package commands

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"
	audit "github.com/willibrandon/mtlog-audit"
	"github.com/willibrandon/mtlog-audit/wal"
	"github.com/willibrandon/mtlog/core"
)

func replayCmd() *cobra.Command {
	var (
		walPath   string
		startTime string
		endTime   string
		format    string
		output    string
	)

	cmd := &cobra.Command{
		Use:   "replay",
		Short: "Replay events from WAL files",
		Long: `Replay events from Write-Ahead Log files.

This command reads events from WAL files and outputs them in various formats.
It can be used for debugging, analysis, or reprocessing of audit events.`,
		Example: `  # Replay all events from a WAL file
  mtlog-audit replay --wal /var/audit/mtlog.wal

  # Replay events from a specific time range
  mtlog-audit replay --wal /var/audit/mtlog.wal \
    --start "2023-01-01T00:00:00Z" \
    --end "2023-01-01T23:59:59Z"

  # Output events as JSON
  mtlog-audit replay --wal /var/audit/mtlog.wal --format json

  # Save output to file
  mtlog-audit replay --wal /var/audit/mtlog.wal --output events.json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReplay(walPath, startTime, endTime, format, output)
		},
	}

	cmd.Flags().StringVar(&walPath, "wal", "", "Path to WAL file (required)")
	cmd.Flags().StringVar(&startTime, "start", "", "Start time (RFC3339 format, e.g., 2023-01-01T00:00:00Z)")
	cmd.Flags().StringVar(&endTime, "end", "", "End time (RFC3339 format, e.g., 2023-01-01T23:59:59Z)")
	cmd.Flags().StringVar(&format, "format", "text", "Output format (text, json, csv)")
	cmd.Flags().StringVar(&output, "output", "", "Output file (default: stdout)")

	cmd.MarkFlagRequired("wal")

	return cmd
}

func runReplay(walPath, startTimeStr, endTimeStr, format, output string) error {
	// Validate WAL path
	if walPath == "" {
		return fmt.Errorf("WAL path is required")
	}

	// Parse time range if provided
	var startTime, endTime time.Time
	var err error

	if startTimeStr != "" {
		startTime, err = time.Parse(time.RFC3339, startTimeStr)
		if err != nil {
			return fmt.Errorf("invalid start time format: %w", err)
		}
	}

	if endTimeStr != "" {
		endTime, err = time.Parse(time.RFC3339, endTimeStr)
		if err != nil {
			return fmt.Errorf("invalid end time format: %w", err)
		}
	}

	if !startTime.IsZero() && !endTime.IsZero() && startTime.After(endTime) {
		return fmt.Errorf("start time cannot be after end time")
	}

	// Validate format
	switch format {
	case "text", "json", "csv":
		// Valid formats
	default:
		return fmt.Errorf("unsupported format: %s (supported: text, json, csv)", format)
	}

	fmt.Printf("🔄 Replaying events from WAL: %s\n", walPath)
	if !startTime.IsZero() {
		fmt.Printf("📅 Start time: %s\n", startTime.Format(time.RFC3339))
	}
	if !endTime.IsZero() {
		fmt.Printf("📅 End time: %s\n", endTime.Format(time.RFC3339))
	}
	fmt.Printf("📄 Format: %s\n", format)
	if output != "" {
		fmt.Printf("💾 Output file: %s\n", output)
	}
	fmt.Println()

	// First, just verify integrity without creating a full sink
	// We'll read directly from the WAL file for replay
	fmt.Printf("🔍 Verifying WAL integrity...")
	
	// Create a temporary sink just for verification
	verifySink, err := audit.New(
		audit.WithWAL(walPath),
	)
	if err != nil {
		return fmt.Errorf("failed to open WAL: %w", err)
	}
	
	report, err := verifySink.VerifyIntegrity()
	if err != nil {
		fmt.Printf(" ❌\n")
		return fmt.Errorf("WAL integrity verification failed: %w", err)
	}
	
	if !report.Valid {
		fmt.Printf(" ⚠️  Warning: WAL has integrity issues\n")
		fmt.Printf("   - Total records: %d\n", report.TotalRecords)
		fmt.Printf("   - Corrupted segments: %d\n", report.CorruptedSegments)
		fmt.Print("Continue anyway? (y/N): ")
		
		var response string
		fmt.Scanln(&response)
		if response != "y" && response != "Y" {
			return fmt.Errorf("replay cancelled due to integrity issues")
		}
	} else {
		fmt.Printf(" ✅\n")
		fmt.Printf("   - Total records: %d\n", report.TotalRecords)
		fmt.Printf("   - Last sequence: %d\n", report.WALIntegrity.LastSequence)
	}

	fmt.Println()

	// Close the verification sink before reading
	verifySink.Close()

	// Now read events directly from the WAL file
	reader, err := wal.NewReader(walPath)
	if err != nil {
		return fmt.Errorf("failed to create reader: %w", err)
	}
	defer reader.Close()

	var events []*core.LogEvent
	if startTime.IsZero() && endTime.IsZero() {
		events, err = reader.ReadAll()
	} else {
		events, err = reader.ReadRange(startTime, endTime)
	}
	if err != nil {
		return fmt.Errorf("failed to read events: %w", err)
	}

	fmt.Printf("📊 Found %d events to replay\n", len(events))
	fmt.Println()

	// Output events based on format
	switch format {
	case "json":
		if err := outputJSON(events, output); err != nil {
			return fmt.Errorf("failed to output JSON: %w", err)
		}
	case "csv":
		if err := outputCSV(events, output); err != nil {
			return fmt.Errorf("failed to output CSV: %w", err)
		}
	case "text":
		if err := outputText(events, output); err != nil {
			return fmt.Errorf("failed to output text: %w", err)
		}
	}

	fmt.Printf("✅ Successfully replayed %d events\n", len(events))
	return nil
}

func outputJSON(events []*core.LogEvent, outputFile string) error {
	var writer io.Writer = os.Stdout

	if outputFile != "" {
		file, err := os.Create(outputFile)
		if err != nil {
			return err
		}
		defer file.Close()
		writer = file
	}

	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")

	for _, event := range events {
		if err := encoder.Encode(event); err != nil {
			return err
		}
	}

	return nil
}

func outputText(events []*core.LogEvent, outputFile string) error {
	var writer io.Writer = os.Stdout

	if outputFile != "" {
		file, err := os.Create(outputFile)
		if err != nil {
			return err
		}
		defer file.Close()
		writer = file
	}

	for _, event := range events {
		levelStr := formatLevel(event.Level)
		fmt.Fprintf(writer, "[%s] [%s] %s\n",
			event.Timestamp.Format(time.RFC3339),
			levelStr,
			event.MessageTemplate)

		if len(event.Properties) > 0 {
			for k, v := range event.Properties {
				fmt.Fprintf(writer, "  %s: %v\n", k, v)
			}
		}
		fmt.Fprintln(writer)
	}

	return nil
}

func outputCSV(events []*core.LogEvent, outputFile string) error {
	var writer io.Writer = os.Stdout

	if outputFile != "" {
		file, err := os.Create(outputFile)
		if err != nil {
			return err
		}
		defer file.Close()
		writer = file
	}

	csvWriter := csv.NewWriter(writer)
	defer csvWriter.Flush()

	// Write header
	if err := csvWriter.Write([]string{"Timestamp", "Level", "Message", "Properties"}); err != nil {
		return err
	}

	for _, event := range events {
		props, _ := json.Marshal(event.Properties)
		levelStr := formatLevel(event.Level)
		record := []string{
			event.Timestamp.Format(time.RFC3339),
			levelStr,
			event.MessageTemplate,
			string(props),
		}
		if err := csvWriter.Write(record); err != nil {
			return err
		}
	}

	return nil
}

func formatLevel(level core.LogEventLevel) string {
	switch level {
	case core.VerboseLevel:
		return "VRB"
	case core.DebugLevel:
		return "DBG"
	case core.InformationLevel:
		return "INF"
	case core.WarningLevel:
		return "WRN"
	case core.ErrorLevel:
		return "ERR"
	case core.FatalLevel:
		return "FTL"
	default:
		return fmt.Sprintf("L%d", level)
	}
}