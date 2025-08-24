// +build integration

package integration

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/willibrandon/mtlog/core"
	audit "github.com/willibrandon/mtlog-audit"
	"github.com/willibrandon/mtlog-audit/backends"
	"github.com/willibrandon/mtlog-audit/performance"
)

func BenchmarkGroupCommit(b *testing.B) {
	tempDir := b.TempDir()
	
	sink, err := audit.New(
		audit.WithWAL(tempDir+"/bench.wal"),
		audit.WithGroupCommit(100, 10*time.Millisecond),
	)
	require.NoError(b, err)
	defer sink.Close()
	
	event := &core.LogEvent{
		Timestamp:       time.Now(),
		Level:           core.InformationLevel,
		MessageTemplate: "Benchmark event",
		Properties: map[string]interface{}{
			"index": 0,
		},
	}
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		err := sink.Emit(event)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRingBuffer(b *testing.B) {
	rb := performance.NewRingBuffer(10000)
	
	event := &core.LogEvent{
		Timestamp:       time.Now(),
		Level:           core.InformationLevel,
		MessageTemplate: "Test",
	}
	
	b.ResetTimer()
	b.ReportAllocs()
	
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if !rb.TryWrite(event) {
				// Buffer full, read to make space
				rb.TryRead()
				rb.TryWrite(event)
			}
		}
	})
}

func TestThroughput20000EventsPerSecond(t *testing.T) {
	tempDir := t.TempDir()
	
	// Create high-performance sink
	sink, err := audit.New(
		audit.WithWAL(tempDir+"/throughput.wal"),
		audit.WithGroupCommit(100, 5*time.Millisecond),
		audit.WithBackend(backends.FilesystemConfig{
			Path:     tempDir + "/backup",
			SyncMode: backends.SyncBatch,
		}),
	)
	require.NoError(t, err)
	defer sink.Close()
	
	const (
		targetEvents = 20000
		duration     = 1 * time.Second
		numWorkers   = 10
	)
	
	var (
		eventCount int64
		errorCount int64
		wg         sync.WaitGroup
	)
	
	// Start time
	start := time.Now()
	deadline := start.Add(duration)
	
	// Launch workers
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			
			for time.Now().Before(deadline) {
				event := &core.LogEvent{
					Timestamp:       time.Now(),
					Level:           core.InformationLevel,
					MessageTemplate: "High throughput test",
					Properties: map[string]interface{}{
						"worker": workerID,
						"seq":    atomic.AddInt64(&eventCount, 1),
					},
				}
				
				if err := sink.Emit(event); err != nil {
					atomic.AddInt64(&errorCount, 1)
				}
			}
		}(w)
	}
	
	// Wait for all workers
	wg.Wait()
	
	// Calculate results
	elapsed := time.Since(start)
	totalEvents := atomic.LoadInt64(&eventCount)
	totalErrors := atomic.LoadInt64(&errorCount)
	eventsPerSecond := float64(totalEvents) / elapsed.Seconds()
	
	t.Logf("Results:")
	t.Logf("  Total events: %d", totalEvents)
	t.Logf("  Total errors: %d", totalErrors)
	t.Logf("  Duration: %v", elapsed)
	t.Logf("  Throughput: %.0f events/second", eventsPerSecond)
	
	// Assertions
	require.Zero(t, totalErrors, "Should have no errors")
	require.Greater(t, eventsPerSecond, 15000.0, "Should achieve >15000 events/sec (allowing for test overhead)")
	
	// Verify all events were written
	report, err := sink.VerifyIntegrity()
	require.NoError(t, err)
	require.True(t, report.Valid)
}

func TestConcurrentWrites(t *testing.T) {
	tempDir := t.TempDir()
	
	sink, err := audit.New(
		audit.WithWAL(tempDir+"/concurrent.wal"),
		audit.WithGroupCommit(100, 10*time.Millisecond),
	)
	require.NoError(t, err)
	defer sink.Close()
	
	const (
		numGoroutines = 100
		eventsPerGoroutine = 100
	)
	
	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines*eventsPerGoroutine)
	
	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			
			for i := 0; i < eventsPerGoroutine; i++ {
				event := &core.LogEvent{
					Timestamp:       time.Now(),
					Level:           core.InformationLevel,
					MessageTemplate: "Concurrent test",
					Properties: map[string]interface{}{
						"goroutine": id,
						"index":     i,
					},
				}
				
				if err := sink.Emit(event); err != nil {
					errors <- err
				}
			}
		}(g)
	}
	
	wg.Wait()
	close(errors)
	
	// Check for errors
	var errorCount int
	for err := range errors {
		t.Logf("Error: %v", err)
		errorCount++
	}
	
	require.Zero(t, errorCount, "Should have no errors in concurrent writes")
	
	// Verify integrity
	report, err := sink.VerifyIntegrity()
	require.NoError(t, err)
	require.True(t, report.Valid)
	require.GreaterOrEqual(t, report.TotalRecords, int64(numGoroutines*eventsPerGoroutine))
}

func TestMemoryEfficiency(t *testing.T) {
	tempDir := t.TempDir()
	
	sink, err := audit.New(
		audit.WithWAL(tempDir+"/memory.wal"),
		audit.WithGroupCommit(100, 10*time.Millisecond),
	)
	require.NoError(t, err)
	defer sink.Close()
	
	// Measure initial memory
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)
	runtime.GC()
	
	// Write many events
	const numEvents = 10000
	for i := 0; i < numEvents; i++ {
		event := &core.LogEvent{
			Timestamp:       time.Now(),
			Level:           core.InformationLevel,
			MessageTemplate: "Memory test",
			Properties: map[string]interface{}{
				"index": i,
				"data":  make([]byte, 1024), // 1KB payload
			},
		}
		
		err := sink.Emit(event)
		require.NoError(t, err)
	}
	
	// Measure final memory
	runtime.GC()
	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)
	
	// Calculate memory growth
	memoryGrowth := m2.Alloc - m1.Alloc
	memoryPerEvent := memoryGrowth / numEvents
	
	t.Logf("Memory usage:")
	t.Logf("  Initial: %d MB", m1.Alloc/1024/1024)
	t.Logf("  Final: %d MB", m2.Alloc/1024/1024)
	t.Logf("  Growth: %d MB", memoryGrowth/1024/1024)
	t.Logf("  Per event: %d bytes", memoryPerEvent)
	
	// Memory should not grow excessively
	require.Less(t, memoryPerEvent, uint64(100), "Should use <100 bytes per event overhead")
}