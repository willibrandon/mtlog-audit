package wal

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"sync"
)

// DoubleWriteBuffer implements torn-write protection using a journal
// It ensures that writes are atomic even if the process crashes mid-write
type DoubleWriteBuffer struct {
	mu          sync.Mutex
	journal     *os.File
	bufferSize  int
	crcTable    *crc32.Table
}

// JournalEntry represents a write that needs to be made atomic
type JournalEntry struct {
	Magic      uint32   // Magic number to identify valid entries
	Status     uint8    // 0 = incomplete, 1 = complete, 2 = applied
	Position   int64    // Position in the main WAL file
	Length     uint32   // Length of the data
	CRC32      uint32   // CRC32 of the data
	Data       []byte   // The actual data to write
}

const (
	JournalMagic       = 0x4A524E4C // "JRNL"
	StatusIncomplete   = 0
	StatusComplete     = 1
	StatusApplied      = 2
)

// NewDoubleWriteBuffer creates a new double-write buffer
func NewDoubleWriteBuffer(journal *os.File, bufferSize int) (*DoubleWriteBuffer, error) {
	return &DoubleWriteBuffer{
		journal:    journal,
		bufferSize: bufferSize,
		crcTable:   crc32.MakeTable(crc32.IEEE),
	}, nil
}

// WriteToJournal writes data to the journal for torn-write protection
func (d *DoubleWriteBuffer) WriteToJournal(data []byte, position int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Create journal entry
	entry := JournalEntry{
		Magic:    JournalMagic,
		Status:   StatusIncomplete,
		Position: position,
		Length:   uint32(len(data)),
		CRC32:    crc32.Checksum(data, d.crcTable),
	}

	// Write entry header
	if err := binary.Write(d.journal, binary.LittleEndian, entry.Magic); err != nil {
		return fmt.Errorf("failed to write magic: %w", err)
	}
	if err := binary.Write(d.journal, binary.LittleEndian, entry.Status); err != nil {
		return fmt.Errorf("failed to write status: %w", err)
	}
	if err := binary.Write(d.journal, binary.LittleEndian, entry.Position); err != nil {
		return fmt.Errorf("failed to write position: %w", err)
	}
	if err := binary.Write(d.journal, binary.LittleEndian, entry.Length); err != nil {
		return fmt.Errorf("failed to write length: %w", err)
	}
	if err := binary.Write(d.journal, binary.LittleEndian, entry.CRC32); err != nil {
		return fmt.Errorf("failed to write CRC32: %w", err)
	}

	// Write data
	n, err := d.journal.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write data: %w", err)
	}
	if n != len(data) {
		return fmt.Errorf("incomplete write: wrote %d of %d bytes", n, len(data))
	}

	// Sync immediately for durability
	return d.journal.Sync()
}

// MarkComplete marks the last journal entry as complete
func (d *DoubleWriteBuffer) MarkComplete() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Seek back to status byte (after magic)
	if _, err := d.journal.Seek(-int64(d.getLastEntrySize())+4, 1); err != nil {
		return fmt.Errorf("failed to seek to status: %w", err)
	}

	// Write complete status
	if err := binary.Write(d.journal, binary.LittleEndian, uint8(StatusComplete)); err != nil {
		return fmt.Errorf("failed to write complete status: %w", err)
	}

	// Seek back to end
	if _, err := d.journal.Seek(0, 2); err != nil {
		return fmt.Errorf("failed to seek to end: %w", err)
	}

	// Sync to ensure status update is durable
	return d.journal.Sync()
}

// MarkIncomplete marks the last journal entry as incomplete
func (d *DoubleWriteBuffer) MarkIncomplete() error {
	// Status is already incomplete by default, just ensure it's synced
	return d.journal.Sync()
}

// RecoverIncomplete reads any incomplete journal entries that need to be replayed
func (d *DoubleWriteBuffer) RecoverIncomplete() ([]JournalEntry, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	var entries []JournalEntry

	// Seek to beginning
	if _, err := d.journal.Seek(0, 0); err != nil {
		return nil, fmt.Errorf("failed to seek to beginning: %w", err)
	}

	for {
		var entry JournalEntry

		// Read header
		var magic uint32
		if err := binary.Read(d.journal, binary.LittleEndian, &magic); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("failed to read magic: %w", err)
		}

		// Check magic number
		if magic != JournalMagic {
			break // End of valid entries
		}

		entry.Magic = magic

		// Read status
		if err := binary.Read(d.journal, binary.LittleEndian, &entry.Status); err != nil {
			return nil, fmt.Errorf("failed to read status: %w", err)
		}

		// Read position
		if err := binary.Read(d.journal, binary.LittleEndian, &entry.Position); err != nil {
			return nil, fmt.Errorf("failed to read position: %w", err)
		}

		// Read length
		if err := binary.Read(d.journal, binary.LittleEndian, &entry.Length); err != nil {
			return nil, fmt.Errorf("failed to read length: %w", err)
		}

		// Read CRC32
		if err := binary.Read(d.journal, binary.LittleEndian, &entry.CRC32); err != nil {
			return nil, fmt.Errorf("failed to read CRC32: %w", err)
		}

		// Read data
		entry.Data = make([]byte, entry.Length)
		n, err := io.ReadFull(d.journal, entry.Data)
		if err != nil {
			return nil, fmt.Errorf("failed to read data: %w", err)
		}
		if n != int(entry.Length) {
			return nil, fmt.Errorf("incomplete data read: read %d of %d bytes", n, entry.Length)
		}

		// Verify CRC32
		actualCRC := crc32.Checksum(entry.Data, d.crcTable)
		if actualCRC != entry.CRC32 {
			return nil, fmt.Errorf("CRC32 mismatch: expected %x, got %x", entry.CRC32, actualCRC)
		}

		// Only include incomplete entries
		if entry.Status == StatusIncomplete {
			entries = append(entries, entry)
		}
	}

	// Seek back to end for new writes
	if _, err := d.journal.Seek(0, 2); err != nil {
		return nil, fmt.Errorf("failed to seek to end: %w", err)
	}

	return entries, nil
}

// Clear truncates the journal file
func (d *DoubleWriteBuffer) Clear() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Truncate the journal
	if err := d.journal.Truncate(0); err != nil {
		return fmt.Errorf("failed to truncate journal: %w", err)
	}

	// Seek to beginning
	if _, err := d.journal.Seek(0, 0); err != nil {
		return fmt.Errorf("failed to seek to beginning: %w", err)
	}

	return d.journal.Sync()
}

// getLastEntrySize calculates the size of the last written entry
func (d *DoubleWriteBuffer) getLastEntrySize() int64 {
	// Get current position
	currentPos, _ := d.journal.Seek(0, 1)
	
	// Seek to beginning to scan
	d.journal.Seek(0, 0)
	defer d.journal.Seek(currentPos, 0)
	
	var lastEntrySize int64
	for {
		var magic uint32
		if err := binary.Read(d.journal, binary.LittleEndian, &magic); err != nil {
			break
		}
		if magic != JournalMagic {
			break
		}
		
		// Skip status
		d.journal.Seek(1, 1)
		
		// Skip position
		d.journal.Seek(8, 1)
		
		// Read length
		var length uint32
		if err := binary.Read(d.journal, binary.LittleEndian, &length); err != nil {
			break
		}
		
		// Skip CRC32
		d.journal.Seek(4, 1)
		
		// Skip data
		d.journal.Seek(int64(length), 1)
		
		// Calculate entry size: magic(4) + status(1) + position(8) + length(4) + crc32(4) + data(length)
		lastEntrySize = 4 + 1 + 8 + 4 + 4 + int64(length)
	}
	
	return lastEntrySize
}

// Compact removes completed entries from the journal
func (d *DoubleWriteBuffer) Compact() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Read all entries
	var validEntries []JournalEntry
	
	// Seek to beginning
	if _, err := d.journal.Seek(0, 0); err != nil {
		return fmt.Errorf("failed to seek to beginning: %w", err)
	}

	for {
		var entry JournalEntry

		// Read header
		var magic uint32
		if err := binary.Read(d.journal, binary.LittleEndian, &magic); err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("failed to read magic: %w", err)
		}

		if magic != JournalMagic {
			break
		}

		entry.Magic = magic

		// Read rest of entry
		if err := binary.Read(d.journal, binary.LittleEndian, &entry.Status); err != nil {
			return fmt.Errorf("failed to read status: %w", err)
		}
		if err := binary.Read(d.journal, binary.LittleEndian, &entry.Position); err != nil {
			return fmt.Errorf("failed to read position: %w", err)
		}
		if err := binary.Read(d.journal, binary.LittleEndian, &entry.Length); err != nil {
			return fmt.Errorf("failed to read length: %w", err)
		}
		if err := binary.Read(d.journal, binary.LittleEndian, &entry.CRC32); err != nil {
			return fmt.Errorf("failed to read CRC32: %w", err)
		}

		entry.Data = make([]byte, entry.Length)
		if _, err := io.ReadFull(d.journal, entry.Data); err != nil {
			return fmt.Errorf("failed to read data: %w", err)
		}

		// Only keep incomplete entries
		if entry.Status == StatusIncomplete {
			validEntries = append(validEntries, entry)
		}
	}

	// Truncate and rewrite
	if err := d.journal.Truncate(0); err != nil {
		return fmt.Errorf("failed to truncate journal: %w", err)
	}
	if _, err := d.journal.Seek(0, 0); err != nil {
		return fmt.Errorf("failed to seek to beginning: %w", err)
	}

	// Write back valid entries
	for _, entry := range validEntries {
		if err := binary.Write(d.journal, binary.LittleEndian, entry.Magic); err != nil {
			return fmt.Errorf("failed to write magic: %w", err)
		}
		if err := binary.Write(d.journal, binary.LittleEndian, entry.Status); err != nil {
			return fmt.Errorf("failed to write status: %w", err)
		}
		if err := binary.Write(d.journal, binary.LittleEndian, entry.Position); err != nil {
			return fmt.Errorf("failed to write position: %w", err)
		}
		if err := binary.Write(d.journal, binary.LittleEndian, entry.Length); err != nil {
			return fmt.Errorf("failed to write length: %w", err)
		}
		if err := binary.Write(d.journal, binary.LittleEndian, entry.CRC32); err != nil {
			return fmt.Errorf("failed to write CRC32: %w", err)
		}
		if _, err := d.journal.Write(entry.Data); err != nil {
			return fmt.Errorf("failed to write data: %w", err)
		}
	}

	return d.journal.Sync()
}