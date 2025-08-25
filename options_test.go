package audit

import (
	"testing"
	"time"

	"github.com/willibrandon/mtlog-audit/backends"
	"github.com/willibrandon/mtlog-audit/compliance"
	"github.com/willibrandon/mtlog-audit/wal"
)

func TestWithBackend(t *testing.T) {
	tests := []struct {
		name    string
		backend backends.Config
		wantErr bool
	}{
		{
			name: "valid S3 backend",
			backend: backends.S3Config{
				Bucket:   "test-bucket",
				Region:   "us-east-1",
				Prefix:   "audit/",
			},
			wantErr: false,
		},
		{
			name: "invalid S3 backend - missing bucket",
			backend: backends.S3Config{
				Region: "us-east-1",
				Prefix: "audit/",
			},
			wantErr: true,
		},
		{
			name: "valid Azure backend",
			backend: backends.AzureConfig{
				Container:        "test-container",
				ConnectionString: "DefaultEndpointsProtocol=https;AccountName=test;AccountKey=key;EndpointSuffix=core.windows.net",
			},
			wantErr: false,
		},
		{
			name: "invalid Azure backend - missing container",
			backend: backends.AzureConfig{
				ConnectionString: "DefaultEndpointsProtocol=https;AccountName=test;AccountKey=key;EndpointSuffix=core.windows.net",
			},
			wantErr: true,
		},
		{
			name: "valid GCS backend",
			backend: backends.GCSConfig{
				Bucket:    "test-bucket",
				ProjectID: "test-project",
			},
			wantErr: false,
		},
		{
			name: "invalid GCS backend - missing bucket",
			backend: backends.GCSConfig{
				ProjectID: "test-project",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := defaultConfig()
			opt := WithBackend(tt.backend)
			err := opt(cfg)

			if (err != nil) != tt.wantErr {
				t.Errorf("WithBackend() error = %v, wantErr %v", err, tt.wantErr)
			}

			if err == nil && len(cfg.BackendConfigs) != 1 {
				t.Errorf("Expected 1 backend config, got %d", len(cfg.BackendConfigs))
			}
		})
	}
}

func TestMultipleBackends(t *testing.T) {
	cfg := defaultConfig()

	// Add multiple backends
	backends := []backends.Config{
		backends.S3Config{
			Bucket: "s3-bucket",
			Region: "us-east-1",
		},
		backends.AzureConfig{
			Container:        "azure-container",
			ConnectionString: "DefaultEndpointsProtocol=https;AccountName=test;AccountKey=key;EndpointSuffix=core.windows.net",
		},
		backends.GCSConfig{
			Bucket:    "gcs-bucket",
			ProjectID: "test-project",
		},
	}

	for _, backend := range backends {
		opt := WithBackend(backend)
		if err := opt(cfg); err != nil {
			t.Fatalf("Failed to add backend: %v", err)
		}
	}

	if len(cfg.BackendConfigs) != 3 {
		t.Errorf("Expected 3 backend configs, got %d", len(cfg.BackendConfigs))
	}

	// Verify each backend is present
	if cfg.BackendConfigs[0].Type() != "s3" {
		t.Errorf("Expected first backend to be s3, got %s", cfg.BackendConfigs[0].Type())
	}
	if cfg.BackendConfigs[1].Type() != "azure" {
		t.Errorf("Expected second backend to be azure, got %s", cfg.BackendConfigs[1].Type())
	}
	if cfg.BackendConfigs[2].Type() != "gcs" {
		t.Errorf("Expected third backend to be gcs, got %s", cfg.BackendConfigs[2].Type())
	}
}

func TestWithComplianceOptions(t *testing.T) {
	cfg := defaultConfig()
	
	// Create mock compliance options
	opts := []compliance.Option{
		compliance.WithEncryptionKey([]byte("test-key-32-bytes-long-exactly!!")),
		compliance.WithRetentionDays(90),
	}

	opt := WithComplianceOptions(opts...)
	if err := opt(cfg); err != nil {
		t.Fatalf("WithComplianceOptions failed: %v", err)
	}

	if len(cfg.ComplianceOptions) != 2 {
		t.Errorf("Expected 2 compliance options, got %d", len(cfg.ComplianceOptions))
	}
}

func TestWithCircuitBreakerOptions(t *testing.T) {
	cfg := defaultConfig()
	
	// Create mock circuit breaker options (as interface{} for now)
	opts := []interface{}{
		"option1",
		"option2",
		42,
	}

	opt := WithCircuitBreakerOptions(opts...)
	if err := opt(cfg); err != nil {
		t.Fatalf("WithCircuitBreakerOptions failed: %v", err)
	}

	if len(cfg.CircuitBreakerOptions) != 3 {
		t.Errorf("Expected 3 circuit breaker options, got %d", len(cfg.CircuitBreakerOptions))
	}
}

func TestWithMetricsOptions(t *testing.T) {
	cfg := defaultConfig()
	
	// Create mock metrics options (as interface{} for now)
	opts := []interface{}{
		"metrics1",
		"metrics2",
	}

	opt := WithMetricsOptions(opts...)
	if err := opt(cfg); err != nil {
		t.Fatalf("WithMetricsOptions failed: %v", err)
	}

	if len(cfg.MetricsOptions) != 2 {
		t.Errorf("Expected 2 metrics options, got %d", len(cfg.MetricsOptions))
	}
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: &Config{
				WALPath:           "/var/audit/test.wal",
				ComplianceProfile: "HIPAA",
			},
			wantErr: false,
		},
		{
			name: "missing WAL path",
			config: &Config{
				WALPath: "",
			},
			wantErr: true,
			errMsg:  "WAL path is required",
		},
		{
			name: "invalid compliance profile",
			config: &Config{
				WALPath:           "/var/audit/test.wal",
				ComplianceProfile: "INVALID_PROFILE",
			},
			wantErr: true,
			errMsg:  "invalid compliance profile",
		},
		{
			name: "valid empty compliance profile",
			config: &Config{
				WALPath:           "/var/audit/test.wal",
				ComplianceProfile: "", // Empty is valid
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Config.validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && tt.errMsg != "" && err.Error() != tt.errMsg {
				if !contains(err.Error(), tt.errMsg) {
					t.Errorf("Expected error message to contain %q, got %q", tt.errMsg, err.Error())
				}
			}
		})
	}
}

func TestCombinedOptions(t *testing.T) {
	// Test applying multiple options together
	cfg := defaultConfig()

	opts := []Option{
		WithWAL("/custom/path/audit.wal"),
		WithCompliance("HIPAA"),
		WithBackend(backends.S3Config{
			Bucket: "audit-bucket",
			Region: "us-west-2",
		}),
		WithGroupCommit(200, 50*time.Millisecond),
		WithPanicOnFailure(),
		WithWALSyncMode(wal.SyncBatch),
	}

	for _, opt := range opts {
		if err := opt(cfg); err != nil {
			t.Fatalf("Failed to apply option: %v", err)
		}
	}

	// Verify all options were applied
	if cfg.WALPath != "/custom/path/audit.wal" {
		t.Errorf("WAL path not set correctly: %s", cfg.WALPath)
	}
	if cfg.ComplianceProfile != "HIPAA" {
		t.Errorf("Compliance profile not set correctly: %s", cfg.ComplianceProfile)
	}
	if len(cfg.BackendConfigs) != 1 {
		t.Errorf("Backend not added correctly: %d backends", len(cfg.BackendConfigs))
	}
	if !cfg.GroupCommit {
		t.Error("Group commit not enabled")
	}
	if cfg.GroupCommitSize != 200 {
		t.Errorf("Group commit size not set correctly: %d", cfg.GroupCommitSize)
	}
	if !cfg.PanicOnFailure {
		t.Error("Panic on failure not enabled")
	}
	if len(cfg.WALOptions) != 1 {
		t.Errorf("WAL options not set correctly: %d options", len(cfg.WALOptions))
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && s[:len(substr)] == substr || 
		   len(s) >= len(substr) && contains(s[1:], substr)
}