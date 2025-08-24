package compliance

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	"sync"

	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/scrypt"
)

// Encryptor provides encryption for audit records
type Encryptor interface {
	Encrypt(plaintext []byte) (ciphertext []byte, err error)
	Decrypt(ciphertext []byte) (plaintext []byte, err error)
	Algorithm() string
}

// AESGCMEncryptor implements AES-256-GCM encryption
type AESGCMEncryptor struct {
	key    []byte
	cipher cipher.AEAD
	mu     sync.RWMutex
}

// NewAESGCMEncryptor creates a new AES-256-GCM encryptor
func NewAESGCMEncryptor(key []byte) (*AESGCMEncryptor, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("AES-256 requires 32-byte key, got %d bytes", len(key))
	}
	
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher: %w", err)
	}
	
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}
	
	return &AESGCMEncryptor{
		key:    key,
		cipher: gcm,
	}, nil
}

// Encrypt encrypts plaintext using AES-256-GCM
func (e *AESGCMEncryptor) Encrypt(plaintext []byte) ([]byte, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	
	// Generate nonce
	nonce := make([]byte, e.cipher.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}
	
	// Encrypt and append nonce to ciphertext
	ciphertext := e.cipher.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

// Decrypt decrypts ciphertext using AES-256-GCM
func (e *AESGCMEncryptor) Decrypt(ciphertext []byte) ([]byte, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	
	nonceSize := e.cipher.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	
	// Extract nonce
	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	
	// Decrypt
	plaintext, err := e.cipher.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decryption failed: %w", err)
	}
	
	return plaintext, nil
}

// Algorithm returns the encryption algorithm
func (e *AESGCMEncryptor) Algorithm() string {
	return "AES-256-GCM"
}

// ChaCha20Poly1305Encryptor implements ChaCha20-Poly1305 encryption
type ChaCha20Poly1305Encryptor struct {
	key    []byte
	cipher cipher.AEAD
	mu     sync.RWMutex
}

// NewChaCha20Poly1305Encryptor creates a new ChaCha20-Poly1305 encryptor
func NewChaCha20Poly1305Encryptor(key []byte) (*ChaCha20Poly1305Encryptor, error) {
	if len(key) != chacha20poly1305.KeySize {
		return nil, fmt.Errorf("ChaCha20-Poly1305 requires %d-byte key, got %d bytes", 
			chacha20poly1305.KeySize, len(key))
	}
	
	cipher, err := chacha20poly1305.New(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create ChaCha20-Poly1305 cipher: %w", err)
	}
	
	return &ChaCha20Poly1305Encryptor{
		key:    key,
		cipher: cipher,
	}, nil
}

// Encrypt encrypts plaintext using ChaCha20-Poly1305
func (e *ChaCha20Poly1305Encryptor) Encrypt(plaintext []byte) ([]byte, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	
	// Generate nonce
	nonce := make([]byte, e.cipher.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}
	
	// Encrypt and append nonce to ciphertext
	ciphertext := e.cipher.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

// Decrypt decrypts ciphertext using ChaCha20-Poly1305
func (e *ChaCha20Poly1305Encryptor) Decrypt(ciphertext []byte) ([]byte, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	
	nonceSize := e.cipher.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	
	// Extract nonce
	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	
	// Decrypt
	plaintext, err := e.cipher.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decryption failed: %w", err)
	}
	
	return plaintext, nil
}

// Algorithm returns the encryption algorithm
func (e *ChaCha20Poly1305Encryptor) Algorithm() string {
	return "ChaCha20-Poly1305"
}

// KeyDerivation derives an encryption key from a password
func DeriveKey(password []byte, salt []byte, keyLen int) ([]byte, error) {
	if len(salt) < 16 {
		return nil, fmt.Errorf("salt must be at least 16 bytes")
	}
	
	// Use scrypt for key derivation (NIST approved)
	// N=32768 (2^15), r=8, p=1 are recommended parameters
	key, err := scrypt.Key(password, salt, 32768, 8, 1, keyLen)
	if err != nil {
		return nil, fmt.Errorf("key derivation failed: %w", err)
	}
	
	return key, nil
}

// GenerateSalt generates a random salt for key derivation
func GenerateSalt() ([]byte, error) {
	salt := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, fmt.Errorf("failed to generate salt: %w", err)
	}
	return salt, nil
}

// GenerateKey generates a random encryption key
func GenerateKey(bits int) ([]byte, error) {
	if bits%8 != 0 {
		return nil, fmt.Errorf("key size must be multiple of 8 bits")
	}
	
	key := make([]byte, bits/8)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("failed to generate key: %w", err)
	}
	return key, nil
}

// EncryptedRecord wraps audit data with encryption metadata
type EncryptedRecord struct {
	Algorithm   string `json:"algorithm"`
	Ciphertext  []byte `json:"ciphertext"`
	KeyID       string `json:"key_id,omitempty"`
	WrappedKey  []byte `json:"wrapped_key,omitempty"` // For key wrapping
	Salt        []byte `json:"salt,omitempty"`        // For key derivation
}

// KeyManager manages encryption keys with rotation support
type KeyManager struct {
	mu          sync.RWMutex
	currentKey  []byte
	currentID   string
	keys        map[string][]byte
	encryptor   Encryptor
	rotateAfter int64 // Number of encryptions before rotation
	counter     int64
}

// NewKeyManager creates a new key manager
func NewKeyManager(initialKey []byte, algorithm string) (*KeyManager, error) {
	var encryptor Encryptor
	var err error
	
	switch algorithm {
	case "AES-256-GCM":
		encryptor, err = NewAESGCMEncryptor(initialKey)
	case "ChaCha20-Poly1305":
		encryptor, err = NewChaCha20Poly1305Encryptor(initialKey)
	default:
		return nil, fmt.Errorf("unsupported algorithm: %s", algorithm)
	}
	
	if err != nil {
		return nil, err
	}
	
	keyID := generateKeyID(initialKey)
	
	return &KeyManager{
		currentKey:  initialKey,
		currentID:   keyID,
		keys:        map[string][]byte{keyID: initialKey},
		encryptor:   encryptor,
		rotateAfter: 1000000, // Rotate after 1M encryptions
	}, nil
}

// Encrypt encrypts data with the current key
func (km *KeyManager) Encrypt(plaintext []byte) (*EncryptedRecord, error) {
	km.mu.Lock()
	defer km.mu.Unlock()
	
	// Check if rotation is needed
	km.counter++
	if km.counter >= km.rotateAfter {
		if err := km.rotateKey(); err != nil {
			return nil, fmt.Errorf("key rotation failed: %w", err)
		}
	}
	
	ciphertext, err := km.encryptor.Encrypt(plaintext)
	if err != nil {
		return nil, err
	}
	
	return &EncryptedRecord{
		Algorithm:  km.encryptor.Algorithm(),
		Ciphertext: ciphertext,
		KeyID:      km.currentID,
	}, nil
}

// Decrypt decrypts data using the appropriate key
func (km *KeyManager) Decrypt(record *EncryptedRecord) ([]byte, error) {
	km.mu.RLock()
	defer km.mu.RUnlock()
	
	// Get the appropriate key
	key, exists := km.keys[record.KeyID]
	if !exists {
		return nil, fmt.Errorf("key not found: %s", record.KeyID)
	}
	
	// Create encryptor for this key if different from current
	var encryptor Encryptor
	if record.KeyID == km.currentID {
		encryptor = km.encryptor
	} else {
		var err error
		switch record.Algorithm {
		case "AES-256-GCM":
			encryptor, err = NewAESGCMEncryptor(key)
		case "ChaCha20-Poly1305":
			encryptor, err = NewChaCha20Poly1305Encryptor(key)
		default:
			return nil, fmt.Errorf("unsupported algorithm: %s", record.Algorithm)
		}
		if err != nil {
			return nil, err
		}
	}
	
	return encryptor.Decrypt(record.Ciphertext)
}

// rotateKey generates a new key and updates the current key
func (km *KeyManager) rotateKey() error {
	newKey, err := GenerateKey(256)
	if err != nil {
		return err
	}
	
	var newEncryptor Encryptor
	switch km.encryptor.Algorithm() {
	case "AES-256-GCM":
		newEncryptor, err = NewAESGCMEncryptor(newKey)
	case "ChaCha20-Poly1305":
		newEncryptor, err = NewChaCha20Poly1305Encryptor(newKey)
	default:
		return fmt.Errorf("unsupported algorithm: %s", km.encryptor.Algorithm())
	}
	
	if err != nil {
		return err
	}
	
	keyID := generateKeyID(newKey)
	km.currentKey = newKey
	km.currentID = keyID
	km.keys[keyID] = newKey
	km.encryptor = newEncryptor
	km.counter = 0
	
	return nil
}

// generateKeyID generates a unique ID for a key
func generateKeyID(key []byte) string {
	hash := sha256.Sum256(key)
	return fmt.Sprintf("%x", hash[:8]) // Use first 8 bytes of hash
}