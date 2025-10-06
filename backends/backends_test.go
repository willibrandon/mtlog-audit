package backends

import (
	"os"
	"testing"
	"time"

	"github.com/willibrandon/mtlog/core"
)

func TestS3ConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  S3Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: S3Config{
				Bucket: "test-bucket",
				Region: "us-east-1",
				Prefix: "audit/",
			},
			wantErr: false,
		},
		{
			name: "missing bucket",
			config: S3Config{
				Region: "us-east-1",
				Prefix: "audit/",
			},
			wantErr: true,
		},
		{
			name: "missing region",
			config: S3Config{
				Bucket: "test-bucket",
				Prefix: "audit/",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestAzureConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  AzureConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: AzureConfig{
				Container:        "test-container",
				ConnectionString: "DefaultEndpointsProtocol=https;AccountName=test;AccountKey=key;EndpointSuffix=core.windows.net",
			},
			wantErr: false,
		},
		{
			name: "missing container",
			config: AzureConfig{
				ConnectionString: "DefaultEndpointsProtocol=https;AccountName=test;AccountKey=key;EndpointSuffix=core.windows.net",
			},
			wantErr: true,
		},
		{
			name: "missing connection string",
			config: AzureConfig{
				Container: "test-container",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGCSConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  GCSConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: GCSConfig{
				Bucket:    "test-bucket",
				ProjectID: "test-project",
			},
			wantErr: false,
		},
		{
			name: "missing bucket",
			config: GCSConfig{
				ProjectID: "test-project",
			},
			wantErr: true,
		},
		{
			name: "missing project ID",
			config: GCSConfig{
				Bucket: "test-bucket",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestS3BackendWithMockCredentials(t *testing.T) {
	// Set mock AWS credentials to avoid the deprecated credential chain error
	if err := os.Setenv("AWS_ACCESS_KEY_ID", "mock-access-key"); err != nil {
		t.Fatalf("Failed to set AWS_ACCESS_KEY_ID: %v", err)
	}
	if err := os.Setenv("AWS_SECRET_ACCESS_KEY", "mock-secret-key"); err != nil {
		t.Fatalf("Failed to set AWS_SECRET_ACCESS_KEY: %v", err)
	}
	defer func() {
		_ = os.Unsetenv("AWS_ACCESS_KEY_ID")
		_ = os.Unsetenv("AWS_SECRET_ACCESS_KEY")
	}()

	config := S3Config{
		Bucket: "test-bucket",
		Region: "us-east-1",
		Prefix: "audit/",
	}

	backend, err := Create(config)
	if err == nil {
		// If it succeeds (unlikely without real bucket), close it
		_ = backend.Close()
	} else {
		// We expect an error about bucket not existing, not about credentials
		if contains(err.Error(), "NoCredentialProviders") || contains(err.Error(), "Deprecated") {
			t.Errorf("Got credential error when mock credentials were provided: %v", err)
		}
		// Bucket doesn't exist error is expected and fine
		t.Logf("Expected error for non-existent bucket: %v", err)
	}
}

func TestFilesystemBackend(t *testing.T) {
	tmpDir := t.TempDir()

	config := FilesystemConfig{
		Path: tmpDir,
	}

	backend, err := Create(config)
	if err != nil {
		t.Fatalf("Failed to create filesystem backend: %v", err)
	}
	defer func() {
		if err := backend.Close(); err != nil {
			t.Errorf("Failed to close backend: %v", err)
		}
	}()

	// Test writing an event
	event := &core.LogEvent{
		Timestamp:       time.Now(),
		Level:           core.InformationLevel,
		MessageTemplate: "Test event",
		Properties: map[string]interface{}{
			"test": "value",
		},
	}

	// Write should succeed for filesystem backend
	if err := backend.Write(event); err != nil {
		t.Errorf("Failed to write event to filesystem backend: %v", err)
	}
}

func TestBackendTypes(t *testing.T) {
	tests := []struct {
		config Config
		typ    string
	}{
		{S3Config{Bucket: "b", Region: "r"}, "s3"},
		{AzureConfig{Container: "c", ConnectionString: "cs"}, "azure"},
		{GCSConfig{Bucket: "b", ProjectID: "p"}, "gcs"},
		{FilesystemConfig{Path: "/tmp"}, "filesystem"},
	}

	for _, tt := range tests {
		if got := tt.config.Type(); got != tt.typ {
			t.Errorf("Config.Type() = %v, want %v", got, tt.typ)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || contains(s[1:], substr))
}
