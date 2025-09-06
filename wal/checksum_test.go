package wal

import (
	"crypto/rand"
	"testing"
)

func TestChecksumAlgorithms(t *testing.T) {
	testData := []byte("Hello, World! This is a test of checksum algorithms.")
	
	tests := []struct {
		name string
		typ  ChecksumType
	}{
		{"CRC32", ChecksumCRC32},
		{"CRC32C", ChecksumCRC32C},
		{"CRC64", ChecksumCRC64},
		{"XXHash3", ChecksumXXHash3},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checksum := NewChecksum(tt.typ)
			
			// Calculate checksum
			sum := checksum.Calculate(testData)
			if sum == 0 {
				t.Error("Checksum returned 0")
			}
			
			// Verify checksum
			if !checksum.Verify(testData, sum) {
				t.Error("Checksum verification failed")
			}
			
			// Verify different data produces different checksum
			differentData := append(testData, byte('!'))
			if checksum.Verify(differentData, sum) {
				t.Error("Different data passed verification")
			}
		})
	}
}

func TestCompositeChecksum(t *testing.T) {
	testData := []byte("Test data for composite checksum")
	
	composite := NewCompositeChecksum(ChecksumCRC32C, ChecksumXXHash3)
	
	// Calculate composite checksum
	sum := composite.Calculate(testData)
	if sum == 0 {
		t.Error("Composite checksum returned 0")
	}
	
	// Verify composite checksum
	if !composite.Verify(testData, sum) {
		t.Error("Composite checksum verification failed")
	}
	
	// Test name includes both algorithms
	name := composite.Name()
	if name != "CRC32C+XXHash64" {
		t.Errorf("Expected name 'CRC32C+XXHash64', got '%s'", name)
	}
}

func TestBlockChecksum(t *testing.T) {
	// Create test data
	data := make([]byte, 10240) // 10KB
	rand.Read(data)
	
	bc := &BlockChecksum{
		BlockSize: 1024, // 1KB blocks
		Type:      ChecksumCRC32C,
	}
	
	// Calculate block checksums
	checksums := bc.CalculateBlocks(data)
	
	// Should have 10 checksums for 10KB data with 1KB blocks
	if len(checksums) != 10 {
		t.Errorf("Expected 10 checksums, got %d", len(checksums))
	}
	
	// Verify all blocks
	blockIdx, err := bc.VerifyBlocks(data, checksums)
	if err != nil {
		t.Errorf("Block verification failed: %v", err)
	}
	if blockIdx != -1 {
		t.Errorf("Expected no corrupted blocks, got block %d", blockIdx)
	}
	
	// Corrupt a block and verify detection
	data[2048] ^= 0xFF // Flip bits in block 2
	blockIdx, err = bc.VerifyBlocks(data, checksums)
	if err == nil {
		t.Error("Expected error for corrupted block")
	}
	if blockIdx != 2 {
		t.Errorf("Expected corrupted block 2, got block %d", blockIdx)
	}
}

func TestRollingChecksum(t *testing.T) {
	rc := NewRollingChecksum(16, ChecksumCRC32)
	
	// Add bytes to rolling window
	testBytes := []byte("This is a test of rolling checksum functionality")
	
	var lastChecksum uint64
	var unchangedCount int
	for i, b := range testBytes {
		checksum := rc.Update(b)
		
		// After filling window, checksum should usually change with each byte
		// But not always - some byte combinations might produce same checksum
		if i >= 16 && checksum == lastChecksum {
			unchangedCount++
		}
		lastChecksum = checksum
	}
	
	// Should have some changes, but not all need to change
	if unchangedCount == len(testBytes)-16 {
		t.Error("Rolling checksum never changing")
	}
	
	// Get final checksum
	finalChecksum := rc.GetChecksum()
	if finalChecksum == 0 {
		t.Error("Final rolling checksum is 0")
	}
}

func TestVerifyIntegrity(t *testing.T) {
	testData := []byte("Data to verify with multiple checksums")
	
	// Calculate checksums with different algorithms
	checksums := map[ChecksumType]uint64{
		ChecksumCRC32:  NewChecksum(ChecksumCRC32).Calculate(testData),
		ChecksumCRC64:  NewChecksum(ChecksumCRC64).Calculate(testData),
		ChecksumXXHash3: NewChecksum(ChecksumXXHash3).Calculate(testData),
	}
	
	// Verify all checksums pass
	err := VerifyIntegrity(testData, checksums)
	if err != nil {
		t.Errorf("Integrity verification failed: %v", err)
	}
	
	// Corrupt one checksum
	checksums[ChecksumCRC32] = 0xDEADBEEF
	
	// Verify detection of mismatch
	err = VerifyIntegrity(testData, checksums)
	if err == nil {
		t.Error("Expected integrity verification to fail")
	}
	
	checksumErr, ok := err.(*ChecksumError)
	if !ok {
		t.Error("Expected ChecksumError type")
	} else {
		if checksumErr.Type != ChecksumCRC32 {
			t.Errorf("Expected CRC32 type in error, got %v", checksumErr.Type)
		}
	}
}

func BenchmarkChecksum_CRC32C(b *testing.B) {
	data := make([]byte, 4096) // 4KB
	rand.Read(data)
	
	checksum := NewChecksum(ChecksumCRC32C)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = checksum.Calculate(data)
	}
	b.SetBytes(int64(len(data)))
}

func BenchmarkChecksum_XXHash3(b *testing.B) {
	data := make([]byte, 4096) // 4KB
	rand.Read(data)
	
	checksum := NewChecksum(ChecksumXXHash3)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = checksum.Calculate(data)
	}
	b.SetBytes(int64(len(data)))
}

func BenchmarkCompositeChecksum(b *testing.B) {
	data := make([]byte, 4096) // 4KB
	rand.Read(data)
	
	composite := NewCompositeChecksum(ChecksumCRC32C, ChecksumXXHash3)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = composite.Calculate(data)
	}
	b.SetBytes(int64(len(data)))
}

func TestChecksumConsistency(t *testing.T) {
	// Test that checksums are consistent across calls
	testData := []byte("Consistency test data")
	
	types := []ChecksumType{
		ChecksumCRC32,
		ChecksumCRC32C,
		ChecksumCRC64,
		ChecksumXXHash3,
	}
	
	for _, typ := range types {
		checksum := NewChecksum(typ)
		
		// Calculate multiple times
		sum1 := checksum.Calculate(testData)
		sum2 := checksum.Calculate(testData)
		sum3 := checksum.Calculate(testData)
		
		if sum1 != sum2 || sum2 != sum3 {
			t.Errorf("%s checksum not consistent: %x, %x, %x", 
				checksum.Name(), sum1, sum2, sum3)
		}
	}
}

func TestChecksumPooling(t *testing.T) {
	// Test that pooling works correctly
	data1 := []byte("First data")
	data2 := []byte("Second data")
	
	// Use CRC32 which uses pooling
	checksum := &CRC32Checksum{}
	
	// Calculate checksums in parallel to test pooling
	done := make(chan uint64, 2)
	
	go func() {
		done <- checksum.Calculate(data1)
	}()
	
	go func() {
		done <- checksum.Calculate(data2)
	}()
	
	sum1 := <-done
	sum2 := <-done
	
	// Verify different data produces different checksums
	if sum1 == sum2 {
		t.Error("Different data produced same checksum")
	}
	
	// Verify checksums are reproducible
	if checksum.Calculate(data1) != sum1 && checksum.Calculate(data1) != sum2 {
		t.Error("Checksum not reproducible")
	}
}

func TestEmptyDataChecksum(t *testing.T) {
	// Test checksums handle empty data correctly
	emptyData := []byte{}
	
	types := []ChecksumType{
		ChecksumCRC32,
		ChecksumCRC32C,
		ChecksumCRC64,
		ChecksumXXHash3,
	}
	
	for _, typ := range types {
		checksum := NewChecksum(typ)
		sum := checksum.Calculate(emptyData)
		
		// Empty data should still produce a checksum (not 0 for most algorithms)
		if !checksum.Verify(emptyData, sum) {
			t.Errorf("%s failed to verify empty data", checksum.Name())
		}
	}
}

func TestLargeDataChecksum(t *testing.T) {
	// Test with large data (1MB)
	largeData := make([]byte, 1024*1024)
	rand.Read(largeData)
	
	checksum := NewChecksum(ChecksumCRC32C) // Use hardware-accelerated version
	
	sum := checksum.Calculate(largeData)
	if !checksum.Verify(largeData, sum) {
		t.Error("Failed to verify large data checksum")
	}
	
	// Flip one bit and verify detection
	largeData[500000] ^= 0x01
	if checksum.Verify(largeData, sum) {
		t.Error("Failed to detect single bit flip in large data")
	}
}

func TestBlockChecksumPartialBlock(t *testing.T) {
	// Test with data that doesn't align to block size
	data := make([]byte, 2500) // 2.5KB
	rand.Read(data)
	
	bc := &BlockChecksum{
		BlockSize: 1024, // 1KB blocks
		Type:      ChecksumCRC32C,
	}
	
	checksums := bc.CalculateBlocks(data)
	
	// Should have 3 blocks (2 full + 1 partial)
	if len(checksums) != 3 {
		t.Errorf("Expected 3 checksums, got %d", len(checksums))
	}
	
	// Verify all blocks including partial
	_, err := bc.VerifyBlocks(data, checksums)
	if err != nil {
		t.Errorf("Failed to verify blocks with partial: %v", err)
	}
}