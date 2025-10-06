package commands

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/willibrandon/mtlog-audit/internal/logger"
	"github.com/willibrandon/mtlog-audit/wal"
)

func recoverCmd() *cobra.Command {
	var (
		walPath        string
		outputPath     string
		skipCorrupted  bool
		verifyChecksum bool
		maxRecordSize  int64
	)

	cmd := &cobra.Command{
		Use:   "recover",
		Short: "Recover data from corrupted WAL files",
		Long: `Recover valid records from corrupted or damaged WAL files.

This command can:
- Extract valid records from partially corrupted files
- Skip over corrupted sections to recover remaining data
- Repair WAL files by writing recovered records to a new file
- Verify checksums during recovery

Example:
  mtlog-audit recover --wal /var/audit/corrupted.wal --output /var/audit/repaired.wal`,
		RunE: func(_ *cobra.Command, _ []string) error {
			logger.Log.Info("Starting recovery of {path}", walPath)

			// Create recovery engine
			engine := wal.NewRecoveryEngine(walPath,
				wal.WithSkipCorrupted(skipCorrupted),
				wal.WithChecksumVerification(verifyChecksum),
				wal.WithMaxRecordSize(maxRecordSize),
			)

			// If output path not specified, generate one
			if outputPath == "" {
				dir := filepath.Dir(walPath)
				base := filepath.Base(walPath)
				base = strings.TrimSuffix(base, filepath.Ext(base))
				timestamp := time.Now().Format("20060102-150405")
				outputPath = filepath.Join(dir, fmt.Sprintf("%s-recovered-%s.wal", base, timestamp))
			}

			// Perform recovery
			report, records, err := engine.Recover()

			// Print recovery report
			logger.Log.Info("")
			logger.Log.Info("=== RECOVERY REPORT ===")
			logger.Log.Info("Total records found: {count}", report.TotalRecords)
			logger.Log.Info("Records recovered: {count}", report.RecoveredRecords)
			logger.Log.Info("Corrupted records: {count}", report.CorruptedRecords)
			logger.Log.Info("Bytes skipped: {bytes}", report.SkippedBytes)

			if report.LastGoodSequence > 0 {
				logger.Log.Info("Last good sequence: {seq}", report.LastGoodSequence)
			}

			if len(report.Errors) > 0 {
				logger.Log.Warn("Errors encountered during recovery:")
				for i, err := range report.Errors {
					if i >= 10 {
						logger.Log.Warn("  ... and {count} more errors", len(report.Errors)-10)
						break
					}
					logger.Log.Warn("  - {error}", err)
				}
			}

			if err != nil && !skipCorrupted {
				return fmt.Errorf("recovery failed: %w", err)
			}

			if report.RecoveredRecords == 0 {
				logger.Log.Error("No records could be recovered")
				return fmt.Errorf("no recoverable data found")
			}

			// Write recovered records to new file
			logger.Log.Info("")
			logger.Log.Info("Writing recovered records to {path}", outputPath)

			if err := engine.RepairWAL(outputPath); err != nil {
				return fmt.Errorf("failed to write repaired WAL: %w", err)
			}

			logger.Log.Info("✅ Recovery complete!")
			logger.Log.Info("Recovered {count} records to {path}", len(records), outputPath)

			return nil
		},
	}

	cmd.Flags().StringVar(&walPath, "wal", "", "Path to corrupted WAL file")
	cmd.Flags().StringVar(&outputPath, "output", "", "Output path for repaired WAL (auto-generated if not specified)")
	cmd.Flags().BoolVar(&skipCorrupted, "skip-corrupted", true, "Skip corrupted records and continue recovery")
	cmd.Flags().BoolVar(&verifyChecksum, "verify-checksum", true, "Verify CRC32 checksums during recovery")
	cmd.Flags().Int64Var(&maxRecordSize, "max-record-size", 10*1024*1024, "Maximum expected record size in bytes")

	_ = cmd.MarkFlagRequired("wal")

	return cmd
}
