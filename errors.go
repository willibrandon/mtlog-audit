// Package audit provides a bulletproof audit logging sink that cannot lose data.
package audit

import "errors"

var (
	// ErrSinkClosed is returned when attempting to use a closed sink.
	ErrSinkClosed = errors.New("audit sink is closed")

	// ErrWALCorrupted indicates WAL corruption has been detected.
	ErrWALCorrupted = errors.New("WAL corruption detected")

	// ErrWriteFailed indicates a write operation failed.
	ErrWriteFailed = errors.New("write failed")

	// ErrIntegrityFailed indicates an integrity check failed.
	ErrIntegrityFailed = errors.New("integrity check failed")

	// ErrComplianceViolation indicates a compliance requirement was violated.
	ErrComplianceViolation = errors.New("compliance violation")
)
