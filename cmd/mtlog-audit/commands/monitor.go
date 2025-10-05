package commands

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	audit "github.com/willibrandon/mtlog-audit"
	"github.com/willibrandon/mtlog-audit/internal/logger"
)

func monitorCmd() *cobra.Command {
	var (
		walPath        string
		checkInterval  string
		alertThreshold int
		output         string
	)

	cmd := &cobra.Command{
		Use:   "monitor",
		Short: "Monitor WAL health and performance",
		Long: `Monitor Write-Ahead Log health and performance in real-time.

This command continuously monitors WAL files for:
- Integrity issues
- Growth rate and disk usage
- Performance metrics
- Corruption detection
- Replication lag (if backends configured)`,
		Example: `  # Monitor WAL with default settings
  mtlog-audit monitor --wal /var/audit/mtlog.wal

  # Monitor with custom check interval
  mtlog-audit monitor --wal /var/audit/mtlog.wal --interval 30s

  # Monitor with alerts for high error rates
  mtlog-audit monitor --wal /var/audit/mtlog.wal --threshold 10

  # Output monitoring data to file
  mtlog-audit monitor --wal /var/audit/mtlog.wal --output monitor.log`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMonitor(walPath, checkInterval, alertThreshold, output)
		},
	}

	cmd.Flags().StringVar(&walPath, "wal", "", "Path to WAL file to monitor (required)")
	cmd.Flags().StringVar(&checkInterval, "interval", "10s", "Check interval (e.g., 10s, 1m, 5m)")
	cmd.Flags().IntVar(&alertThreshold, "threshold", 5, "Alert threshold for error rate (errors per interval)")
	cmd.Flags().StringVar(&output, "output", "", "Output monitoring data to file (default: stdout)")

	cmd.MarkFlagRequired("wal")

	return cmd
}

func runMonitor(walPath, intervalStr string, threshold int, output string) error {
	// Validate WAL path
	if walPath == "" {
		return fmt.Errorf("WAL path is required")
	}

	// Parse check interval
	interval, err := time.ParseDuration(intervalStr)
	if err != nil {
		return fmt.Errorf("invalid interval format: %w", err)
	}

	if interval < time.Second {
		return fmt.Errorf("interval must be at least 1 second")
	}

	logger.Log.Info("🔍 Starting WAL monitoring")
	logger.Log.Info("📁 WAL Path: {path}", walPath)
	logger.Log.Info("⏱️  Check Interval: {interval}", interval)
	logger.Log.Info("🚨 Alert Threshold: {threshold} errors per interval", threshold)
	if output != "" {
		logger.Log.Info("💾 Output File: {file}", output)
	}
	logger.Log.Info("")

	// Setup signal handling for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Monitoring state
	var lastSize uint64
	var lastSequence uint64
	var errorCount int
	var lastCheck time.Time

	// Create ticker for periodic checks
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	logger.Log.Info("📊 Monitoring started. Press Ctrl+C to stop.")
	logger.Log.Info("")

	// Initial check
	if err := performCheck(walPath, &lastSize, &lastSequence, &errorCount, &lastCheck, threshold); err != nil {
		logger.Log.Error("Initial check failed: {error}", err)
	}

	for {
		select {
		case <-ticker.C:
			if err := performCheck(walPath, &lastSize, &lastSequence, &errorCount, &lastCheck, threshold); err != nil {
				logger.Log.Error("Check failed: {error}", err)
				errorCount++
			}

		case sig := <-sigCh:
			logger.Log.Info("")
			logger.Log.Info("🛑 Received signal: {signal}", sig)
			logger.Log.Info("📊 Monitoring stopped.")
			return nil
		}
	}
}

func performCheck(walPath string, lastSize, lastSequence *uint64, errorCount *int, lastCheck *time.Time, threshold int) error {
	now := time.Now()

	// Try to create audit sink to access WAL
	sink, err := audit.New(
		audit.WithWAL(walPath),
	)
	if err != nil {
		logger.Log.Error("❌ Failed to open WAL: {error}", err)
		return err
	}
	defer sink.Close()

	// Check WAL file stats
	info, err := os.Stat(walPath)
	if err != nil {
		logger.Log.Error("❌ Failed to stat WAL file: {error}", err)
		return err
	}

	currentSize := uint64(info.Size())
	sizeGrowth := int64(currentSize - *lastSize)

	// Verify integrity
	report, err := sink.VerifyIntegrity()
	if err != nil {
		logger.Log.Error("❌ Integrity check failed: {error}", err)
		return err
	}

	currentSequence := report.WALIntegrity.LastSequence
	sequenceGrowth := currentSequence - *lastSequence

	// Calculate rates if we have previous data
	var growthRate, recordRate float64
	if !lastCheck.IsZero() {
		elapsed := now.Sub(*lastCheck).Seconds()
		if elapsed > 0 {
			growthRate = float64(sizeGrowth) / elapsed     // bytes per second
			recordRate = float64(sequenceGrowth) / elapsed // records per second
		}
	}

	// Log current status
	status := "✅"
	if !report.Valid {
		status = "❌"
		*errorCount++
	} else if report.CorruptedSegments > 0 {
		status = "⚠️"
	}

	logger.Log.Info("{status} {time} | Size: {size} bytes ({growth:+d}) | Records: {records} ({recGrowth:+d}) | Growth: {growthRate:.1f} B/s | Rate: {recordRate:.2f} rec/s",
		status, now.Format("15:04:05"), currentSize, sizeGrowth, report.TotalRecords, sequenceGrowth, growthRate, recordRate)

	// Check for alerts
	if *errorCount >= threshold {
		logger.Log.Error("🚨 ALERT: Error threshold exceeded! {errors} errors in monitoring period", *errorCount)
		*errorCount = 0 // Reset counter after alerting
	}

	if !report.Valid {
		logger.Log.Error("🚨 ALERT: WAL integrity issues detected!")
		logger.Log.Error("   - Corrupted segments: {count}", report.CorruptedSegments)
		logger.Log.Error("   - Total records: {count}", report.TotalRecords)
	}

	// Update state for next check
	*lastSize = currentSize
	*lastSequence = currentSequence
	*lastCheck = now

	return nil
}
