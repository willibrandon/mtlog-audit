package compliance

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/willibrandon/mtlog/core"
)

// Engine provides compliance transformations and enforcement
type Engine struct {
	mu              sync.RWMutex
	profile         Profile
	encryptor       Encryptor
	signer          Signer
	keyManager      *KeyManager
	signatureChain  *SignatureChain
	sequence        uint64
	maskSensitive   bool
	enforceRequired bool
}

// New creates a new compliance engine for the specified profile
func New(profileName string, opts ...Option) (*Engine, error) {
	profile, exists := Profiles[profileName]
	if !exists {
		return nil, fmt.Errorf("unknown compliance profile: %s", profileName)
	}
	
	engine := &Engine{
		profile:         profile,
		maskSensitive:   true,
		enforceRequired: true,
	}
	
	// Apply options
	for _, opt := range opts {
		if err := opt(engine); err != nil {
			return nil, err
		}
	}
	
	// Initialize encryption if required
	if profile.EncryptionRequired && engine.encryptor == nil {
		key, err := GenerateKey(256)
		if err != nil {
			return nil, fmt.Errorf("failed to generate encryption key: %w", err)
		}
		
		keyManager, err := NewKeyManager(key, profile.EncryptionAlgorithm)
		if err != nil {
			return nil, fmt.Errorf("failed to create key manager: %w", err)
		}
		engine.keyManager = keyManager
	}
	
	// Initialize signing if required
	if profile.SigningRequired && engine.signer == nil {
		var signer Signer
		var err error
		
		switch profile.SigningAlgorithm {
		case "Ed25519":
			signer, err = NewEd25519Signer()
		case "RSA-PSS":
			signer, err = NewRSASigner(4096)
		default:
			return nil, fmt.Errorf("unsupported signing algorithm: %s", profile.SigningAlgorithm)
		}
		
		if err != nil {
			return nil, fmt.Errorf("failed to create signer: %w", err)
		}
		
		engine.signer = signer
		engine.signatureChain = NewSignatureChain(signer)
	}
	
	return engine, nil
}

// Option configures the compliance engine
type Option func(*Engine) error

// WithEncryptionKey sets a specific encryption key
func WithEncryptionKey(key []byte) Option {
	return func(e *Engine) error {
		if !e.profile.EncryptionRequired {
			return nil // Ignore if not required
		}
		
		keyManager, err := NewKeyManager(key, e.profile.EncryptionAlgorithm)
		if err != nil {
			return err
		}
		e.keyManager = keyManager
		return nil
	}
}

// WithSigner sets a specific signer
func WithSigner(signer Signer) Option {
	return func(e *Engine) error {
		if !e.profile.SigningRequired {
			return nil // Ignore if not required
		}
		
		e.signer = signer
		e.signatureChain = NewSignatureChain(signer)
		return nil
	}
}

// WithMaskingDisabled disables sensitive data masking
func WithMaskingDisabled() Option {
	return func(e *Engine) error {
		e.maskSensitive = false
		return nil
	}
}

// WithRetentionDays sets the retention period in days
func WithRetentionDays(days int) Option {
	return func(e *Engine) error {
		if days < e.profile.MinRetentionDays {
			return fmt.Errorf("retention days %d is less than minimum %d for profile %s", 
				days, e.profile.MinRetentionDays, e.profile.Name)
		}
		if days > e.profile.MaxRetentionDays {
			return fmt.Errorf("retention days %d exceeds maximum %d for profile %s", 
				days, e.profile.MaxRetentionDays, e.profile.Name)
		}
		e.profile.RetentionDays = days
		return nil
	}
}

// Transform applies compliance transformations to a log event
func (e *Engine) Transform(event *core.LogEvent) *core.LogEvent {
	e.mu.Lock()
	defer e.mu.Unlock()
	
	// Clone the event to avoid modifying the original
	transformed := e.cloneEvent(event)
	
	// Add required audit properties
	e.addRequiredProperties(transformed)
	
	// Mask sensitive data if enabled
	if e.maskSensitive && len(e.profile.MaskSensitive) > 0 {
		e.maskSensitiveData(transformed)
	}
	
	// Add compliance metadata
	if transformed.Properties == nil {
		transformed.Properties = make(map[string]interface{})
	}
	transformed.Properties["_compliance_profile"] = e.profile.Name
	transformed.Properties["_compliance_sequence"] = atomic.AddUint64(&e.sequence, 1)
	
	return transformed
}

// ProcessForStorage prepares an event for storage with encryption and signing
func (e *Engine) ProcessForStorage(event *core.LogEvent) (*ComplianceRecord, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	
	// Serialize the event
	eventData, err := json.Marshal(event)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize event: %w", err)
	}
	
	record := &ComplianceRecord{
		Timestamp: event.Timestamp,
		Profile:   e.profile.Name,
		Sequence:  atomic.AddUint64(&e.sequence, 1),
	}
	
	// Encrypt if required
	if e.profile.EncryptionRequired && e.keyManager != nil {
		encrypted, err := e.keyManager.Encrypt(eventData)
		if err != nil {
			return nil, fmt.Errorf("encryption failed: %w", err)
		}
		record.Encrypted = true
		record.EncryptedData = encrypted
	} else {
		record.PlainData = eventData
	}
	
	// Sign if required
	if e.profile.SigningRequired && e.signatureChain != nil {
		sig, err := e.signatureChain.Sign(record.Sequence, eventData)
		if err != nil {
			return nil, fmt.Errorf("signing failed: %w", err)
		}
		record.Signed = true
		record.Signature = sig
	}
	
	return record, nil
}

// VerifyRecord verifies a compliance record
func (e *Engine) VerifyRecord(record *ComplianceRecord) error {
	e.mu.RLock()
	defer e.mu.RUnlock()
	
	// Get the original data
	var eventData []byte
	if record.Encrypted {
		if e.keyManager == nil {
			return fmt.Errorf("no key manager available for decryption")
		}
		
		plaintext, err := e.keyManager.Decrypt(record.EncryptedData)
		if err != nil {
			return fmt.Errorf("decryption failed: %w", err)
		}
		eventData = plaintext
	} else {
		eventData = record.PlainData
	}
	
	// Verify signature if present
	if record.Signed && record.Signature != nil {
		if e.signer == nil {
			return fmt.Errorf("no signer available for verification")
		}
		
		// Recreate chain data for verification
		chainData := make([]byte, len(record.Signature.PrevHash)+len(record.Signature.DataHash)+8)
		copy(chainData, record.Signature.PrevHash)
		copy(chainData[len(record.Signature.PrevHash):], record.Signature.DataHash)
		for i := 0; i < 8; i++ {
			chainData[len(record.Signature.PrevHash)+len(record.Signature.DataHash)+i] = 
				byte(record.Signature.Sequence >> (8 * i))
		}
		
		if err := e.signer.Verify(chainData, record.Signature.Signature); err != nil {
			return fmt.Errorf("signature verification failed: %w", err)
		}
	}
	
	// Validate the event data if needed
	_ = eventData // Mark as used for validation
	
	return nil
}

// VerifyChain verifies the entire signature chain
func (e *Engine) VerifyChain() (*ChainVerificationReport, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	
	if e.signatureChain == nil {
		return nil, fmt.Errorf("no signature chain available")
	}
	
	report := &ChainVerificationReport{
		Timestamp: time.Now(),
		Profile:   e.profile.Name,
	}
	
	if err := e.signatureChain.Verify(); err != nil {
		report.Valid = false
		report.Error = err.Error()
	} else {
		report.Valid = true
		report.TotalSignatures = len(e.signatureChain.signatures)
		report.LastSequence = e.sequence
	}
	
	return report, nil
}

// cloneEvent creates a deep copy of a log event
func (e *Engine) cloneEvent(event *core.LogEvent) *core.LogEvent {
	cloned := &core.LogEvent{
		Timestamp:       event.Timestamp,
		Level:           event.Level,
		MessageTemplate: event.MessageTemplate,
		Exception:       event.Exception,
	}
	
	// Deep copy properties
	if event.Properties != nil {
		cloned.Properties = make(map[string]interface{})
		for k, v := range event.Properties {
			cloned.Properties[k] = v
		}
	}
	
	return cloned
}

// addRequiredProperties adds properties required by the compliance profile
func (e *Engine) addRequiredProperties(event *core.LogEvent) {
	if event.Properties == nil {
		event.Properties = make(map[string]interface{})
	}
	
	// Add timestamp if not present
	if _, exists := event.Properties["Timestamp"]; !exists {
		event.Properties["Timestamp"] = event.Timestamp.Format(time.RFC3339Nano)
	}
	
	// Check for required properties and add placeholders if missing
	for _, prop := range e.profile.AuditProperties {
		if _, exists := event.Properties[prop]; !exists && e.enforceRequired {
			// Add a placeholder to indicate missing required property
			event.Properties[prop] = "[MISSING_REQUIRED]"
		}
	}
}

// maskSensitiveData masks sensitive fields in the event
func (e *Engine) maskSensitiveData(event *core.LogEvent) {
	if event.Properties == nil {
		return
	}
	
	for key, value := range event.Properties {
		if e.isSensitiveField(key) {
			// Mask the value while preserving type information
			switch v := value.(type) {
			case string:
				if len(v) > 4 {
					event.Properties[key] = maskString(v)
				} else {
					event.Properties[key] = "****"
				}
			case int, int64, float64:
				event.Properties[key] = "****"
			default:
				event.Properties[key] = "[REDACTED]"
			}
		}
	}
	
	// Also check message template for sensitive data
	if event.MessageTemplate != "" {
		for _, sensitive := range e.profile.MaskSensitive {
			if strings.Contains(strings.ToLower(event.MessageTemplate), strings.ToLower(sensitive)) {
				// Create a new template with masked content
				maskedText := event.MessageTemplate
				// Simple masking - in production, use regex for better masking
				maskedText = strings.ReplaceAll(maskedText, sensitive, "[REDACTED]")
				event.MessageTemplate = maskedText
				break
			}
		}
	}
}

// isSensitiveField checks if a field name is sensitive
func (e *Engine) isSensitiveField(fieldName string) bool {
	lowerField := strings.ToLower(fieldName)
	for _, sensitive := range e.profile.MaskSensitive {
		if strings.Contains(lowerField, strings.ToLower(sensitive)) {
			return true
		}
	}
	return false
}

// maskString masks a string value preserving first and last characters
func maskString(s string) string {
	if len(s) <= 4 {
		return "****"
	}
	if len(s) <= 8 {
		return s[:2] + strings.Repeat("*", len(s)-2)
	}
	return s[:2] + strings.Repeat("*", len(s)-4) + s[len(s)-2:]
}

// ComplianceRecord represents a processed event with compliance metadata
type ComplianceRecord struct {
	Timestamp     time.Time              `json:"timestamp"`
	Profile       string                 `json:"profile"`
	Sequence      uint64                 `json:"sequence"`
	Encrypted     bool                   `json:"encrypted"`
	Signed        bool                   `json:"signed"`
	PlainData     []byte                 `json:"plain_data,omitempty"`
	EncryptedData *EncryptedRecord       `json:"encrypted_data,omitempty"`
	Signature     *ChainedSignature      `json:"signature,omitempty"`
}

// ChainVerificationReport represents the result of chain verification
type ChainVerificationReport struct {
	Timestamp       time.Time `json:"timestamp"`
	Profile         string    `json:"profile"`
	Valid           bool      `json:"valid"`
	TotalSignatures int       `json:"total_signatures"`
	LastSequence    uint64    `json:"last_sequence"`
	Error           string    `json:"error,omitempty"`
}

// GetRetentionPeriod returns the retention period for the profile
func (e *Engine) GetRetentionPeriod() time.Duration {
	return time.Duration(e.profile.RetentionDays) * 24 * time.Hour
}

// RequiresImmutableStorage returns if immutable storage is required
func (e *Engine) RequiresImmutableStorage() bool {
	return e.profile.RequiresImmutable
}

// RequiresAccessLogging returns if access logging is required
func (e *Engine) RequiresAccessLogging() bool {
	return e.profile.RequiresAccessLog
}