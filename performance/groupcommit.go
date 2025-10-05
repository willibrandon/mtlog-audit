package performance

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/willibrandon/mtlog-audit/monitoring"
	"github.com/willibrandon/mtlog-audit/wal"
	"github.com/willibrandon/mtlog/core"
)

// GroupCommitter batches events for efficient writes
type GroupCommitter struct {
	mu         sync.Mutex
	wal        *wal.WAL
	batchSize  int
	maxDelay   time.Duration
	pending    []*core.LogEvent
	waiters    []chan error
	flushTimer *time.Timer
	closed     atomic.Bool
	flushChan  chan struct{}
	errorChan  chan error
	wg         sync.WaitGroup

	// Statistics
	batchCount   int64
	eventCount   int64
	flushCount   int64
	timerFlushes int64
	sizeFlushes  int64
	totalLatency int64 // nanoseconds
}

// NewGroupCommitter creates a new group committer
func NewGroupCommitter(w *wal.WAL, batchSize int, maxDelay time.Duration) *GroupCommitter {
	if batchSize <= 0 {
		batchSize = 100
	}
	if maxDelay <= 0 {
		maxDelay = 10 * time.Millisecond
	}

	gc := &GroupCommitter{
		wal:       w,
		batchSize: batchSize,
		maxDelay:  maxDelay,
		pending:   make([]*core.LogEvent, 0, batchSize),
		waiters:   make([]chan error, 0, batchSize),
		flushChan: make(chan struct{}, 1),
		errorChan: make(chan error, batchSize),
	}

	// Start background flusher
	gc.wg.Add(1)
	go gc.runFlusher()

	return gc
}

// Add adds an event to the batch
func (gc *GroupCommitter) Add(event *core.LogEvent) error {
	if gc.closed.Load() {
		return fmt.Errorf("group committer is closed")
	}

	startTime := time.Now()

	// Create waiter channel for this event
	waiter := make(chan error, 1)

	gc.mu.Lock()
	gc.pending = append(gc.pending, event)
	gc.waiters = append(gc.waiters, waiter)

	shouldFlush := len(gc.pending) >= gc.batchSize

	// Start timer if this is the first event in batch
	if len(gc.pending) == 1 && !shouldFlush {
		gc.flushTimer = time.AfterFunc(gc.maxDelay, func() {
			select {
			case gc.flushChan <- struct{}{}:
				atomic.AddInt64(&gc.timerFlushes, 1)
			default:
				// Flush already in progress
			}
		})
	}

	// Trigger flush if batch is full
	if shouldFlush {
		if gc.flushTimer != nil {
			gc.flushTimer.Stop()
			gc.flushTimer = nil
		}
		atomic.AddInt64(&gc.sizeFlushes, 1)
		select {
		case gc.flushChan <- struct{}{}:
		default:
			// Flush already triggered
		}
	}

	gc.mu.Unlock()

	// Wait for flush to complete
	err := <-waiter

	// Record latency
	latency := time.Since(startTime)
	atomic.AddInt64(&gc.totalLatency, latency.Nanoseconds())
	atomic.AddInt64(&gc.eventCount, 1)

	monitoring.RecordWriteLatency("groupcommit", latency, err == nil)

	return err
}

// AddBatch adds multiple events efficiently
func (gc *GroupCommitter) AddBatch(events []*core.LogEvent) error {
	if gc.closed.Load() {
		return fmt.Errorf("group committer is closed")
	}

	if len(events) == 0 {
		return nil
	}

	startTime := time.Now()

	// Create waiters for all events
	waiters := make([]chan error, len(events))
	for i := range waiters {
		waiters[i] = make(chan error, 1)
	}

	gc.mu.Lock()

	// Add all events
	gc.pending = append(gc.pending, events...)
	gc.waiters = append(gc.waiters, waiters...)

	// Check if we should flush
	if len(gc.pending) >= gc.batchSize {
		if gc.flushTimer != nil {
			gc.flushTimer.Stop()
			gc.flushTimer = nil
		}
		atomic.AddInt64(&gc.sizeFlushes, 1)
		select {
		case gc.flushChan <- struct{}{}:
		default:
		}
	} else if len(gc.pending) == len(events) {
		// First events in batch, start timer
		gc.flushTimer = time.AfterFunc(gc.maxDelay, func() {
			select {
			case gc.flushChan <- struct{}{}:
				atomic.AddInt64(&gc.timerFlushes, 1)
			default:
			}
		})
	}

	gc.mu.Unlock()

	// Wait for all events to be flushed
	var firstErr error
	for _, waiter := range waiters {
		if err := <-waiter; err != nil && firstErr == nil {
			firstErr = err
		}
	}

	// Record metrics
	latency := time.Since(startTime)
	atomic.AddInt64(&gc.totalLatency, latency.Nanoseconds())
	atomic.AddInt64(&gc.eventCount, int64(len(events)))

	monitoring.RecordWriteLatency("groupcommit_batch", latency, firstErr == nil)

	return firstErr
}

// runFlusher runs the background flush loop
func (gc *GroupCommitter) runFlusher() {
	defer gc.wg.Done()

	for {
		select {
		case <-gc.flushChan:
			gc.flush()

		case <-time.After(gc.maxDelay):
			// Periodic check for pending events
			gc.mu.Lock()
			if len(gc.pending) > 0 {
				gc.mu.Unlock()
				gc.flush()
			} else {
				gc.mu.Unlock()
			}
		}

		if gc.closed.Load() {
			// Final flush before exit
			gc.flush()
			return
		}
	}
}

// flush writes the pending batch to WAL
func (gc *GroupCommitter) flush() {
	gc.mu.Lock()

	if len(gc.pending) == 0 {
		gc.mu.Unlock()
		return
	}

	// Stop timer if running
	if gc.flushTimer != nil {
		gc.flushTimer.Stop()
		gc.flushTimer = nil
	}

	// Take ownership of pending events
	batch := gc.pending
	waiters := gc.waiters
	gc.pending = make([]*core.LogEvent, 0, gc.batchSize)
	gc.waiters = make([]chan error, 0, gc.batchSize)

	gc.mu.Unlock()

	// Write batch to WAL
	startTime := time.Now()
	var writeErr error

	// Write all events to WAL
	for _, event := range batch {
		if err := gc.wal.Write(event); err != nil {
			writeErr = err
			break
		}
	}

	// Single fsync for the entire batch
	if writeErr == nil {
		writeErr = gc.wal.Flush()
	}

	// Record metrics
	_ = time.Since(startTime) // flushLatency for future metrics
	monitoring.UpdateQueueDepth("groupcommit", 0)

	if writeErr == nil {
		atomic.AddInt64(&gc.batchCount, 1)
		atomic.AddInt64(&gc.flushCount, 1)
		monitoring.RecordEvent("success", "groupcommit")
	} else {
		monitoring.RecordEvent("failure", "groupcommit")
	}

	// Notify all waiters
	for _, waiter := range waiters {
		waiter <- writeErr
		close(waiter)
	}

	// Update batch size metric
	monitoring.UpdateQueueDepth("batch_size", len(batch))
}

// Close closes the group committer
func (gc *GroupCommitter) Close() error {
	if !gc.closed.CompareAndSwap(false, true) {
		return nil
	}

	// Trigger final flush
	select {
	case gc.flushChan <- struct{}{}:
	default:
	}

	// Wait for flusher to exit
	gc.wg.Wait()

	// Close channels
	close(gc.flushChan)
	close(gc.errorChan)

	return nil
}

// GetStats returns statistics
func (gc *GroupCommitter) GetStats() GroupCommitStats {
	eventCount := atomic.LoadInt64(&gc.eventCount)
	batchCount := atomic.LoadInt64(&gc.batchCount)
	totalLatency := atomic.LoadInt64(&gc.totalLatency)

	avgBatchSize := float64(0)
	if batchCount > 0 {
		avgBatchSize = float64(eventCount) / float64(batchCount)
	}

	avgLatency := time.Duration(0)
	if eventCount > 0 {
		avgLatency = time.Duration(totalLatency / eventCount)
	}

	return GroupCommitStats{
		EventCount:   eventCount,
		BatchCount:   batchCount,
		FlushCount:   atomic.LoadInt64(&gc.flushCount),
		TimerFlushes: atomic.LoadInt64(&gc.timerFlushes),
		SizeFlushes:  atomic.LoadInt64(&gc.sizeFlushes),
		AvgBatchSize: avgBatchSize,
		AvgLatency:   avgLatency,
		MaxBatchSize: gc.batchSize,
		MaxDelay:     gc.maxDelay,
	}
}

// GroupCommitStats contains group commit statistics
type GroupCommitStats struct {
	EventCount   int64
	BatchCount   int64
	FlushCount   int64
	TimerFlushes int64
	SizeFlushes  int64
	AvgBatchSize float64
	AvgLatency   time.Duration
	MaxBatchSize int
	MaxDelay     time.Duration
}

// OptimizedGroupCommitter uses lock-free techniques for higher throughput
type OptimizedGroupCommitter struct {
	wal        *wal.WAL
	ringBuffer *RingBuffer
	batchSize  int
	maxDelay   time.Duration
	closed     atomic.Bool
	eventCount int64
	errorCount int64
}

// NewOptimizedGroupCommitter creates an optimized group committer
func NewOptimizedGroupCommitter(w *wal.WAL, bufferSize int, batchSize int, maxDelay time.Duration) *OptimizedGroupCommitter {
	if bufferSize <= 0 {
		bufferSize = 10000
	}
	if batchSize <= 0 {
		batchSize = 100
	}
	if maxDelay <= 0 {
		maxDelay = 10 * time.Millisecond
	}

	ogc := &OptimizedGroupCommitter{
		wal:        w,
		ringBuffer: NewRingBuffer(bufferSize),
		batchSize:  batchSize,
		maxDelay:   maxDelay,
	}

	// Start background processor
	go ogc.processLoop()

	return ogc
}

// Add adds an event using lock-free operations
func (ogc *OptimizedGroupCommitter) Add(event *core.LogEvent) error {
	if ogc.closed.Load() {
		return fmt.Errorf("committer is closed")
	}

	if !ogc.ringBuffer.Write(event) {
		atomic.AddInt64(&ogc.errorCount, 1)
		return fmt.Errorf("ring buffer full")
	}

	atomic.AddInt64(&ogc.eventCount, 1)
	return nil
}

// processLoop processes events from the ring buffer
func (ogc *OptimizedGroupCommitter) processLoop() {
	ticker := time.NewTicker(ogc.maxDelay)
	defer ticker.Stop()

	batch := make([]*core.LogEvent, 0, ogc.batchSize)

	for {
		select {
		case <-ticker.C:
			// Timer-based flush
			if len(batch) > 0 {
				ogc.flushBatch(batch)
				batch = batch[:0]
			}

		default:
			// Try to read from ring buffer
			event := ogc.ringBuffer.Read()
			if event != nil {
				batch = append(batch, event)

				// Size-based flush
				if len(batch) >= ogc.batchSize {
					ogc.flushBatch(batch)
					batch = batch[:0]
				}
			} else {
				// No events available, sleep briefly
				time.Sleep(100 * time.Microsecond)
			}
		}

		if ogc.closed.Load() && ogc.ringBuffer.IsEmpty() {
			// Final flush
			if len(batch) > 0 {
				ogc.flushBatch(batch)
			}
			return
		}
	}
}

// flushBatch writes a batch to WAL
func (ogc *OptimizedGroupCommitter) flushBatch(batch []*core.LogEvent) {
	for _, event := range batch {
		if err := ogc.wal.Write(event); err != nil {
			atomic.AddInt64(&ogc.errorCount, 1)
			return
		}
	}

	// Single fsync for the batch
	if err := ogc.wal.Flush(); err != nil {
		atomic.AddInt64(&ogc.errorCount, 1)
	}
}

// Close closes the optimized committer
func (ogc *OptimizedGroupCommitter) Close() error {
	ogc.closed.Store(true)

	// Wait for ring buffer to drain
	for !ogc.ringBuffer.IsEmpty() {
		time.Sleep(10 * time.Millisecond)
	}

	return nil
}
