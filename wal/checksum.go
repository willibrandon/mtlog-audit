package wal

import (
	"fmt"
	"hash"
	"hash/crc32"
	"hash/crc64"
	"sync"

	"github.com/cespare/xxhash/v2"
)

// Checksum provides multiple checksum algorithms for data integrity
type Checksum interface {
	// Calculate returns the checksum of data
	Calculate(data []byte) uint64
	// Verify checks if data matches the expected checksum
	Verify(data []byte, expected uint64) bool
	// Name returns the algorithm name
	Name() string
}

// ChecksumType represents different checksum algorithms
type ChecksumType int

const (
	// ChecksumCRC32 is the CRC32 (IEEE) checksum algorithm
	ChecksumCRC32 ChecksumType = iota
	// ChecksumCRC32C is the CRC32C (Castagnoli) checksum algorithm - hardware accelerated
	ChecksumCRC32C
	// ChecksumCRC64 is the CRC64 (ISO) checksum algorithm
	ChecksumCRC64
	// ChecksumXXHash3 is the XXHash64 non-cryptographic hash algorithm
	ChecksumXXHash3
)

// checksumPool provides object pooling for hash instances
var checksumPool = sync.Pool{
	New: func() interface{} {
		return &checksumState{
			crc32:  crc32.New(crc32.IEEETable),
			crc32c: crc32.New(crc32.MakeTable(crc32.Castagnoli)),
			crc64:  crc64.New(crc64.MakeTable(crc64.ISO)),
		}
	},
}

// checksumState holds reusable hash instances
type checksumState struct {
	crc32  hash.Hash32
	crc32c hash.Hash32
	crc64  hash.Hash64
}

// CRC32Checksum implements CRC32 (IEEE) checksum
type CRC32Checksum struct{}

// Calculate computes the CRC32 checksum of the given data.
func (c *CRC32Checksum) Calculate(data []byte) uint64 {
	state, ok := checksumPool.Get().(*checksumState)
	if !ok {
		panic("checksum pool returned invalid type")
	}
	defer checksumPool.Put(state)

	state.crc32.Reset()
	// #nosec G104 - hash.Hash.Write never returns an error
	state.crc32.Write(data)
	return uint64(state.crc32.Sum32())
}

// Verify checks if the data matches the expected CRC32 checksum.
func (c *CRC32Checksum) Verify(data []byte, expected uint64) bool {
	return c.Calculate(data) == expected
}

// Name returns the checksum algorithm name.
func (c *CRC32Checksum) Name() string {
	return "CRC32-IEEE"
}

// CRC32CChecksum implements CRC32C (Castagnoli) checksum
// This is hardware-accelerated on modern CPUs
type CRC32CChecksum struct{}

// Calculate computes the CRC32C checksum of the given data.
func (c *CRC32CChecksum) Calculate(data []byte) uint64 {
	state, ok := checksumPool.Get().(*checksumState)
	if !ok {
		panic("checksum pool returned invalid type")
	}
	defer checksumPool.Put(state)

	state.crc32c.Reset()
	// #nosec G104 - hash.Hash.Write never returns an error
	state.crc32c.Write(data)
	return uint64(state.crc32c.Sum32())
}

// Verify checks if the data matches the expected CRC32C checksum.
func (c *CRC32CChecksum) Verify(data []byte, expected uint64) bool {
	return c.Calculate(data) == expected
}

// Name returns the checksum algorithm name.
func (c *CRC32CChecksum) Name() string {
	return "CRC32C"
}

// CRC64Checksum implements CRC64 (ISO) checksum
type CRC64Checksum struct{}

// Calculate computes the CRC64 checksum of the given data.
func (c *CRC64Checksum) Calculate(data []byte) uint64 {
	state, ok := checksumPool.Get().(*checksumState)
	if !ok {
		panic("checksum pool returned invalid type")
	}
	defer checksumPool.Put(state)

	state.crc64.Reset()
	// #nosec G104 - hash.Hash.Write never returns an error
	state.crc64.Write(data)
	return state.crc64.Sum64()
}

// Verify checks if the data matches the expected CRC64 checksum.
func (c *CRC64Checksum) Verify(data []byte, expected uint64) bool {
	return c.Calculate(data) == expected
}

// Name returns the checksum algorithm name.
func (c *CRC64Checksum) Name() string {
	return "CRC64-ISO"
}

// XXHash3Checksum implements xxHash - extremely fast non-cryptographic hash
// Note: This uses xxHash (v2) which is production-ready and widely used
type XXHash3Checksum struct{}

// Calculate computes the xxHash checksum of the given data.
func (c *XXHash3Checksum) Calculate(data []byte) uint64 {
	return xxhash.Sum64(data)
}

// Verify checks if the data matches the expected xxHash checksum.
func (c *XXHash3Checksum) Verify(data []byte, expected uint64) bool {
	return c.Calculate(data) == expected
}

// Name returns the checksum algorithm name.
func (c *XXHash3Checksum) Name() string {
	return "XXHash64"
}

// NewChecksum creates a checksum calculator for the specified type
func NewChecksum(typ ChecksumType) Checksum {
	switch typ {
	case ChecksumCRC32:
		return &CRC32Checksum{}
	case ChecksumCRC32C:
		return &CRC32CChecksum{}
	case ChecksumCRC64:
		return &CRC64Checksum{}
	case ChecksumXXHash3:
		return &XXHash3Checksum{}
	default:
		return &CRC32CChecksum{} // Default to CRC32C
	}
}

// CompositeChecksum combines multiple checksums for extra protection
type CompositeChecksum struct {
	primary   Checksum
	secondary Checksum
}

// NewCompositeChecksum creates a checksum that uses two algorithms
func NewCompositeChecksum(primary, secondary ChecksumType) *CompositeChecksum {
	return &CompositeChecksum{
		primary:   NewChecksum(primary),
		secondary: NewChecksum(secondary),
	}
}

// Calculate computes the composite checksum using both algorithms.
func (c *CompositeChecksum) Calculate(data []byte) uint64 {
	p := c.primary.Calculate(data)
	s := c.secondary.Calculate(data)
	// Combine both checksums
	return (p << 32) | (s & 0xFFFFFFFF)
}

// Verify checks if the data matches the expected composite checksum.
func (c *CompositeChecksum) Verify(data []byte, expected uint64) bool {
	return c.Calculate(data) == expected
}

// Name returns the composite checksum algorithm name.
func (c *CompositeChecksum) Name() string {
	return c.primary.Name() + "+" + c.secondary.Name()
}

// VerifyIntegrity performs multi-level integrity verification
func VerifyIntegrity(data []byte, checksums map[ChecksumType]uint64) error {
	for typ, expected := range checksums {
		calculator := NewChecksum(typ)
		if !calculator.Verify(data, expected) {
			return &ChecksumError{
				Type:     typ,
				Expected: expected,
				Actual:   calculator.Calculate(data),
			}
		}
	}
	return nil
}

// ChecksumError represents a checksum mismatch error
type ChecksumError struct {
	Type     ChecksumType
	Expected uint64
	Actual   uint64
}

func (e *ChecksumError) Error() string {
	calculator := NewChecksum(e.Type)
	return fmt.Sprintf("checksum mismatch (%s): expected %x, got %x",
		calculator.Name(), e.Expected, e.Actual)
}

// BlockChecksum calculates checksums for data blocks
type BlockChecksum struct {
	BlockSize int
	Type      ChecksumType
}

// CalculateBlocks returns checksums for each block of data
func (bc *BlockChecksum) CalculateBlocks(data []byte) []uint64 {
	calculator := NewChecksum(bc.Type)
	blocks := (len(data) + bc.BlockSize - 1) / bc.BlockSize
	checksums := make([]uint64, blocks)

	for i := 0; i < blocks; i++ {
		start := i * bc.BlockSize
		end := start + bc.BlockSize
		if end > len(data) {
			end = len(data)
		}
		checksums[i] = calculator.Calculate(data[start:end])
	}

	return checksums
}

// VerifyBlocks verifies checksums for each block of data
func (bc *BlockChecksum) VerifyBlocks(data []byte, checksums []uint64) (int, error) {
	calculator := NewChecksum(bc.Type)
	blocks := (len(data) + bc.BlockSize - 1) / bc.BlockSize

	if len(checksums) != blocks {
		return -1, fmt.Errorf("checksum count mismatch: expected %d, got %d", blocks, len(checksums))
	}

	for i := 0; i < blocks; i++ {
		start := i * bc.BlockSize
		end := start + bc.BlockSize
		if end > len(data) {
			end = len(data)
		}

		actual := calculator.Calculate(data[start:end])
		if actual != checksums[i] {
			return i, &ChecksumError{
				Type:     bc.Type,
				Expected: checksums[i],
				Actual:   actual,
			}
		}
	}

	return -1, nil
}

// RollingChecksum provides a rolling checksum for streaming data
type RollingChecksum struct {
	hasher    hash.Hash64
	crc32Hash hash.Hash32
	window    []byte
	size      int
	pos       int
	sum       uint64
	typ       ChecksumType
	a         uint32
	b         uint32
}

// NewRollingChecksum creates a rolling checksum with specified window size
func NewRollingChecksum(windowSize int, typ ChecksumType) *RollingChecksum {
	rc := &RollingChecksum{
		window: make([]byte, windowSize),
		size:   windowSize,
		typ:    typ,
	}

	// Initialize hashers based on type
	switch typ {
	case ChecksumCRC32, ChecksumCRC32C:
		if typ == ChecksumCRC32 {
			rc.crc32Hash = crc32.New(crc32.IEEETable)
		} else {
			rc.crc32Hash = crc32.New(crc32.MakeTable(crc32.Castagnoli))
		}
	case ChecksumCRC64:
		rc.hasher = crc64.New(crc64.MakeTable(crc64.ISO))
	}

	// Initialize Adler32-style components for rolling
	rc.a = 1
	rc.b = 0

	return rc
}

// Update adds new data to the rolling window using incremental updates
func (rc *RollingChecksum) Update(b byte) uint64 {
	oldByte := rc.window[rc.pos]
	rc.window[rc.pos] = b
	oldPos := rc.pos
	rc.pos = (rc.pos + 1) % rc.size

	switch rc.typ {
	case ChecksumXXHash3:
		// XXHash doesn't support incremental rolling, use Adler32-style rolling hash
		// Remove old byte contribution
		rc.a = (rc.a - uint32(oldByte) + uint32(b)) % 65521
		// #nosec G115 - rc.size is window size, bounded and controlled
		rc.b = (rc.b - uint32(rc.size)*uint32(oldByte) + rc.a - 1) % 65521
		rc.sum = (uint64(rc.b) << 32) | uint64(rc.a)

	case ChecksumCRC32, ChecksumCRC32C:
		// CRC32 requires full recalculation for rolling window
		// But we can optimize by keeping the hasher state
		rc.crc32Hash.Reset()
		// Write from current position to end, then from start to current position
		if rc.pos < rc.size {
			// #nosec G104 - hash.Hash.Write never returns an error
			rc.crc32Hash.Write(rc.window[rc.pos:])
		}
		// #nosec G104 - hash.Hash.Write never returns an error
		rc.crc32Hash.Write(rc.window[:rc.pos])
		rc.sum = uint64(rc.crc32Hash.Sum32())

	case ChecksumCRC64:
		// Similar to CRC32, maintain hasher state
		rc.hasher.Reset()
		if rc.pos < rc.size {
			// #nosec G104 - hash.Hash.Write never returns an error
			rc.hasher.Write(rc.window[rc.pos:])
		}
		// #nosec G104 - hash.Hash.Write never returns an error
		rc.hasher.Write(rc.window[:rc.pos])
		rc.sum = rc.hasher.Sum64()

	default:
		// Fallback to full recalculation
		calculator := NewChecksum(rc.typ)
		rc.sum = calculator.Calculate(rc.window)
	}

	_ = oldPos // Keep for potential future optimizations
	return rc.sum
}

// GetChecksum returns the current rolling checksum
func (rc *RollingChecksum) GetChecksum() uint64 {
	return rc.sum
}
