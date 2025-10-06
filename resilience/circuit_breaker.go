// Package resilience provides failure handling and recovery mechanisms.
package resilience

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// State represents the state of a circuit breaker
type State int32

const (
	// StateClosed allows requests to pass through
	StateClosed State = iota
	// StateOpen blocks all requests
	StateOpen
	// StateHalfOpen allows limited requests to test recovery
	StateHalfOpen
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitBreaker implements the circuit breaker pattern
type CircuitBreaker struct {
	lastFailureTime     time.Time
	lastOpenedAt        time.Time
	onStateChange       func(from, to State)
	name                string
	resetTimeout        time.Duration
	totalSuccesses      int64
	totalFailures       int64
	totalCalls          int64
	mu                  sync.RWMutex
	halfOpenMaxCalls    int32
	halfOpenCalls       int32
	successes           int32
	failures            int32
	state               int32
	consecutiveFailures int32
	maxFailures         int32
}

// CircuitBreakerConfig configures a circuit breaker
type CircuitBreakerConfig struct {
	OnStateChange    func(from, to State)
	Name             string
	ResetTimeout     time.Duration
	MaxFailures      int32
	HalfOpenMaxCalls int32
}

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(config CircuitBreakerConfig) *CircuitBreaker {
	if config.MaxFailures <= 0 {
		config.MaxFailures = 5
	}
	if config.ResetTimeout <= 0 {
		config.ResetTimeout = 60 * time.Second
	}
	if config.HalfOpenMaxCalls <= 0 {
		config.HalfOpenMaxCalls = 1
	}

	return &CircuitBreaker{
		name:             config.Name,
		maxFailures:      config.MaxFailures,
		resetTimeout:     config.ResetTimeout,
		halfOpenMaxCalls: config.HalfOpenMaxCalls,
		onStateChange:    config.OnStateChange,
		state:            int32(StateClosed),
	}
}

// Execute executes a function through the circuit breaker
func (cb *CircuitBreaker) Execute(fn func() error) error {
	if !cb.canExecute() {
		return fmt.Errorf("circuit breaker '%s' is open", cb.name)
	}

	atomic.AddInt64(&cb.totalCalls, 1)

	err := fn()

	if err != nil {
		cb.recordFailure()
	} else {
		cb.recordSuccess()
	}

	return err
}

// canExecute checks if execution is allowed
func (cb *CircuitBreaker) canExecute() bool {
	state := State(atomic.LoadInt32(&cb.state))

	switch state {
	case StateClosed:
		return true

	case StateOpen:
		// Check if we should transition to half-open
		cb.mu.RLock()
		shouldTransition := time.Since(cb.lastFailureTime) > cb.resetTimeout
		cb.mu.RUnlock()

		if shouldTransition {
			cb.transitionTo(StateHalfOpen)
			return true
		}
		return false

	case StateHalfOpen:
		// Allow limited calls in half-open state
		calls := atomic.AddInt32(&cb.halfOpenCalls, 1)
		return calls <= cb.halfOpenMaxCalls

	default:
		return false
	}
}

// recordFailure records a failure
func (cb *CircuitBreaker) recordFailure() {
	atomic.AddInt64(&cb.totalFailures, 1)
	failures := atomic.AddInt32(&cb.failures, 1)
	atomic.AddInt32(&cb.consecutiveFailures, 1)

	cb.mu.Lock()
	cb.lastFailureTime = time.Now()
	cb.mu.Unlock()

	state := State(atomic.LoadInt32(&cb.state))

	switch state {
	case StateClosed:
		if failures >= cb.maxFailures {
			cb.transitionTo(StateOpen)
		}

	case StateHalfOpen:
		// Any failure in half-open state reopens the circuit
		cb.transitionTo(StateOpen)
	}
}

// recordSuccess records a success
func (cb *CircuitBreaker) recordSuccess() {
	atomic.AddInt64(&cb.totalSuccesses, 1)
	atomic.StoreInt32(&cb.consecutiveFailures, 0)

	state := State(atomic.LoadInt32(&cb.state))

	switch state {
	case StateHalfOpen:
		successes := atomic.AddInt32(&cb.successes, 1)
		// Need enough successes in half-open state to close
		if successes >= cb.halfOpenMaxCalls {
			cb.transitionTo(StateClosed)
		}

	case StateClosed:
		// Reset failure count on success in closed state
		atomic.StoreInt32(&cb.failures, 0)
	}
}

// transitionTo transitions to a new state
func (cb *CircuitBreaker) transitionTo(newState State) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	oldState := State(atomic.LoadInt32(&cb.state))
	if oldState == newState {
		return
	}

	atomic.StoreInt32(&cb.state, int32(newState))

	// Reset counters based on new state
	switch newState {
	case StateClosed:
		atomic.StoreInt32(&cb.failures, 0)
		atomic.StoreInt32(&cb.successes, 0)
		atomic.StoreInt32(&cb.halfOpenCalls, 0)

	case StateOpen:
		cb.lastOpenedAt = time.Now()
		atomic.StoreInt32(&cb.successes, 0)
		atomic.StoreInt32(&cb.halfOpenCalls, 0)

	case StateHalfOpen:
		atomic.StoreInt32(&cb.failures, 0)
		atomic.StoreInt32(&cb.successes, 0)
		atomic.StoreInt32(&cb.halfOpenCalls, 0)
	}

	// Notify state change
	if cb.onStateChange != nil {
		cb.onStateChange(oldState, newState)
	}
}

// GetState returns the current state
func (cb *CircuitBreaker) GetState() State {
	return State(atomic.LoadInt32(&cb.state))
}

// GetStats returns circuit breaker statistics
func (cb *CircuitBreaker) GetStats() CircuitBreakerStats {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	return CircuitBreakerStats{
		Name:                cb.name,
		State:               State(atomic.LoadInt32(&cb.state)),
		TotalCalls:          atomic.LoadInt64(&cb.totalCalls),
		TotalFailures:       atomic.LoadInt64(&cb.totalFailures),
		TotalSuccesses:      atomic.LoadInt64(&cb.totalSuccesses),
		ConsecutiveFailures: atomic.LoadInt32(&cb.consecutiveFailures),
		LastFailureTime:     cb.lastFailureTime,
		LastOpenedAt:        cb.lastOpenedAt,
	}
}

// Reset resets the circuit breaker
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	atomic.StoreInt32(&cb.state, int32(StateClosed))
	atomic.StoreInt32(&cb.failures, 0)
	atomic.StoreInt32(&cb.successes, 0)
	atomic.StoreInt32(&cb.halfOpenCalls, 0)
	atomic.StoreInt32(&cb.consecutiveFailures, 0)
}

// CircuitBreakerStats contains circuit breaker statistics
type CircuitBreakerStats struct {
	LastFailureTime     time.Time
	LastOpenedAt        time.Time
	Name                string
	TotalCalls          int64
	TotalFailures       int64
	TotalSuccesses      int64
	State               State
	ConsecutiveFailures int32
}

// SuccessRate returns the success rate
func (s *CircuitBreakerStats) SuccessRate() float64 {
	if s.TotalCalls == 0 {
		return 0
	}
	return float64(s.TotalSuccesses) / float64(s.TotalCalls)
}
