// Package main demonstrates basic usage of mtlog-audit.
package main

import (
	"fmt"
	"log"
	"time"

	"github.com/willibrandon/mtlog/core"
	audit "github.com/willibrandon/mtlog-audit"
)

func main() {
	fmt.Println("mtlog-audit Basic Example")
	fmt.Println("=========================")
	
	// Create bulletproof audit sink
	auditSink, err := audit.New(
		audit.WithWAL("/tmp/example-audit.wal"),
		audit.WithPanicOnFailure(), // Panic if we can't write audit logs
	)
	if err != nil {
		log.Fatal("Failed to initialize audit sink:", err)
	}
	defer auditSink.Close()
	
	fmt.Println("✓ Audit sink initialized")
	
	// Simulate some important events
	events := []struct {
		level   core.LogEventLevel
		message string
		props   map[string]interface{}
	}{
		{
			level:   core.InformationLevel,
			message: "Application started",
			props:   map[string]interface{}{"version": "1.0.0"},
		},
		{
			level:   core.InformationLevel,
			message: "User login",
			props:   map[string]interface{}{"userId": 123, "ip": "192.168.1.1"},
		},
		{
			level:   core.WarningLevel,
			message: "Failed login attempt",
			props:   map[string]interface{}{"userId": 456, "ip": "10.0.0.1", "attempts": 3},
		},
		{
			level:   core.ErrorLevel,
			message: "Database connection lost",
			props:   map[string]interface{}{"database": "audit_db", "error": "timeout"},
		},
		{
			level:   core.InformationLevel,
			message: "Transaction processed",
			props:   map[string]interface{}{"transactionId": "tx-789", "amount": 99.99, "currency": "USD"},
		},
	}
	
	// Emit events
	for i, evt := range events {
		event := &core.LogEvent{
			Timestamp:       time.Now(),
			Level:           evt.level,
			MessageTemplate: evt.message,
			Properties:      evt.props,
		}
		
		auditSink.Emit(event)
		fmt.Printf("✓ Event %d logged: %s\n", i+1, evt.message)
		
		// Small delay to simulate real application
		time.Sleep(10 * time.Millisecond)
	}
	
	// Verify integrity
	fmt.Println("\nVerifying audit log integrity...")
	report, err := auditSink.VerifyIntegrity()
	if err != nil {
		log.Fatal("Integrity verification failed:", err)
	}
	
	if report.Valid {
		fmt.Println("✅ Integrity check PASSED")
		fmt.Printf("   Total records: %d\n", report.TotalRecords)
	} else {
		fmt.Println("❌ Integrity check FAILED")
	}
	
	fmt.Println("\nExample completed successfully!")
	fmt.Println("WAL file saved to: /tmp/example-audit.wal")
	fmt.Println("\nYou can verify it with:")
	fmt.Println("  ./bin/mtlog-audit verify --wal /tmp/example-audit.wal")
}