//go:build torture
// +build torture

package torture

import (
	"os"
	"testing"

	"github.com/willibrandon/mtlog-audit/torture/scenarios"
)

func TestTorture(t *testing.T) {
	// Configure the torture test
	iterations := 10
	if testing.Short() {
		iterations = 10 // Quick test for CI
	} else if os.Getenv("TORTURE_PRODUCTION") == "true" {
		iterations = 1000000 // Full production torture test
	} else {
		iterations = 1000 // Standard test run
	}
	
	cfg := Config{
		Iterations:    iterations,
		StopOnFailure: false,
		Verbose:       testing.Verbose(),
	}

	// Create test suite
	suite := NewSuite(cfg)

	// Register scenarios
	suite.RegisterScenario(scenarios.NewKill9DuringWrite())

	// TODO: Add more scenarios
	// suite.RegisterScenario(scenarios.NewDiskFull())
	// suite.RegisterScenario(scenarios.NewRandomCorruption())
	// suite.RegisterScenario(scenarios.NewNetworkPartition())

	// Run the torture tests
	report, err := suite.Run()
	if err != nil {
		t.Fatalf("Torture test failed: %v", err)
	}

	// Print report
	report.PrintReport()

	// Check for failures
	if !report.Success {
		t.Errorf("Torture tests failed")
		for name, result := range report.Scenarios {
			if result.Failed > 0 {
				t.Errorf("Scenario %s: %d failures", name, result.Failed)
				if len(result.Errors) > 0 {
					t.Errorf("  Last error: %v", result.Errors[len(result.Errors)-1])
				}
			}
		}
	}
}

func TestQuickTorture(t *testing.T) {
	// A quicker version for CI
	cfg := Config{
		Iterations:    1,
		StopOnFailure: true,
		Verbose:       true,
	}

	suite := NewSuite(cfg)
	suite.RegisterScenario(scenarios.NewKill9DuringWrite())

	report, err := suite.Run()
	if err != nil {
		t.Fatalf("Quick torture test failed: %v", err)
	}

	if !report.Success {
		t.Error("Quick torture test failed")
	}
}