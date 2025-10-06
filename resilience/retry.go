package resilience

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"sync/atomic"
	"time"
)

// RetryPolicy defines retry behavior for failed operations
type RetryPolicy struct {
	MaxAttempts     int              // Maximum number of retry attempts
	InitialDelay    time.Duration    // Initial delay between retries
	MaxDelay        time.Duration    // Maximum delay between retries
	Multiplier      float64          // Multiplier for exponential backoff
	Jitter          float64          // Jitter factor (0.0 to 1.0)
	RetryableErrors func(error) bool // Function to determine if error is retryable
}

// DefaultRetryPolicy returns a sensible default retry policy
func DefaultRetryPolicy() *RetryPolicy {
	return &RetryPolicy{
		MaxAttempts:     3,
		InitialDelay:    100 * time.Millisecond,
		MaxDelay:        5 * time.Second,
		Multiplier:      2.0,
		Jitter:          0.1,
		RetryableErrors: DefaultRetryableErrors,
	}
}

// DefaultRetryableErrors determines if an error is retryable
func DefaultRetryableErrors(err error) bool {
	// Add specific error types that should be retried
	// For now, retry everything except context cancellation
	if err == context.Canceled || err == context.DeadlineExceeded {
		return false
	}
	return true
}

// Execute executes a function with retry logic
func (p *RetryPolicy) Execute(fn func() error) error {
	return p.ExecuteWithContext(context.Background(), fn)
}

// ExecuteWithContext executes a function with retry logic and context
func (p *RetryPolicy) ExecuteWithContext(ctx context.Context, fn func() error) error {
	var lastErr error

	for attempt := 0; attempt < p.MaxAttempts; attempt++ {
		// Check context before attempting
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Execute the function
		err := fn()
		if err == nil {
			return nil // Success
		}

		lastErr = err

		// Check if error is retryable
		if p.RetryableErrors != nil && !p.RetryableErrors(err) {
			return err // Non-retryable error
		}

		// Don't sleep after the last attempt
		if attempt < p.MaxAttempts-1 {
			delay := p.calculateDelay(attempt)

			select {
			case <-time.After(delay):
				// Continue to next attempt
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	return fmt.Errorf("operation failed after %d attempts: %w", p.MaxAttempts, lastErr)
}

// calculateDelay calculates the delay for a given attempt
func (p *RetryPolicy) calculateDelay(attempt int) time.Duration {
	// Exponential backoff
	delay := float64(p.InitialDelay) * math.Pow(p.Multiplier, float64(attempt))

	// Cap at max delay
	if delay > float64(p.MaxDelay) {
		delay = float64(p.MaxDelay)
	}

	// Add jitter
	if p.Jitter > 0 {
		// #nosec G404 - weak random acceptable for jitter in retry backoff
		jitter := delay * p.Jitter * (2*rand.Float64() - 1) // Random between -jitter and +jitter
		delay += jitter

		// Ensure delay is positive
		if delay < 0 {
			delay = float64(p.InitialDelay)
		}
	}

	return time.Duration(delay)
}

// RetryStats tracks retry statistics
type RetryStats struct {
	TotalAttempts     int64
	SuccessfulRetries int64
	FailedRetries     int64
	TotalDelay        int64 // in nanoseconds
}

// RetryExecutor wraps retry logic with statistics
type RetryExecutor struct {
	policy *RetryPolicy
	stats  RetryStats
}

// NewRetryExecutor creates a new retry executor
func NewRetryExecutor(policy *RetryPolicy) *RetryExecutor {
	if policy == nil {
		policy = DefaultRetryPolicy()
	}
	return &RetryExecutor{
		policy: policy,
	}
}

// Execute executes with retry and tracks statistics
func (e *RetryExecutor) Execute(fn func() error) error {
	startTime := time.Now()
	attempts := 0

	err := e.policy.Execute(func() error {
		attempts++
		atomic.AddInt64(&e.stats.TotalAttempts, 1)
		return fn()
	})

	delay := time.Since(startTime).Nanoseconds()
	atomic.AddInt64(&e.stats.TotalDelay, delay)

	if err == nil && attempts > 1 {
		atomic.AddInt64(&e.stats.SuccessfulRetries, 1)
	} else if err != nil {
		atomic.AddInt64(&e.stats.FailedRetries, 1)
	}

	return err
}

// GetStats returns current statistics
func (e *RetryExecutor) GetStats() RetryStats {
	return RetryStats{
		TotalAttempts:     atomic.LoadInt64(&e.stats.TotalAttempts),
		SuccessfulRetries: atomic.LoadInt64(&e.stats.SuccessfulRetries),
		FailedRetries:     atomic.LoadInt64(&e.stats.FailedRetries),
		TotalDelay:        atomic.LoadInt64(&e.stats.TotalDelay),
	}
}

// BulkRetryPolicy handles retries for batch operations
type BulkRetryPolicy struct {
	*RetryPolicy
	PartialSuccess bool    // Allow partial success for batch operations
	MinSuccessRate float64 // Minimum success rate to consider operation successful
}

// ExecuteBatch executes a batch operation with retry logic
func (p *BulkRetryPolicy) ExecuteBatch(items []interface{}, fn func(interface{}) error) ([]error, error) {
	errors := make([]error, len(items))
	successCount := 0

	for i, item := range items {
		err := p.Execute(func() error {
			return fn(item)
		})

		if err != nil {
			errors[i] = err
		} else {
			successCount++
		}

		// Check if we should continue based on success rate
		if p.MinSuccessRate > 0 {
			currentRate := float64(successCount) / float64(i+1)
			if currentRate < p.MinSuccessRate && i > len(items)/4 {
				// Fail fast if success rate is too low after processing 25% of items
				return errors, fmt.Errorf("success rate too low: %.2f%% < %.2f%%",
					currentRate*100, p.MinSuccessRate*100)
			}
		}
	}

	// Check final success rate
	finalRate := float64(successCount) / float64(len(items))
	if !p.PartialSuccess && successCount < len(items) {
		return errors, fmt.Errorf("batch operation failed: %d/%d succeeded", successCount, len(items))
	}

	if p.MinSuccessRate > 0 && finalRate < p.MinSuccessRate {
		return errors, fmt.Errorf("success rate too low: %.2f%% < %.2f%%",
			finalRate*100, p.MinSuccessRate*100)
	}

	return errors, nil
}
