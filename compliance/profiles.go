package compliance

import (
	"time"
)

// Profile defines compliance requirements for different standards
type Profile struct {
	EncryptionAlgorithm string
	Name                string
	SigningAlgorithm    string
	MaskSensitive       []string
	AuditProperties     []string
	RetentionDays       int
	MinRetentionDays    int
	MaxRetentionDays    int
	SigningRequired     bool
	RequiresTamperProof bool
	RequiresAccessLog   bool
	RequiresImmutable   bool
	EncryptionRequired  bool
}

// Profiles defines all supported compliance profiles
var Profiles = map[string]Profile{
	"HIPAA": {
		Name:                "HIPAA",
		EncryptionRequired:  true,
		EncryptionAlgorithm: "AES-256-GCM",
		SigningRequired:     true,
		SigningAlgorithm:    "Ed25519",
		RetentionDays:       2190, // 6 years default
		MinRetentionDays:    2190, // 6 years minimum
		MaxRetentionDays:    3650, // 10 years maximum
		MaskSensitive: []string{
			"SSN", "ssn", "SocialSecurityNumber",
			"MRN", "mrn", "MedicalRecordNumber",
			"DOB", "dob", "DateOfBirth", "birthdate",
			"AccountNumber", "account_number",
			"DL", "DriversLicense", "drivers_license",
			"Email", "email", "EmailAddress",
			"Phone", "phone", "PhoneNumber",
		},
		RequiresTamperProof: true,
		RequiresAccessLog:   true,
		RequiresImmutable:   true,
		AuditProperties: []string{
			"UserId",
			"PatientId",
			"Action",
			"Timestamp",
			"IPAddress",
		},
	},
	"PCI-DSS": {
		Name:                "PCI-DSS",
		EncryptionRequired:  true,
		EncryptionAlgorithm: "AES-256-GCM",
		SigningRequired:     true,
		SigningAlgorithm:    "RSA-PSS",
		RetentionDays:       365,  // 1 year default
		MinRetentionDays:    365,  // 1 year minimum
		MaxRetentionDays:    2555, // 7 years maximum
		MaskSensitive: []string{
			"PAN", "pan", "CardNumber", "card_number",
			"CVV", "cvv", "SecurityCode", "security_code",
			"PIN", "pin",
			"CardholderName", "cardholder_name",
			"ExpiryDate", "expiry_date",
		},
		RequiresTamperProof: true,
		RequiresAccessLog:   true,
		RequiresImmutable:   false,
		AuditProperties: []string{
			"TransactionId",
			"MerchantId",
			"Amount",
			"Currency",
			"Timestamp",
		},
	},
	"SOX": {
		Name:                "SOX",
		EncryptionRequired:  true,
		EncryptionAlgorithm: "AES-256-GCM",
		SigningRequired:     true,
		SigningAlgorithm:    "RSA-PSS",
		RetentionDays:       2555, // 7 years default
		MinRetentionDays:    2555, // 7 years minimum
		MaxRetentionDays:    3650, // 10 years maximum
		MaskSensitive:       []string{},
		RequiresTamperProof: true,
		RequiresAccessLog:   true,
		RequiresImmutable:   true,
		AuditProperties: []string{
			"UserId",
			"TransactionId",
			"Amount",
			"Account",
			"Timestamp",
			"ApprovalChain",
		},
	},
	"GDPR": {
		Name:                "GDPR",
		EncryptionRequired:  true,
		EncryptionAlgorithm: "AES-256-GCM",
		SigningRequired:     false,
		SigningAlgorithm:    "",
		RetentionDays:       1095, // 3 years default
		MinRetentionDays:    0,    // Can be deleted on request
		MaxRetentionDays:    2190, // 6 years maximum
		MaskSensitive: []string{
			"Email", "email",
			"Name", "name", "FullName",
			"Address", "address",
			"Phone", "phone",
			"IPAddress", "ip_address",
			"DeviceId", "device_id",
			"NationalId", "national_id",
		},
		RequiresTamperProof: false,
		RequiresAccessLog:   true,
		RequiresImmutable:   false, // Must support deletion
		AuditProperties: []string{
			"DataSubjectId",
			"Purpose",
			"LegalBasis",
			"Timestamp",
			"ConsentId",
		},
	},
}

// IsValidProfile checks if a profile name is valid
func IsValidProfile(name string) bool {
	_, exists := Profiles[name]
	return exists
}

// GetProfile returns a profile by name
func GetProfile(name string) (Profile, bool) {
	profile, exists := Profiles[name]
	return profile, exists
}

// RequiresEncryption checks if any configured profile requires encryption
func RequiresEncryption(profiles []string) bool {
	for _, name := range profiles {
		if profile, exists := Profiles[name]; exists && profile.EncryptionRequired {
			return true
		}
	}
	return false
}

// RequiresSigning checks if any configured profile requires signing
func RequiresSigning(profiles []string) bool {
	for _, name := range profiles {
		if profile, exists := Profiles[name]; exists && profile.SigningRequired {
			return true
		}
	}
	return false
}

// GetMaxRetention returns the maximum retention period from all profiles
func GetMaxRetention(profiles []string) time.Duration {
	maxDays := 0
	for _, name := range profiles {
		if profile, exists := Profiles[name]; exists {
			if profile.RetentionDays > maxDays {
				maxDays = profile.RetentionDays
			}
		}
	}
	return time.Duration(maxDays) * 24 * time.Hour
}
