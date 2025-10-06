// Package compliance provides tests for compliance engine functionality.
package compliance

import (
	"testing"
	"time"

	"github.com/willibrandon/mtlog/core"
)

func TestComplianceProfiles(t *testing.T) {
	profiles := []string{"HIPAA", "PCI-DSS", "SOX", "GDPR"}

	for _, profileName := range profiles {
		t.Run(profileName, func(t *testing.T) {
			profile, ok := GetProfile(profileName)
			if !ok {
				t.Fatalf("Profile %s not found", profileName)
			}

			if profile.Name != profileName {
				t.Errorf("Profile name mismatch: got %s, want %s", profile.Name, profileName)
			}

			// Verify retention days are within bounds
			if profile.RetentionDays < profile.MinRetentionDays {
				t.Errorf("RetentionDays %d < MinRetentionDays %d",
					profile.RetentionDays, profile.MinRetentionDays)
			}
			if profile.RetentionDays > profile.MaxRetentionDays {
				t.Errorf("RetentionDays %d > MaxRetentionDays %d",
					profile.RetentionDays, profile.MaxRetentionDays)
			}
		})
	}
}

func TestComplianceEngine(t *testing.T) {
	engine, err := New("HIPAA")
	if err != nil {
		t.Fatalf("Failed to create compliance engine: %v", err)
	}

	// Create test event with sensitive data
	event := &core.LogEvent{
		Timestamp:       time.Now(),
		Level:           core.InformationLevel,
		MessageTemplate: "Patient record accessed",
		Properties: map[string]interface{}{
			"SSN":       "123-45-6789",
			"PatientId": "P-001",
			"UserId":    "D-001",
			"Action":    "VIEW",
		},
	}

	// Transform the event
	transformed := engine.Transform(event)

	// Verify SSN was masked
	if ssn, ok := transformed.Properties["SSN"].(string); ok {
		if ssn == "123-45-6789" {
			t.Error("SSN was not masked")
		}
		if ssn != "****" && ssn != "***-**-6789" {
			t.Logf("SSN was masked to: %s", ssn)
		}
	}

	// Verify required properties were added
	if _, ok := transformed.Properties["_compliance_profile"]; !ok {
		t.Error("Compliance profile not added to event")
	}
	if _, ok := transformed.Properties["_compliance_sequence"]; !ok {
		t.Error("Compliance sequence not added to event")
	}
}

func TestEncryption(t *testing.T) {
	key := []byte("test-key-32-bytes-long-exactly!!")

	engine, err := New(
		"HIPAA",
		WithEncryptionKey(key),
	)
	if err != nil {
		t.Fatalf("Failed to create engine with encryption: %v", err)
	}

	event := &core.LogEvent{
		Timestamp:       time.Now(),
		Level:           core.InformationLevel,
		MessageTemplate: "Test event",
		Properties: map[string]interface{}{
			"data": "sensitive",
		},
	}

	// Process for storage
	record, err := engine.ProcessForStorage(event)
	if err != nil {
		t.Fatalf("Failed to process event for storage: %v", err)
	}

	if record.EncryptedData == nil {
		t.Error("Encrypted data is nil")
	}

	if record.EncryptedData != nil && len(record.EncryptedData.Ciphertext) == 0 {
		t.Error("Event was not encrypted")
	}

	// Verify record has required fields
	if record.Profile != "HIPAA" {
		t.Errorf("Profile not set correctly: %s", record.Profile)
	}
	if record.Sequence == 0 {
		t.Error("Sequence not set")
	}
}

func TestRetentionDaysOption(t *testing.T) {
	// Test valid retention days
	engine, err := New(
		"HIPAA",
		WithRetentionDays(2555), // Valid for HIPAA
	)
	if err != nil {
		t.Fatalf("Failed to create engine with valid retention: %v", err)
	}

	retention := engine.GetRetentionPeriod()
	expectedDays := 2555
	expected := time.Duration(expectedDays) * 24 * time.Hour
	if retention != expected {
		t.Errorf("Retention period mismatch: got %v, want %v", retention, expected)
	}

	// Test retention below minimum
	_, err = New(
		"HIPAA",
		WithRetentionDays(100), // Below HIPAA minimum
	)
	if err == nil {
		t.Error("Expected error for retention below minimum")
	}

	// Test retention above maximum
	_, err = New(
		"HIPAA",
		WithRetentionDays(5000), // Above HIPAA maximum
	)
	if err == nil {
		t.Error("Expected error for retention above maximum")
	}
}

func TestMaskingDisabled(t *testing.T) {
	engine, err := New(
		"GDPR",
		WithMaskingDisabled(),
	)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	event := &core.LogEvent{
		Timestamp:       time.Now(),
		Level:           core.InformationLevel,
		MessageTemplate: "User data",
		Properties: map[string]interface{}{
			"Email": "test@example.com",
			"Name":  "John Doe",
		},
	}

	transformed := engine.Transform(event)

	// With masking disabled, email should remain unchanged
	if email, ok := transformed.Properties["Email"].(string); ok {
		if email != "test@example.com" {
			t.Errorf("Email was masked when masking was disabled: %s", email)
		}
	}
}

func TestComplianceValidation(t *testing.T) {
	// Test valid profiles
	validProfiles := []string{"HIPAA", "PCI-DSS", "SOX", "GDPR"}
	for _, profile := range validProfiles {
		if !IsValidProfile(profile) {
			t.Errorf("Profile %s should be valid", profile)
		}
	}

	// Test invalid profile
	if IsValidProfile("INVALID") {
		t.Error("INVALID should not be a valid profile")
	}
}

func TestRequiresEncryption(t *testing.T) {
	tests := []struct {
		profiles []string
		want     bool
	}{
		{[]string{"HIPAA"}, true},
		{[]string{"PCI-DSS"}, true},
		{[]string{"SOX"}, true},
		{[]string{"GDPR"}, true},
		{[]string{}, false},
		{[]string{"INVALID"}, false},
		{[]string{"HIPAA", "GDPR"}, true},
	}

	for _, tt := range tests {
		got := RequiresEncryption(tt.profiles)
		if got != tt.want {
			t.Errorf("RequiresEncryption(%v) = %v, want %v", tt.profiles, got, tt.want)
		}
	}
}

func TestRequiresSigning(t *testing.T) {
	tests := []struct {
		profiles []string
		want     bool
	}{
		{[]string{"HIPAA"}, true},
		{[]string{"PCI-DSS"}, true},
		{[]string{"SOX"}, true},
		{[]string{"GDPR"}, false},
		{[]string{}, false},
		{[]string{"GDPR"}, false},
		{[]string{"HIPAA", "GDPR"}, true},
	}

	for _, tt := range tests {
		got := RequiresSigning(tt.profiles)
		if got != tt.want {
			t.Errorf("RequiresSigning(%v) = %v, want %v", tt.profiles, got, tt.want)
		}
	}
}

func TestGetMaxRetention(t *testing.T) {
	tests := []struct {
		profiles []string
		wantDays int
	}{
		{[]string{"HIPAA"}, 2190},
		{[]string{"PCI-DSS"}, 365},
		{[]string{"SOX"}, 2555},
		{[]string{"GDPR"}, 1095},
		{[]string{"HIPAA", "SOX"}, 2555}, // Should return max
		{[]string{}, 0},
	}

	for _, tt := range tests {
		got := GetMaxRetention(tt.profiles)
		want := time.Duration(tt.wantDays) * 24 * time.Hour
		if got != want {
			t.Errorf("GetMaxRetention(%v) = %v, want %v", tt.profiles, got, want)
		}
	}
}
