package commands

import (
	"fmt"

	"github.com/spf13/cobra"
	audit "github.com/willibrandon/mtlog-audit"
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
			fmt.Printf("Verifying WAL: %s\n", walPath)
			report, err := sink.VerifyIntegrity()
			if err != nil {
				return fmt.Errorf("verification failed: %w", err)
			}

			// Print results
			fmt.Println("\n=== INTEGRITY REPORT ===")
			if report.Valid {
				fmt.Println("✅ Integrity check PASSED")
			} else {
				fmt.Println("❌ Integrity check FAILED")
			}
			
			fmt.Printf("\nStatistics:\n")
			fmt.Printf("  Total records: %d\n", report.TotalRecords)
			
			if report.WALIntegrity != nil {
				fmt.Printf("  Last sequence: %d\n", report.WALIntegrity.LastSequence)
				if report.WALIntegrity.CorruptedSegments > 0 {
					fmt.Printf("  Corrupted segments: %d\n", report.WALIntegrity.CorruptedSegments)
				}
				if report.WALIntegrity.RecoveredRecords > 0 {
					fmt.Printf("  Recovered records: %d\n", report.WALIntegrity.RecoveredRecords)
				}
			}
			
			if len(report.BackendErrors) > 0 {
				fmt.Printf("\nBackend errors:\n")
				for _, err := range report.BackendErrors {
					fmt.Printf("  - %v\n", err)
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