// Package commands implements CLI commands for mtlog-audit.
package commands

import (
	"github.com/spf13/cobra"
	"github.com/willibrandon/mtlog-audit/internal/logger"
)

var (
	version string
	rootCmd = &cobra.Command{
		Use:   "mtlog-audit",
		Short: "Bulletproof audit log management",
		Long: `mtlog-audit provides tools for managing bulletproof audit logs.
		
The audit sink that cannot lose data, designed for compliance-critical
applications in financial services, healthcare, and government.`,
	}
)

// Execute runs the CLI.
func Execute(v string) error {
	version = v
	
	// Add commands
	rootCmd.AddCommand(
		versionCmd(),
		verifyCmd(),
		tortureCmd(),
		recoverCmd(),
		// TODO: Add more commands
		// replayCmd(),
		// monitorCmd(),
	)
	
	return rootCmd.Execute()
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			logger.Log.Info("mtlog-audit version {version}", version)
		},
	}
}