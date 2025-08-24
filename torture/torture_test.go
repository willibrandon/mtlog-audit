//go:build torture
// +build torture

package torture

import (
	"testing"

	"github.com/willibrandon/mtlog-audit/torture/scenarios"
)

func TestTorture(t *testing.T) {
	// Configure the torture test
	cfg := Config{
		Iterations:    10, // Start with 10 for testing, will be 1000000 in production
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