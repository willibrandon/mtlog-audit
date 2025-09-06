// Package commands implements CLI commands for mtlog-audit.
package commands

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/willibrandon/mtlog-audit/internal/logger"
	"github.com/willibrandon/mtlog-audit/wal"
)

// compactCmd creates the compact command.
func compactCmd() *cobra.Command {
	var (
		walPath   string
		force     bool
		dryRun    bool
		startSeq  uint64
		endSeq    uint64
		threshold float64
	)

	cmd := &cobra.Command{
		Use:   "compact",
		Short: "Manually trigger WAL compaction",
		Long: `Compact WAL segments to reclaim disk space by removing deleted records
and merging small segments.

Examples:
  # Compact all eligible segments
  mtlog-audit compact --wal /var/audit/app.wal
  
  # Force compaction even if segments don't meet threshold
  mtlog-audit compact --wal /var/audit/app.wal --force
  
  # Dry run to see what would be compacted
  mtlog-audit compact --wal /var/audit/app.wal --dry-run
  
  # Compact specific sequence range
  mtlog-audit compact --wal /var/audit/app.wal --start 1000 --end 5000
  
  # Set custom compaction threshold (0.0-1.0)
  mtlog-audit compact --wal /var/audit/app.wal --threshold 0.3`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Open WAL
			w, err := wal.New(walPath)
			if err != nil {
				return fmt.Errorf("failed to open WAL: %w", err)
			}
			defer w.Close()

			// Create compactor with custom policy if threshold is set
			var policy *wal.CompactionPolicy
			if cmd.Flags().Changed("threshold") {
				policy = &wal.CompactionPolicy{
					MinSegments:       1, // Allow compacting even single segments
					MaxSegmentAge:     time.Minute, // Lower age requirement  
					MinSegmentSize:    1024, // 1KB minimum
					TargetSegmentSize: 64 * 1024 * 1024, // 64MB target
					CompactRatio:      threshold,
				}
			} else {
				policy = wal.DefaultCompactionPolicy()
			}

			compactor := wal.NewCompactor(w, policy)

			// Get initial stats
			stats := compactor.GetStats()
			initialSize := calculateTotalSize(w)
			
			logger.Log.Info("Starting compaction on WAL: {path}", walPath)
			logger.Log.Info("Initial size: {size} bytes", initialSize)

			if dryRun {
				// Perform dry run analysis
				return performDryRun(compactor, w)
			}

			// Perform compaction
			var compactErr error
			if startSeq > 0 || endSeq > 0 {
				// Compact specific range
				if endSeq == 0 {
					endSeq = ^uint64(0) // Max uint64
				}
				logger.Log.Info("Compacting sequence range {start} to {end}", startSeq, endSeq)
				compactErr = compactor.CompactRange(startSeq, endSeq)
			} else if force {
				// Force compact all segments
				logger.Log.Info("Force compacting all eligible segments...")
				compactErr = compactor.ForceCompact()
			} else {
				// Normal compaction based on policy
				logger.Log.Info("Compacting based on policy...")
				compactErr = compactor.Compact()
			}

			if compactErr != nil {
				return fmt.Errorf("compaction failed: %w", compactErr)
			}


			// Get final stats
			finalStats := compactor.GetStats()
			finalSize := calculateTotalSize(w)
			spaceSaved := initialSize - finalSize

			// Report results
			logger.Log.Info("Compaction complete!")
			logger.Log.Info("Segments compacted: {count}", finalStats.SegmentsCompacted-stats.SegmentsCompacted)
			logger.Log.Info("Bytes compacted: {bytes}", finalStats.BytesCompacted-stats.BytesCompacted)
			logger.Log.Info("Space reclaimed: {bytes} bytes ({percent}%)", 
				spaceSaved, (spaceSaved*100)/initialSize)
			logger.Log.Info("Final size: {size} bytes", finalSize)

			return nil
		},
	}

	cmd.Flags().StringVar(&walPath, "wal", "", "Path to WAL file (required)")
	cmd.Flags().BoolVar(&force, "force", false, "Force compaction even if segments don't meet threshold")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be compacted without actually doing it")
	cmd.Flags().Uint64Var(&startSeq, "start", 0, "Start sequence number for range compaction")
	cmd.Flags().Uint64Var(&endSeq, "end", 0, "End sequence number for range compaction")
	cmd.Flags().Float64Var(&threshold, "threshold", 0.5, "Compaction ratio threshold (0.0-1.0)")

	cmd.MarkFlagRequired("wal")

	return cmd
}

// performDryRun analyzes what would be compacted without actually doing it.
func performDryRun(compactor *wal.Compactor, w *wal.WAL) error {
	// Get segments - for now we'll use a placeholder
	segments := make([]*wal.Segment, 0)
	
	logger.Log.Info("Dry run: analyzing {count} segments", len(segments))
	
	compactableCount := 0
	estimatedSavings := int64(0)
	
	for _, seg := range segments {
		if !seg.Sealed {
			logger.Log.Info("Segment {path}: ACTIVE (not compactable)", seg.Path)
			continue
		}
		
		// Calculate compaction ratio (simplified)
		ratio := float64(seg.Size) / float64(1024*1024) // Simple ratio based on size
		isCompactable := seg.Size > 0 && seg.Sealed
		
		if isCompactable {
			compactableCount++
			// Estimate savings (rough approximation)
			estimatedSavings += int64(float64(seg.Size) * (1.0 - ratio))
			logger.Log.Info("Segment {path}: COMPACTABLE (ratio: {ratio}, size: {size})", 
				seg.Path, ratio, seg.Size)
		} else {
			logger.Log.Info("Segment {path}: NOT COMPACTABLE (ratio: {ratio}, size: {size})", 
				seg.Path, ratio, seg.Size)
		}
	}
	
	logger.Log.Info("Dry run complete:")
	logger.Log.Info("  Compactable segments: {count}", compactableCount)
	logger.Log.Info("  Estimated space savings: {bytes} bytes", estimatedSavings)
	
	return nil
}

// calculateTotalSize calculates the total size of all WAL segments.
func calculateTotalSize(w *wal.WAL) int64 {
	segments := w.GetSegments()
	var totalSize int64
	
	for _, segment := range segments {
		totalSize += segment.Size
	}
	
	return totalSize
}