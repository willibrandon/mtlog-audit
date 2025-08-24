package audit

import (
	"fmt"
	"time"

	"github.com/willibrandon/mtlog/core"
	"github.com/willibrandon/mtlog-audit/wal"
)

// Option configures the audit sink.
type Option func(*Config) error

// Config holds the audit sink configuration.
type Config struct {
	// Core configuration
	WALPath    string
	WALOptions []wal.Option

	// Compliance
	ComplianceProfile string
	// ComplianceOptions  []compliance.Option // TODO: Add when compliance is implemented

	// Backends
	// Backends []backends.Config // TODO: Add when backends are implemented

	// Resilience
	FailureHandler FailureHandler
	RetryPolicy    RetryPolicy
	// CircuitBreakerOptions []resilience.Option // TODO: Add when resilience is implemented
	PanicOnFailure bool

	// Performance
	GroupCommit      bool
	GroupCommitSize  int
	GroupCommitDelay time.Duration

	// Monitoring
	// MetricsOptions []monitoring.Option // TODO: Add when monitoring is implemented
}

// FailureHandler is called when audit write fails.
type FailureHandler func(event *core.LogEvent, err error)

// RetryPolicy defines retry behavior for failed operations.
type RetryPolicy struct {
	MaxAttempts  int
	InitialDelay time.Duration
	MaxDelay     time.Duration
	Multiplier   float64
}

// WithWAL configures the write-ahead log path.
func WithWAL(path string, opts ...wal.Option) Option {
	return func(c *Config) error {
		c.WALPath = path
		c.WALOptions = opts
		return nil
	}
}

// WithCompliance applies a compliance profile.
func WithCompliance(profile string) Option {
	return func(c *Config) error {
		c.ComplianceProfile = profile
		// TODO: Validate profile when compliance is implemented
		return nil
	}
}

// WithFailureHandler sets a custom failure handler.
func WithFailureHandler(handler FailureHandler) Option {
	return func(c *Config) error {
		c.FailureHandler = handler
		return nil
	}
}

// WithPanicOnFailure causes the sink to panic on write failure.
func WithPanicOnFailure() Option {
	return func(c *Config) error {
		c.PanicOnFailure = true
		return nil
	}
}

// WithGroupCommit enables group commit for better throughput.
func WithGroupCommit(size int, delay time.Duration) Option {
	return func(c *Config) error {
		if size <= 0 {
			return fmt.Errorf("group commit size must be positive")
		}
		if delay <= 0 {
			return fmt.Errorf("group commit delay must be positive")
		}
		c.GroupCommit = true
		c.GroupCommitSize = size
		c.GroupCommitDelay = delay
		return nil
	}
}

// WithRetryPolicy configures retry behavior.
func WithRetryPolicy(policy RetryPolicy) Option {
	return func(c *Config) error {
		if policy.MaxAttempts <= 0 {
			return fmt.Errorf("max attempts must be positive")
		}
		if policy.InitialDelay <= 0 {
			return fmt.Errorf("initial delay must be positive")
		}
		if policy.MaxDelay <= 0 {
			return fmt.Errorf("max delay must be positive")
		}
		if policy.Multiplier <= 1 {
			return fmt.Errorf("multiplier must be greater than 1")
		}
		c.RetryPolicy = policy
		return nil
	}
}

// defaultConfig returns the default configuration.
func defaultConfig() *Config {
	return &Config{
		WALPath: "/var/audit/mtlog.wal",
		RetryPolicy: RetryPolicy{
			MaxAttempts:  3,
			InitialDelay: 100 * time.Millisecond,
			MaxDelay:     5 * time.Second,
			Multiplier:   2.0,
		},
		GroupCommitSize:  100,
		GroupCommitDelay: 10 * time.Millisecond,
	}
}

// validate checks if the configuration is valid.
func (c *Config) validate() error {
	if c.WALPath == "" {
		return fmt.Errorf("WAL path is required")
	}

	// TODO: Validate compliance profile when implemented
	// if c.ComplianceProfile != "" {
	//     if !compliance.IsValidProfile(c.ComplianceProfile) {
	//         return fmt.Errorf("invalid compliance profile: %s", c.ComplianceProfile)
	//     }
	// }

	return nil
}