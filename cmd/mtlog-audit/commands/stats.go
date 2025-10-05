// Package commands implements CLI commands for mtlog-audit.
package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/willibrandon/mtlog-audit/internal/logger"
	"github.com/willibrandon/mtlog-audit/wal"
	"github.com/willibrandon/mtlog/core"
)

// WALStats contains comprehensive WAL statistics.
type WALStats struct {
	// File statistics
	Path         string    `json:"path"`
	TotalSize    int64     `json:"total_size"`
	SegmentCount int       `json:"segment_count"`
	CreatedAt    time.Time `json:"created_at"`
	ModifiedAt   time.Time `json:"modified_at"`

	// Record statistics
	TotalRecords  int    `json:"total_records"`
	FirstSequence uint64 `json:"first_sequence"`
	LastSequence  uint64 `json:"last_sequence"`
	ErrorCount    int    `json:"error_count"`
	WarningCount  int    `json:"warning_count"`
	InfoCount     int    `json:"info_count"`
	DebugCount    int    `json:"debug_count"`

	// Time range
	FirstEventTime time.Time `json:"first_event_time"`
	LastEventTime  time.Time `json:"last_event_time"`
	Duration       string    `json:"duration"`

	// Segment details
	Segments []SegmentStats `json:"segments"`

	// Health indicators
	HasCorruption bool   `json:"has_corruption"`
	IsSealed      bool   `json:"is_sealed"`
	Compression   string `json:"compression"`

	// Performance metrics
	AvgRecordSize    int64   `json:"avg_record_size"`
	AvgSegmentSize   int64   `json:"avg_segment_size"`
	FragmentationPct float64 `json:"fragmentation_pct"`
}

// SegmentStats contains statistics for a single segment.
type SegmentStats struct {
	Path          string    `json:"path"`
	Size          int64     `json:"size"`
	StartSeq      uint64    `json:"start_seq"`
	EndSeq        uint64    `json:"end_seq"`
	RecordCount   int       `json:"record_count"`
	CreatedAt     time.Time `json:"created_at"`
	Sealed        bool      `json:"sealed"`
	Corrupted     bool      `json:"corrupted"`
	CompactionPct float64   `json:"compaction_pct"`
}

// statsCmd creates the stats command.
func statsCmd() *cobra.Command {
	var (
		walPath string
		format  string
		verbose bool
	)

	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Display WAL statistics and metrics",
		Long: `Display comprehensive statistics about a WAL file including size,
record counts, time ranges, and health indicators.

Examples:
  # Show basic statistics
  mtlog-audit stats --wal /var/audit/app.wal
  
  # Show detailed statistics with segment breakdown
  mtlog-audit stats --wal /var/audit/app.wal --verbose
  
  # Output statistics as JSON
  mtlog-audit stats --wal /var/audit/app.wal --format json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Gather statistics
			stats, err := gatherStats(walPath, verbose)
			if err != nil {
				return fmt.Errorf("failed to gather statistics: %w", err)
			}

			// Output based on format
			switch format {
			case "json":
				return outputStatsJSON(stats)
			case "table":
				return outputTable(stats, verbose)
			default:
				return fmt.Errorf("unsupported format: %s", format)
			}
		},
	}

	cmd.Flags().StringVar(&walPath, "wal", "", "Path to WAL file (required)")
	cmd.Flags().StringVar(&format, "format", "table", "Output format (table, json)")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "Show detailed statistics")

	cmd.MarkFlagRequired("wal")

	return cmd
}

// gatherStats collects comprehensive statistics about a WAL.
func gatherStats(walPath string, includeSegments bool) (*WALStats, error) {
	stats := &WALStats{
		Path: walPath,
	}

	// Open WAL
	w, err := wal.New(walPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open WAL: %w", err)
	}
	defer w.Close()

	// Get segments from WAL
	segments := w.GetSegments()

	// Get modification time from the most recent segment
	for _, seg := range segments {
		if stats.ModifiedAt.IsZero() || seg.CreatedAt.After(stats.ModifiedAt) {
			stats.ModifiedAt = seg.CreatedAt
		}
	}
	stats.SegmentCount = len(segments)

	// Calculate total size and gather segment stats
	for _, seg := range segments {
		stats.TotalSize += seg.Size

		if includeSegments {
			// Calculate record count more safely
			recordCount := 0
			if seg.EndSeq > seg.StartSeq {
				recordCount = int(seg.EndSeq - seg.StartSeq + 1)
			} else if seg.StartSeq > 0 && !seg.Sealed {
				// For active segments, we don't know the end sequence yet
				recordCount = -1 // Indicate unknown
			}

			segStats := SegmentStats{
				Path:        seg.Path,
				Size:        seg.Size,
				StartSeq:    seg.StartSeq,
				EndSeq:      seg.EndSeq,
				RecordCount: recordCount,
				CreatedAt:   seg.CreatedAt,
				Sealed:      seg.Sealed,
				Corrupted:   seg.Corrupted,
			}

			// Calculate compaction potential (simplified)
			if seg.Sealed && seg.Size > 0 {
				// Simple heuristic: segments that are very small might benefit from compaction
				segStats.CompactionPct = float64(seg.Size) / float64(64*1024*1024) * 100
			}

			stats.Segments = append(stats.Segments, segStats)
		}

		// Track earliest creation time
		if stats.CreatedAt.IsZero() || seg.CreatedAt.Before(stats.CreatedAt) {
			stats.CreatedAt = seg.CreatedAt
		}

		// Track corruption
		if seg.Corrupted {
			stats.HasCorruption = true
		}

		// Check if all segments are sealed
		if !seg.Sealed {
			stats.IsSealed = false
		} else if stats.SegmentCount > 0 {
			stats.IsSealed = true
		}
	}

	// Read all events from all segments using the segment manager
	var events []*core.LogEvent

	for _, segment := range segments {
		if segment.Size == 0 {
			continue // Skip empty segments
		}

		// Read events from this segment
		segmentReader, err := wal.NewReader(segment.Path)
		if err != nil {
			logger.Log.Warn("Failed to read segment {path}: {error}", segment.Path, err)
			continue
		}

		segmentEvents, err := segmentReader.ReadAll()
		segmentReader.Close()
		if err != nil {
			logger.Log.Warn("Failed to read all events from segment {path}: {error}", segment.Path, err)
			continue
		}

		events = append(events, segmentEvents...)
	}

	if len(events) > 0 {
		stats.TotalRecords = len(events)
		stats.FirstEventTime = events[0].Timestamp
		stats.LastEventTime = events[len(events)-1].Timestamp
		stats.Duration = stats.LastEventTime.Sub(stats.FirstEventTime).String()

		// Count by level
		for _, event := range events {
			switch event.Level {
			case core.ErrorLevel, core.FatalLevel:
				stats.ErrorCount++
			case core.WarningLevel:
				stats.WarningCount++
			case core.InformationLevel:
				stats.InfoCount++
			case core.DebugLevel, core.VerboseLevel:
				stats.DebugCount++
			}
		}

		// Calculate average record size
		if stats.TotalRecords > 0 {
			stats.AvgRecordSize = stats.TotalSize / int64(stats.TotalRecords)
		}
	}

	// Calculate average segment size
	if stats.SegmentCount > 0 {
		stats.AvgSegmentSize = stats.TotalSize / int64(stats.SegmentCount)
	}

	// Get sequence range from segments
	if len(segments) > 0 {
		stats.FirstSequence = segments[0].StartSeq
		stats.LastSequence = segments[len(segments)-1].EndSeq
	}

	// Calculate fragmentation (simplified - ratio of segments to ideal)
	if stats.TotalSize > 0 && stats.SegmentCount > 0 {
		idealSegmentSize := int64(64 * 1024 * 1024) // 64MB ideal
		idealSegments := (stats.TotalSize / idealSegmentSize) + 1
		if idealSegments > 0 {
			stats.FragmentationPct = float64(stats.SegmentCount-int(idealSegments)) / float64(idealSegments) * 100
			if stats.FragmentationPct < 0 {
				stats.FragmentationPct = 0
			}
		}
	}

	// Set compression (currently none)
	stats.Compression = "none"

	return stats, nil
}

// outputStatsJSON outputs statistics in JSON format.
func outputStatsJSON(stats *WALStats) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(stats)
}

// outputTable outputs statistics in table format.
func outputTable(stats *WALStats, verbose bool) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)

	fmt.Fprintln(w, "WAL STATISTICS")
	fmt.Fprintln(w, "==============")
	fmt.Fprintln(w)

	// File information
	fmt.Fprintf(w, "Path:\t%s\n", stats.Path)
	fmt.Fprintf(w, "Total Size:\t%s\n", formatBytes(stats.TotalSize))
	fmt.Fprintf(w, "Segments:\t%d\n", stats.SegmentCount)
	fmt.Fprintf(w, "Created:\t%s\n", stats.CreatedAt.Format(time.RFC3339))
	fmt.Fprintf(w, "Modified:\t%s\n", stats.ModifiedAt.Format(time.RFC3339))
	fmt.Fprintln(w)

	// Record statistics
	fmt.Fprintln(w, "RECORDS")
	fmt.Fprintln(w, "-------")
	fmt.Fprintf(w, "Total:\t%d\n", stats.TotalRecords)
	fmt.Fprintf(w, "Sequence Range:\t%d - %d\n", stats.FirstSequence, stats.LastSequence)
	fmt.Fprintf(w, "Time Range:\t%s - %s\n",
		stats.FirstEventTime.Format(time.RFC3339),
		stats.LastEventTime.Format(time.RFC3339))
	fmt.Fprintf(w, "Duration:\t%s\n", stats.Duration)
	fmt.Fprintln(w)

	// Level breakdown
	fmt.Fprintln(w, "BY LEVEL")
	fmt.Fprintln(w, "--------")
	fmt.Fprintf(w, "Errors:\t%d\n", stats.ErrorCount)
	fmt.Fprintf(w, "Warnings:\t%d\n", stats.WarningCount)
	fmt.Fprintf(w, "Info:\t%d\n", stats.InfoCount)
	fmt.Fprintf(w, "Debug:\t%d\n", stats.DebugCount)
	fmt.Fprintln(w)

	// Health indicators
	fmt.Fprintln(w, "HEALTH")
	fmt.Fprintln(w, "------")
	fmt.Fprintf(w, "Has Corruption:\t%v\n", stats.HasCorruption)
	fmt.Fprintf(w, "Is Sealed:\t%v\n", stats.IsSealed)
	fmt.Fprintf(w, "Compression:\t%s\n", stats.Compression)
	fmt.Fprintf(w, "Fragmentation:\t%.1f%%\n", stats.FragmentationPct)
	fmt.Fprintln(w)

	// Performance metrics
	fmt.Fprintln(w, "PERFORMANCE")
	fmt.Fprintln(w, "-----------")
	fmt.Fprintf(w, "Avg Record Size:\t%s\n", formatBytes(stats.AvgRecordSize))
	fmt.Fprintf(w, "Avg Segment Size:\t%s\n", formatBytes(stats.AvgSegmentSize))
	fmt.Fprintln(w)

	// Detailed segment information if verbose
	if verbose && len(stats.Segments) > 0 {
		fmt.Fprintln(w, "SEGMENTS")
		fmt.Fprintln(w, "--------")
		fmt.Fprintln(w, "Path\tSize\tRecords\tSeq Range\tSealed\tCompaction%")

		for _, seg := range stats.Segments {
			fmt.Fprintf(w, "%s\t%s\t%d\t%d-%d\t%v\t%.1f%%\n",
				seg.Path,
				formatBytes(seg.Size),
				seg.RecordCount,
				seg.StartSeq,
				seg.EndSeq,
				seg.Sealed,
				seg.CompactionPct)
		}
	}

	return w.Flush()
}

// formatBytes formats a byte count as a human-readable string.
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
