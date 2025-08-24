// Package torture implements comprehensive torture testing for the audit sink.
package torture

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	audit "github.com/willibrandon/mtlog-audit"
	"github.com/willibrandon/mtlog-audit/internal/logger"
)

// Suite orchestrates torture testing.
type Suite struct {
	scenarios []Scenario
	config    Config
	mu        sync.Mutex
}

// Scenario represents a torture test scenario.
type Scenario interface {
	Name() string
	Execute(sink *audit.Sink, dir string) error
	Verify(dir string) error
}

// Config configures the torture test suite.
type Config struct {
	Iterations     int
	StopOnFailure  bool
	TempDir        string
	Concurrency    int
	Verbose        bool
}

// Report contains the results of a torture test run.
type Report struct {
	StartTime  time.Time
	EndTime    time.Time
	Iterations int
	Scenarios  map[string]*ScenarioResult
	Success    bool
}

// ScenarioResult contains results for a single scenario.
type ScenarioResult struct {
	Passed   int
	Failed   int
	Errors   []error
	Duration time.Duration
}

// NewSuite creates a new torture test suite.
func NewSuite(cfg Config) *Suite {
	return &Suite{
		config:    cfg,
		scenarios: []Scenario{
			// Scenarios will be registered here
		},
	}
}

// RegisterScenario adds a scenario to the test suite.
func (s *Suite) RegisterScenario(scenario Scenario) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.scenarios = append(s.scenarios, scenario)
}

// Run executes the torture test suite.
func (s *Suite) Run() (*Report, error) {
	report := &Report{
		StartTime:  time.Now(),
		Iterations: s.config.Iterations,
		Scenarios:  make(map[string]*ScenarioResult),
	}

	// Initialize results for each scenario
	for _, scenario := range s.scenarios {
		report.Scenarios[scenario.Name()] = &ScenarioResult{}
	}

	// Run iterations
	for i := 0; i < s.config.Iterations; i++ {
		if s.config.Verbose {
			logger.Log.Info("Iteration {current}/{total}", i+1, s.config.Iterations)
		}

		for _, scenario := range s.scenarios {
			if err := s.runScenario(scenario, report); err != nil {
				if s.config.StopOnFailure {
					report.EndTime = time.Now()
					return report, err
				}
			}
		}

		// Progress reporting
		if i > 0 && i%100 == 0 && !s.config.Verbose {
			logger.Log.Info("Progress: {current}/{total} iterations", i, s.config.Iterations)
		}
	}

	report.EndTime = time.Now()
	report.Success = s.calculateSuccess(report)

	return report, nil
}

func (s *Suite) runScenario(scenario Scenario, report *Report) error {
	result := report.Scenarios[scenario.Name()]
	startTime := time.Now()

	// Create isolated test directory
	dir, err := os.MkdirTemp(s.config.TempDir, "torture-")
	if err != nil {
		result.Failed++
		result.Errors = append(result.Errors, err)
		return err
	}
	defer os.RemoveAll(dir)

	// Create sink
	sink, err := audit.New(
		audit.WithWAL(filepath.Join(dir, "test.wal")),
	)
	if err != nil {
		result.Failed++
		result.Errors = append(result.Errors, err)
		return err
	}

	// Execute scenario
	if err := scenario.Execute(sink, dir); err != nil {
		result.Failed++
		result.Errors = append(result.Errors, err)
		sink.Close()
		return err
	}

	// Close sink (simulates crash/shutdown)
	sink.Close()

	// Verify results
	if err := scenario.Verify(dir); err != nil {
		result.Failed++
		result.Errors = append(result.Errors, err)
		return err
	}

	result.Passed++
	result.Duration += time.Since(startTime)
	return nil
}

func (s *Suite) calculateSuccess(report *Report) bool {
	for _, result := range report.Scenarios {
		if result.Failed > 0 {
			return false
		}
	}
	return true
}

// PrintReport outputs a summary of the test results.
func (r *Report) PrintReport() {
	logger.Log.Info("")
	logger.Log.Info("=== TORTURE TEST REPORT ===")
	logger.Log.Info("Duration: {duration}", r.EndTime.Sub(r.StartTime))
	logger.Log.Info("Iterations: {count}", r.Iterations)
	logger.Log.Info("Overall Success: {success}", r.Success)
	logger.Log.Info("")

	for name, result := range r.Scenarios {
		logger.Log.Info("Scenario: {name}", name)
		logger.Log.Info("  Passed: {count}", result.Passed)
		logger.Log.Info("  Failed: {count}", result.Failed)
		if result.Failed > 0 && len(result.Errors) > 0 {
			logger.Log.Error("  Last Error: {error}", result.Errors[len(result.Errors)-1])
		}
		if result.Passed > 0 {
			avgDuration := result.Duration / time.Duration(result.Passed)
			logger.Log.Info("  Avg Duration: {duration}", avgDuration)
		}
		logger.Log.Info("")
	}

	// Final summary
	totalPassed := 0
	totalFailed := 0
	for _, result := range r.Scenarios {
		totalPassed += result.Passed
		totalFailed += result.Failed
	}

	logger.Log.Info("TOTAL: {passed} passed, {failed} failed", totalPassed, totalFailed)
	if r.Success {
		logger.Log.Info("✅ ALL TESTS PASSED")
	} else {
		logger.Log.Error("❌ SOME TESTS FAILED")
	}
}