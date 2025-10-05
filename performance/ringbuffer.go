package performance

import (
	"runtime"
	"sync/atomic"
	"unsafe"

	"github.com/willibrandon/mtlog/core"
)

// RingBuffer is a lock-free ring buffer for high throughput
type RingBuffer struct {
	buffer   []*core.LogEvent
	size     uint64
	mask     uint64
	padding1 [128]byte // Cache line padding
	writePos uint64
	padding2 [128]byte // Cache line padding
	readPos  uint64
	padding3 [128]byte // Cache line padding
}

// NewRingBuffer creates a new ring buffer
// Size must be a power of 2
func NewRingBuffer(size int) *RingBuffer {
	// Round up to next power of 2
	actualSize := uint64(1)
	for actualSize < uint64(size) {
		actualSize <<= 1
	}

	return &RingBuffer{
		buffer: make([]*core.LogEvent, actualSize),
		size:   actualSize,
		mask:   actualSize - 1,
	}
}

// Write writes an event to the ring buffer (lock-free)
func (rb *RingBuffer) Write(event *core.LogEvent) bool {
	for {
		writePos := atomic.LoadUint64(&rb.writePos)
		readPos := atomic.LoadUint64(&rb.readPos)

		// Check if buffer is full
		if writePos-readPos >= rb.size {
			return false // Buffer full
		}

		// Try to claim the write position
		if atomic.CompareAndSwapUint64(&rb.writePos, writePos, writePos+1) {
			// Successfully claimed position
			index := writePos & rb.mask

			// Store the event
			atomic.StorePointer(
				(*unsafe.Pointer)(unsafe.Pointer(&rb.buffer[index])),
				unsafe.Pointer(event),
			)

			return true
		}

		// CAS failed, retry
		runtime.Gosched()
	}
}

// Read reads an event from the ring buffer (lock-free)
func (rb *RingBuffer) Read() *core.LogEvent {
	for {
		readPos := atomic.LoadUint64(&rb.readPos)
		writePos := atomic.LoadUint64(&rb.writePos)

		// Check if buffer is empty
		if readPos >= writePos {
			return nil // Buffer empty
		}

		// Try to claim the read position
		if atomic.CompareAndSwapUint64(&rb.readPos, readPos, readPos+1) {
			// Successfully claimed position
			index := readPos & rb.mask

			// Load the event
			event := (*core.LogEvent)(atomic.LoadPointer(
				(*unsafe.Pointer)(unsafe.Pointer(&rb.buffer[index])),
			))

			// Clear the slot
			atomic.StorePointer(
				(*unsafe.Pointer)(unsafe.Pointer(&rb.buffer[index])),
				nil,
			)

			return event
		}

		// CAS failed, retry
		runtime.Gosched()
	}
}

// TryWrite attempts to write without blocking
func (rb *RingBuffer) TryWrite(event *core.LogEvent) bool {
	writePos := atomic.LoadUint64(&rb.writePos)
	readPos := atomic.LoadUint64(&rb.readPos)

	// Check if buffer is full
	if writePos-readPos >= rb.size {
		return false
	}

	// Try to claim position (single attempt)
	if !atomic.CompareAndSwapUint64(&rb.writePos, writePos, writePos+1) {
		return false
	}

	// Store the event
	index := writePos & rb.mask
	atomic.StorePointer(
		(*unsafe.Pointer)(unsafe.Pointer(&rb.buffer[index])),
		unsafe.Pointer(event),
	)

	return true
}

// TryRead attempts to read without blocking
func (rb *RingBuffer) TryRead() *core.LogEvent {
	readPos := atomic.LoadUint64(&rb.readPos)
	writePos := atomic.LoadUint64(&rb.writePos)

	// Check if buffer is empty
	if readPos >= writePos {
		return nil
	}

	// Try to claim position (single attempt)
	if !atomic.CompareAndSwapUint64(&rb.readPos, readPos, readPos+1) {
		return nil
	}

	// Load the event
	index := readPos & rb.mask
	event := (*core.LogEvent)(atomic.LoadPointer(
		(*unsafe.Pointer)(unsafe.Pointer(&rb.buffer[index])),
	))

	// Clear the slot
	atomic.StorePointer(
		(*unsafe.Pointer)(unsafe.Pointer(&rb.buffer[index])),
		nil,
	)

	return event
}

// Size returns the current number of items in the buffer
func (rb *RingBuffer) Size() uint64 {
	writePos := atomic.LoadUint64(&rb.writePos)
	readPos := atomic.LoadUint64(&rb.readPos)
	return writePos - readPos
}

// Capacity returns the capacity of the ring buffer
func (rb *RingBuffer) Capacity() uint64 {
	return rb.size
}

// IsEmpty returns true if the buffer is empty
func (rb *RingBuffer) IsEmpty() bool {
	return rb.Size() == 0
}

// IsFull returns true if the buffer is full
func (rb *RingBuffer) IsFull() bool {
	return rb.Size() >= rb.size
}

// Clear clears the ring buffer
func (rb *RingBuffer) Clear() {
	// Reset positions
	atomic.StoreUint64(&rb.writePos, 0)
	atomic.StoreUint64(&rb.readPos, 0)

	// Clear all slots
	for i := range rb.buffer {
		atomic.StorePointer(
			(*unsafe.Pointer)(unsafe.Pointer(&rb.buffer[i])),
			nil,
		)
	}
}

// MultiProducerRingBuffer supports multiple concurrent writers
type MultiProducerRingBuffer struct {
	buffer      []*core.LogEvent
	size        uint64
	mask        uint64
	padding1    [128]byte
	writePos    uint64
	padding2    [128]byte
	writeCommit uint64
	padding3    [128]byte
	readPos     uint64
	padding4    [128]byte
}

// NewMultiProducerRingBuffer creates a multi-producer ring buffer
func NewMultiProducerRingBuffer(size int) *MultiProducerRingBuffer {
	actualSize := uint64(1)
	for actualSize < uint64(size) {
		actualSize <<= 1
	}

	return &MultiProducerRingBuffer{
		buffer: make([]*core.LogEvent, actualSize),
		size:   actualSize,
		mask:   actualSize - 1,
	}
}

// Write writes to the multi-producer buffer
func (mp *MultiProducerRingBuffer) Write(event *core.LogEvent) bool {
	// Reserve a slot
	writePos := atomic.AddUint64(&mp.writePos, 1) - 1
	readPos := atomic.LoadUint64(&mp.readPos)

	// Check if buffer is full
	if writePos-readPos >= mp.size {
		// Revert the increment
		atomic.AddUint64(&mp.writePos, ^uint64(0))
		return false
	}

	// Write to the slot
	index := writePos & mp.mask
	atomic.StorePointer(
		(*unsafe.Pointer)(unsafe.Pointer(&mp.buffer[index])),
		unsafe.Pointer(event),
	)

	// Wait for our turn to commit
	for atomic.LoadUint64(&mp.writeCommit) != writePos {
		runtime.Gosched()
	}

	// Commit our write
	atomic.StoreUint64(&mp.writeCommit, writePos+1)

	return true
}

// Read reads from the multi-producer buffer
func (mp *MultiProducerRingBuffer) Read() *core.LogEvent {
	readPos := atomic.LoadUint64(&mp.readPos)
	writeCommit := atomic.LoadUint64(&mp.writeCommit)

	// Check if buffer is empty
	if readPos >= writeCommit {
		return nil
	}

	// Try to claim the read position
	if !atomic.CompareAndSwapUint64(&mp.readPos, readPos, readPos+1) {
		return nil
	}

	// Read from the slot
	index := readPos & mp.mask
	event := (*core.LogEvent)(atomic.LoadPointer(
		(*unsafe.Pointer)(unsafe.Pointer(&mp.buffer[index])),
	))

	// Clear the slot
	atomic.StorePointer(
		(*unsafe.Pointer)(unsafe.Pointer(&mp.buffer[index])),
		nil,
	)

	return event
}

// BatchReader reads multiple events at once for efficiency
type BatchReader struct {
	buffer   *RingBuffer
	batch    []*core.LogEvent
	maxBatch int
}

// NewBatchReader creates a batch reader
func NewBatchReader(buffer *RingBuffer, maxBatch int) *BatchReader {
	if maxBatch <= 0 {
		maxBatch = 100
	}

	return &BatchReader{
		buffer:   buffer,
		batch:    make([]*core.LogEvent, 0, maxBatch),
		maxBatch: maxBatch,
	}
}

// ReadBatch reads up to maxBatch events
func (br *BatchReader) ReadBatch() []*core.LogEvent {
	br.batch = br.batch[:0]

	for i := 0; i < br.maxBatch; i++ {
		event := br.buffer.TryRead()
		if event == nil {
			break
		}
		br.batch = append(br.batch, event)
	}

	return br.batch
}
