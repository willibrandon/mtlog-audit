// Package main demonstrates basic usage of mtlog-audit.
package main

import (
	"time"

	"github.com/willibrandon/mtlog"
	audit "github.com/willibrandon/mtlog-audit"
	"github.com/willibrandon/mtlog/core"
)

func main() {
	// Create bulletproof audit sink for critical audit events
	auditSink, err := audit.New(
		audit.WithWAL("/tmp/example-audit.wal"),
		audit.WithPanicOnFailure(), // Panic if we can't write audit logs
	)
	if err != nil {
		panic("Failed to initialize audit sink: " + err.Error())
	}
	defer auditSink.Close()

	// Create logger using mtlog.New() with functional options
	logger := mtlog.New(
		mtlog.WithConsole(),
		mtlog.WithSink(auditSink),
		mtlog.WithMinimumLevel(core.InformationLevel),
	)

	// Log application startup
	logger.Info("mtlog-audit Basic Example")
	logger.Info("=========================")
	logger.Info("Audit sink initialized {walPath}", "/tmp/example-audit.wal")

	// Simulate some important events with message templates
	logger.Info("Application started with version {version}", "1.0.0")

	// User activity
	logger.Info("User {userId} logged in from {ip}", 123, "192.168.1.1")
	time.Sleep(10 * time.Millisecond)

	// Security event
	logger.Warn("Failed login attempt for user {userId} from {ip} after {attempts} attempts", 456, "10.0.0.1", 3)
	time.Sleep(10 * time.Millisecond)

	// System error
	logger.Error("Database connection lost to {database}: {error}", "audit_db", "timeout")
	time.Sleep(10 * time.Millisecond)

	// Business transaction
	logger.Info("Transaction {transactionId} processed for {amount:F2} {currency}", "tx-789", 99.99, "USD")
	time.Sleep(10 * time.Millisecond)

	// Verify integrity
	logger.Info("Verifying audit log integrity...")
	report, err := auditSink.VerifyIntegrity()
	if err != nil {
		logger.Fatal("Integrity verification failed: {error}", err)
	}

	if report.Valid {
		logger.Info("✅ Integrity check PASSED with {totalRecords} records", report.TotalRecords)
	} else {
		logger.Error("❌ Integrity check FAILED")
	}

	logger.Info("Example completed successfully!")
	logger.Info("WAL file saved to: {path}", "/tmp/example-audit.wal")
	logger.Info("You can verify it with: {command}", "./bin/mtlog-audit verify --wal /tmp/example-audit.wal")
}
