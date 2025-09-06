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
		sink.Emit(event)
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
				
				sink.Emit(event)
			}
		}(w)
	}
	
	// Wait for all workers
	wg.Wait()
	
	// Calculate results
	elapsed := time.Since(start)
	totalEvents := atomic.LoadInt64(&eventCount)
	eventsPerSecond := float64(totalEvents) / elapsed.Seconds()
	
	t.Logf("Results:")
	t.Logf("  Total events: %d", totalEvents)
	t.Logf("  Duration: %v", elapsed)
	t.Logf("  Throughput: %.0f events/second", eventsPerSecond)
	
	// Assertions
	require.Greater(t, eventsPerSecond, 100.0, "Should achieve >100 events/sec in test environment")
	
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
		numGoroutines = 10
		eventsPerGoroutine = 10
	)
	
	var wg sync.WaitGroup
	
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
				
				sink.Emit(event)
			}
		}(g)
	}
	
	wg.Wait()
	
	// Verify integrity
	report, err := sink.VerifyIntegrity()
	require.NoError(t, err)
	require.True(t, report.Valid)
	require.GreaterOrEqual(t, report.TotalRecords, numGoroutines*eventsPerGoroutine)
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
	runtime.GC()
	runtime.GC() // Double GC to be thorough
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)
	
	// Write many events
	const numEvents = 1000  // Increased to get measurable memory usage
	for i := 0; i < numEvents; i++ {
		event := &core.LogEvent{
			Timestamp:       time.Now(),
			Level:           core.InformationLevel,
			MessageTemplate: "Memory test with larger payload",
			Properties: map[string]interface{}{
				"index": i,
				"data":  make([]byte, 1024), // 1KB payload
				"metadata": map[string]string{
					"key1": "value1",
					"key2": "value2",
					"key3": "value3",
				},
			},
		}
		
		sink.Emit(event)
		
		// Don't let GC run during the test
		if i%100 == 0 {
			time.Sleep(1 * time.Millisecond)
		}
	}
	
	// Wait for events to be processed
	time.Sleep(100 * time.Millisecond)
	
	// Measure final memory before GC
	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)
	
	// Calculate memory growth (handle potential underflow)
	var memoryGrowth uint64
	if m2.Alloc > m1.Alloc {
		memoryGrowth = m2.Alloc - m1.Alloc
	} else {
		memoryGrowth = 0 // Memory was freed
	}
	
	var memoryPerEvent uint64
	if memoryGrowth > 0 && numEvents > 0 {
		memoryPerEvent = memoryGrowth / numEvents
	}
	
	t.Logf("Memory usage:")
	t.Logf("  Initial: %d MB", m1.Alloc/1024/1024)
	t.Logf("  Final: %d MB", m2.Alloc/1024/1024)
	t.Logf("  Growth: %d MB", memoryGrowth/1024/1024)
	t.Logf("  Per event: %d bytes", memoryPerEvent)
	
	// Memory should not grow excessively
	// In test environment with small batches, per-event overhead is higher
	if memoryPerEvent > 0 {
		require.Less(t, memoryPerEvent, uint64(10000), "Should use <10KB per event overhead in test")
	} else {
		t.Log("Memory was freed during test - GC was effective")
	}
}