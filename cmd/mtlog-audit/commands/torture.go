package commands

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/willibrandon/mtlog-audit/internal/logger"
	"github.com/willibrandon/mtlog-audit/torture"
	"github.com/willibrandon/mtlog-audit/torture/scenarios"
)

func tortureCmd() *cobra.Command {
	var (
		iterations    int
		stopOnFailure bool
		verbose       bool
		scenario      string
		parallel      int
	)

	cmd := &cobra.Command{
		Use:   "torture",
		Short: "Run torture tests to verify durability",
		Long: `Run comprehensive torture tests to verify that the audit sink
cannot lose data under extreme conditions.

Available scenarios:
- kill9: Simulates process termination during writes
- all: Run all available scenarios (default)

Examples:
  mtlog-audit torture --iterations 1000 --scenario kill9
  mtlog-audit torture --iterations 10000 --parallel 8  # Run with 8 workers
  mtlog-audit torture --iterations 1000000 --parallel 16  # Million iterations with 16 workers`,
		RunE: func(cmd *cobra.Command, args []string) error {
			logger.Log.Info("Starting torture tests...")
			logger.Log.Info("Iterations: {count}", iterations)
			logger.Log.Info("Scenario: {name}", scenario)
			if parallel > 1 {
				logger.Log.Info("Parallel workers: {count}", parallel)
			}
			logger.Log.Info("")

			// Use TMPDIR environment variable if set, otherwise default to /tmp
			tempDir := "/tmp"
			if envTmpDir := os.Getenv("TMPDIR"); envTmpDir != "" {
				tempDir = envTmpDir
			}

			// Configure test suite
			cfg := torture.Config{
				Iterations:    iterations,
				StopOnFailure: stopOnFailure,
				Verbose:       verbose,
				TempDir:       tempDir,
			}

			suite := torture.NewSuite(cfg)

			// Register scenarios based on selection
			switch scenario {
			case "kill9":
				suite.RegisterScenario(scenarios.NewKill9DuringWrite())
			case "diskfull":
				suite.RegisterScenario(scenarios.NewDiskFull())
			case "corruption":
				suite.RegisterScenario(scenarios.NewRandomCorruption())
			case "all":
				suite.RegisterScenario(scenarios.NewKill9DuringWrite())
				suite.RegisterScenario(scenarios.NewDiskFull())
				suite.RegisterScenario(scenarios.NewRandomCorruption())
			default:
				return fmt.Errorf("unknown scenario: %s (available: kill9, diskfull, corruption, all)", scenario)
			}

			// Run the torture tests
			var report *torture.Report
			var err error
			
			if parallel > 1 {
				report, err = suite.RunParallel(parallel)
			} else {
				report, err = suite.Run()
			}
			
			if err != nil && !stopOnFailure {
				return fmt.Errorf("torture test failed: %w", err)
			}

			// Print report
			report.PrintReport()

			if !report.Success {
				return fmt.Errorf("some torture tests failed")
			}

			return nil
		},
	}

	cmd.Flags().IntVar(&iterations, "iterations", 100, "Number of iterations to run")
	cmd.Flags().IntVar(&parallel, "parallel", 1, "Number of parallel workers (default 1, 0 = num CPUs)")
	cmd.Flags().BoolVar(&stopOnFailure, "stop-on-failure", false, "Stop on first failure")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Verbose output")
	cmd.Flags().StringVar(&scenario, "scenario", "all", "Scenario to run (kill9, all)")

	return cmd
}