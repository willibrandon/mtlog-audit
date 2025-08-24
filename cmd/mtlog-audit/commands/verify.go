package commands

import (
	"fmt"

	"github.com/spf13/cobra"
	audit "github.com/willibrandon/mtlog-audit"
	"github.com/willibrandon/mtlog-audit/internal/logger"
)

func verifyCmd() *cobra.Command {
	var walPath string

	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Verify audit log integrity",
		Long: `Verify the integrity of an audit log WAL file.
		
This command checks:
- CRC32 checksums for corruption detection
- SHA256 hash chain for tamper detection
- Record sequence numbers for completeness
- Magic headers/footers for torn-write detection`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Create sink to access the WAL
			sink, err := audit.New(
				audit.WithWAL(walPath),
			)
			if err != nil {
				return fmt.Errorf("failed to open WAL: %w", err)
			}
			defer sink.Close()

			// Verify integrity
			logger.Log.Info("Verifying WAL: {path}", walPath)
			report, err := sink.VerifyIntegrity()
			if err != nil {
				return fmt.Errorf("verification failed: %w", err)
			}

			// Print results
			logger.Log.Info("")
			logger.Log.Info("=== INTEGRITY REPORT ===")
			if report.Valid {
				logger.Log.Info("✅ Integrity check PASSED")
			} else {
				logger.Log.Error("❌ Integrity check FAILED")
			}
			
			logger.Log.Info("")
			logger.Log.Info("Statistics:")
			logger.Log.Info("  Total records: {count}", report.TotalRecords)
			
			if report.WALIntegrity != nil {
				logger.Log.Info("  Last sequence: {seq}", report.WALIntegrity.LastSequence)
				if report.WALIntegrity.CorruptedSegments > 0 {
					logger.Log.Warn("  Corrupted segments: {count}", report.WALIntegrity.CorruptedSegments)
				}
				if report.WALIntegrity.RecoveredRecords > 0 {
					logger.Log.Info("  Recovered records: {count}", report.WALIntegrity.RecoveredRecords)
				}
			}
			
			if len(report.BackendErrors) > 0 {
				logger.Log.Error("Backend errors:")
				for _, err := range report.BackendErrors {
					logger.Log.Error("  - {error}", err)
				}
			}

			if !report.Valid {
				return fmt.Errorf("integrity check failed")
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&walPath, "wal", "/var/audit/app.wal", "Path to WAL file")
	cmd.MarkFlagRequired("wal")

	return cmd
}