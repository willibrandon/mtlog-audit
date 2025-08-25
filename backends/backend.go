package backends

import (
	"fmt"
	"time"

	"github.com/willibrandon/mtlog/core"
)

// Backend defines the interface for storage backends
type Backend interface {
	// Write writes an event to the backend
	Write(event *core.LogEvent) error
	
	// WriteBatch writes multiple events efficiently
	WriteBatch(events []*core.LogEvent) error
	
	// Read reads events within a time range
	Read(start, end time.Time) ([]*core.LogEvent, error)
	
	// VerifyIntegrity verifies the integrity of stored data
	VerifyIntegrity() (*IntegrityReport, error)
	
	// Name returns the backend name
	Name() string
	
	// Close closes the backend
	Close() error
}

// IntegrityReport contains integrity verification results
type IntegrityReport struct {
	Timestamp        time.Time `json:"timestamp"`
	Backend          string    `json:"backend"`
	TotalRecords     int64     `json:"total_records"`
	VerifiedRecords  int64     `json:"verified_records"`
	CorruptedRecords int64     `json:"corrupted_records"`
	Valid            bool      `json:"valid"`
	Errors           []string  `json:"errors,omitempty"`
}

// Query represents a query for reading events
type Query struct {
	StartTime time.Time
	EndTime   time.Time
	Filters   map[string]interface{}
	Limit     int
}

// Config defines backend configuration
type Config interface {
	Type() string
	Validate() error
}

// FilesystemConfig configures a filesystem backend
type FilesystemConfig struct {
	Path      string        `json:"path"`
	SyncMode  SyncMode      `json:"sync_mode"`
	MaxSize   int64         `json:"max_size"`   // Max file size before rotation
	MaxAge    time.Duration `json:"max_age"`    // Max age before rotation
	Compress  bool          `json:"compress"`   // Compress rotated files
	Shadow    bool          `json:"shadow"`     // Shadow copy for redundancy
}

func (c FilesystemConfig) Type() string {
	return "filesystem"
}

func (c FilesystemConfig) Validate() error {
	if c.Path == "" {
		return fmt.Errorf("path is required")
	}
	if c.MaxSize <= 0 {
		c.MaxSize = 100 * 1024 * 1024 // 100MB default
	}
	if c.MaxAge <= 0 {
		c.MaxAge = 24 * time.Hour // 1 day default
	}
	return nil
}

// S3Config configures an S3 backend
type S3Config struct {
	Bucket          string        `json:"bucket"`
	Region          string        `json:"region"`
	Prefix          string        `json:"prefix"`
	StorageClass    string        `json:"storage_class"`
	ServerSideEncryption bool     `json:"server_side_encryption"`
	Versioning      bool          `json:"versioning"`
	ObjectLock      bool          `json:"object_lock"`
	RetentionDays   int           `json:"retention_days"`
}

func (c S3Config) Type() string {
	return "s3"
}

func (c S3Config) Validate() error {
	if c.Bucket == "" {
		return fmt.Errorf("bucket is required")
	}
	if c.Region == "" {
		return fmt.Errorf("region is required")
	}
	return nil
}

// AzureConfig configures an Azure Blob Storage backend
type AzureConfig struct {
	Container        string        `json:"container"`
	ConnectionString string        `json:"connection_string"`
	Prefix           string        `json:"prefix"`
	AccessTier       string        `json:"access_tier"`
	Immutable        bool          `json:"immutable"`
	RetentionDays    int           `json:"retention_days"`
}

func (c AzureConfig) Type() string {
	return "azure"
}

func (c AzureConfig) Validate() error {
	if c.Container == "" {
		return fmt.Errorf("container is required")
	}
	if c.ConnectionString == "" {
		return fmt.Errorf("connection string is required")
	}
	return nil
}

// GCSConfig configures a Google Cloud Storage backend
type GCSConfig struct {
	Bucket        string        `json:"bucket"`
	ProjectID     string        `json:"project_id"`
	Prefix        string        `json:"prefix"`
	StorageClass  string        `json:"storage_class"`
	RetentionDays int           `json:"retention_days"`
}

func (c GCSConfig) Type() string {
	return "gcs"
}

func (c GCSConfig) Validate() error {
	if c.Bucket == "" {
		return fmt.Errorf("bucket is required")
	}
	if c.ProjectID == "" {
		return fmt.Errorf("project ID is required")
	}
	return nil
}

// SyncMode defines synchronization modes
type SyncMode int

const (
	// SyncImmediate syncs after every write
	SyncImmediate SyncMode = iota
	// SyncInterval syncs periodically
	SyncInterval
	// SyncBatch syncs after batch
	SyncBatch
)

// Create creates a backend from configuration
func Create(config Config) (Backend, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}
	
	switch cfg := config.(type) {
	case FilesystemConfig:
		return NewFilesystemBackend(cfg)
	case S3Config:
		return NewS3Backend(cfg)
	case AzureConfig:
		return NewAzureBackend(cfg)
	case GCSConfig:
		return NewGCSBackend(cfg)
	default:
		return nil, fmt.Errorf("unknown backend type: %s", config.Type())
	}
}

// BackendError represents a backend-specific error
type BackendError struct {
	Backend string
	Op      string
	Err     error
}

func (e *BackendError) Error() string {
	return fmt.Sprintf("backend %s: %s: %v", e.Backend, e.Op, e.Err)
}

func (e *BackendError) Unwrap() error {
	return e.Err
}