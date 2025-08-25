package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/pprof"
	"time"

	audit "github.com/willibrandon/mtlog-audit"
	"github.com/willibrandon/mtlog/core"
)

func main() {
	// Create CPU profile
	f, err := os.Create("cpu.prof")
	if err != nil {
		panic(err)
	}
	defer f.Close()

	if err := pprof.StartCPUProfile(f); err != nil {
		panic(err)
	}
	defer pprof.StopCPUProfile()

	// Create temp dir
	tmpDir, err := os.MkdirTemp("", "profile-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create sink
	sink, err := audit.New(
		audit.WithWAL(filepath.Join(tmpDir, "test.wal")),
	)
	if err != nil {
		panic(err)
	}
	defer sink.Close()

	// Write 100 events
	start := time.Now()
	for i := 0; i < 100; i++ {
		event := &core.LogEvent{
			Timestamp:       time.Now(),
			Level:           core.InformationLevel,
			MessageTemplate: fmt.Sprintf("Test event %d", i),
			Properties: map[string]interface{}{
				"Index":     i,
				"Timestamp": time.Now().UnixNano(),
				"Random":    i * 42,
			},
		}
		sink.Emit(event)
	}
	
	fmt.Printf("Time to write 100 events: %v\n", time.Since(start))
}