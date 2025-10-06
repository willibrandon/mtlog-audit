// Package logger provides internal logging utilities for mtlog-audit CLI.
package logger

import (
	"github.com/willibrandon/mtlog"
	"github.com/willibrandon/mtlog/core"
)

// Log is the internal logger for mtlog-audit
var Log core.Logger

func init() {
	// Create internal logger for mtlog-audit
	Log = mtlog.New(
		mtlog.WithConsole(),
		mtlog.WithMinimumLevel(core.InformationLevel),
	)
}
