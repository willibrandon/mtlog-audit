package commands

import (
	"fmt"

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
	)

	cmd := &cobra.Command{
		Use:   "torture",
		Short: "Run torture tests to verify durability",
		Long: `Run comprehensive torture tests to verify that the audit sink
cannot lose data under extreme conditions.

Available scenarios:
- kill9: Simulates process termination during writes
- all: Run all available scenarios (default)

Example:
  mtlog-audit torture --iterations 1000 --scenario kill9`,
		RunE: func(cmd *cobra.Command, args []string) error {
			logger.Log.Info("Starting torture tests...")
			logger.Log.Info("Iterations: {count}", iterations)
			logger.Log.Info("Scenario: {name}", scenario)
			logger.Log.Info("")

			// Configure test suite
			cfg := torture.Config{
				Iterations:    iterations,
				StopOnFailure: stopOnFailure,
				Verbose:       verbose,
			}

			suite := torture.NewSuite(cfg)

			// Register scenarios based on selection
			switch scenario {
			case "kill9":
				suite.RegisterScenario(scenarios.NewKill9DuringWrite())
			case "all":
				suite.RegisterScenario(scenarios.NewKill9DuringWrite())
				// TODO: Add more scenarios as they're implemented
				// suite.RegisterScenario(scenarios.NewDiskFull())
				// suite.RegisterScenario(scenarios.NewRandomCorruption())
			default:
				return fmt.Errorf("unknown scenario: %s", scenario)
			}

			// Run the torture tests
			report, err := suite.Run()
			if err != nil {
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
	cmd.Flags().BoolVar(&stopOnFailure, "stop-on-failure", false, "Stop on first failure")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Verbose output")
	cmd.Flags().StringVar(&scenario, "scenario", "all", "Scenario to run (kill9, all)")

	return cmd
}