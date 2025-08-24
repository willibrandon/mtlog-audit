package compliance

import (
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"io"
	"os"
	"sync"
)

// Signer provides cryptographic signing for audit records
type Signer interface {
	Sign(data []byte) (signature []byte, err error)
	Verify(data, signature []byte) error
	Algorithm() string
}

// Ed25519Signer implements signing using Ed25519
type Ed25519Signer struct {
	privateKey ed25519.PrivateKey
	publicKey  ed25519.PublicKey
}

// NewEd25519Signer creates a new Ed25519 signer
func NewEd25519Signer() (*Ed25519Signer, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate Ed25519 key: %w", err)
	}
	
	return &Ed25519Signer{
		privateKey: priv,
		publicKey:  pub,
	}, nil
}

// LoadEd25519Signer loads an Ed25519 signer from a PEM file
func LoadEd25519Signer(privateKeyPath string) (*Ed25519Signer, error) {
	keyData, err := os.ReadFile(privateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read private key: %w", err)
	}
	
	block, _ := pem.Decode(keyData)
	if block == nil {
		return nil, fmt.Errorf("failed to parse PEM block")
	}
	
	if block.Type != "PRIVATE KEY" {
		return nil, fmt.Errorf("invalid key type: %s", block.Type)
	}
	
	privateKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}
	
	ed25519Key, ok := privateKey.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("not an Ed25519 private key")
	}
	
	return &Ed25519Signer{
		privateKey: ed25519Key,
		publicKey:  ed25519Key.Public().(ed25519.PublicKey),
	}, nil
}

// Sign signs data using Ed25519
func (s *Ed25519Signer) Sign(data []byte) ([]byte, error) {
	return ed25519.Sign(s.privateKey, data), nil
}

// Verify verifies a signature using Ed25519
func (s *Ed25519Signer) Verify(data, signature []byte) error {
	if !ed25519.Verify(s.publicKey, data, signature) {
		return fmt.Errorf("signature verification failed")
	}
	return nil
}

// Algorithm returns the signing algorithm
func (s *Ed25519Signer) Algorithm() string {
	return "Ed25519"
}

// RSASigner implements signing using RSA-PSS
type RSASigner struct {
	privateKey *rsa.PrivateKey
	publicKey  *rsa.PublicKey
}

// NewRSASigner creates a new RSA signer
func NewRSASigner(bits int) (*RSASigner, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		return nil, fmt.Errorf("failed to generate RSA key: %w", err)
	}
	
	return &RSASigner{
		privateKey: privateKey,
		publicKey:  &privateKey.PublicKey,
	}, nil
}

// Sign signs data using RSA-PSS
func (s *RSASigner) Sign(data []byte) ([]byte, error) {
	hash := sha256.Sum256(data)
	signature, err := rsa.SignPSS(rand.Reader, s.privateKey, crypto.SHA256, hash[:], nil)
	if err != nil {
		return nil, fmt.Errorf("failed to sign data: %w", err)
	}
	return signature, nil
}

// Verify verifies a signature using RSA-PSS
func (s *RSASigner) Verify(data, signature []byte) error {
	hash := sha256.Sum256(data)
	err := rsa.VerifyPSS(s.publicKey, crypto.SHA256, hash[:], signature, nil)
	if err != nil {
		return fmt.Errorf("signature verification failed: %w", err)
	}
	return nil
}

// Algorithm returns the signing algorithm
func (s *RSASigner) Algorithm() string {
	return "RSA-PSS"
}

// SignatureChain maintains a chain of signatures for tamper detection
type SignatureChain struct {
	mu         sync.RWMutex
	signer     Signer
	lastHash   []byte
	signatures []ChainedSignature
}

// ChainedSignature represents a signature in the chain
type ChainedSignature struct {
	Sequence  uint64
	DataHash  []byte
	PrevHash  []byte
	Signature []byte
	Algorithm string
}

// NewSignatureChain creates a new signature chain
func NewSignatureChain(signer Signer) *SignatureChain {
	return &SignatureChain{
		signer:     signer,
		lastHash:   make([]byte, 32), // Initialize with zeros
		signatures: make([]ChainedSignature, 0),
	}
}

// Sign adds a new signature to the chain
func (sc *SignatureChain) Sign(sequence uint64, data []byte) (*ChainedSignature, error) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	
	// Calculate data hash
	dataHash := sha256.Sum256(data)
	
	// Create chain data: prevHash + dataHash + sequence
	chainData := make([]byte, len(sc.lastHash)+len(dataHash)+8)
	copy(chainData, sc.lastHash)
	copy(chainData[len(sc.lastHash):], dataHash[:])
	// Add sequence number
	for i := 0; i < 8; i++ {
		chainData[len(sc.lastHash)+len(dataHash)+i] = byte(sequence >> (8 * i))
	}
	
	// Sign the chain data
	signature, err := sc.signer.Sign(chainData)
	if err != nil {
		return nil, fmt.Errorf("failed to sign chain data: %w", err)
	}
	
	// Create chained signature
	chainedSig := ChainedSignature{
		Sequence:  sequence,
		DataHash:  dataHash[:],
		PrevHash:  sc.lastHash,
		Signature: signature,
		Algorithm: sc.signer.Algorithm(),
	}
	
	// Update last hash
	sc.lastHash = dataHash[:]
	sc.signatures = append(sc.signatures, chainedSig)
	
	return &chainedSig, nil
}

// Verify verifies the entire signature chain
func (sc *SignatureChain) Verify() error {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	
	prevHash := make([]byte, 32) // Start with zeros
	
	for i, sig := range sc.signatures {
		// Recreate chain data
		chainData := make([]byte, len(prevHash)+len(sig.DataHash)+8)
		copy(chainData, prevHash)
		copy(chainData[len(prevHash):], sig.DataHash)
		// Add sequence number
		for j := 0; j < 8; j++ {
			chainData[len(prevHash)+len(sig.DataHash)+j] = byte(sig.Sequence >> (8 * j))
		}
		
		// Verify signature
		if err := sc.signer.Verify(chainData, sig.Signature); err != nil {
			return fmt.Errorf("chain verification failed at position %d: %w", i, err)
		}
		
		prevHash = sig.DataHash
	}
	
	return nil
}

// SignatureRecord wraps audit data with signature information
type SignatureRecord struct {
	Data      []byte `json:"data"`
	Signature string `json:"signature"` // Base64 encoded
	Algorithm string `json:"algorithm"`
	Sequence  uint64 `json:"sequence"`
	PrevHash  string `json:"prev_hash"` // Base64 encoded
}

// SignData signs data and returns a signature record
func SignData(signer Signer, sequence uint64, data []byte, prevHash []byte) (*SignatureRecord, error) {
	signature, err := signer.Sign(data)
	if err != nil {
		return nil, err
	}
	
	return &SignatureRecord{
		Data:      data,
		Signature: base64.StdEncoding.EncodeToString(signature),
		Algorithm: signer.Algorithm(),
		Sequence:  sequence,
		PrevHash:  base64.StdEncoding.EncodeToString(prevHash),
	}, nil
}

// VerifySignatureRecord verifies a signature record
func VerifySignatureRecord(signer Signer, record *SignatureRecord) error {
	signature, err := base64.StdEncoding.DecodeString(record.Signature)
	if err != nil {
		return fmt.Errorf("failed to decode signature: %w", err)
	}
	
	return signer.Verify(record.Data, signature)
}

// GenerateKeyPair generates a new key pair for the specified algorithm
func GenerateKeyPair(algorithm string, writer io.Writer) error {
	switch algorithm {
	case "Ed25519":
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return fmt.Errorf("failed to generate Ed25519 key: %w", err)
		}
		
		// Marshal private key
		privKeyBytes, err := x509.MarshalPKCS8PrivateKey(priv)
		if err != nil {
			return fmt.Errorf("failed to marshal private key: %w", err)
		}
		
		// Write private key PEM
		privateBlock := &pem.Block{
			Type:  "PRIVATE KEY",
			Bytes: privKeyBytes,
		}
		if err := pem.Encode(writer, privateBlock); err != nil {
			return fmt.Errorf("failed to encode private key: %w", err)
		}
		
		// Marshal public key
		pubKeyBytes, err := x509.MarshalPKIXPublicKey(pub)
		if err != nil {
			return fmt.Errorf("failed to marshal public key: %w", err)
		}
		
		// Write public key PEM
		publicBlock := &pem.Block{
			Type:  "PUBLIC KEY",
			Bytes: pubKeyBytes,
		}
		if err := pem.Encode(writer, publicBlock); err != nil {
			return fmt.Errorf("failed to encode public key: %w", err)
		}
		
	case "RSA", "RSA-PSS":
		privateKey, err := rsa.GenerateKey(rand.Reader, 4096)
		if err != nil {
			return fmt.Errorf("failed to generate RSA key: %w", err)
		}
		
		// Marshal private key
		privKeyBytes, err := x509.MarshalPKCS8PrivateKey(privateKey)
		if err != nil {
			return fmt.Errorf("failed to marshal private key: %w", err)
		}
		
		// Write private key PEM
		privateBlock := &pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: privKeyBytes,
		}
		if err := pem.Encode(writer, privateBlock); err != nil {
			return fmt.Errorf("failed to encode private key: %w", err)
		}
		
		// Marshal public key
		pubKeyBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
		if err != nil {
			return fmt.Errorf("failed to marshal public key: %w", err)
		}
		
		// Write public key PEM
		publicBlock := &pem.Block{
			Type:  "PUBLIC KEY",
			Bytes: pubKeyBytes,
		}
		if err := pem.Encode(writer, publicBlock); err != nil {
			return fmt.Errorf("failed to encode public key: %w", err)
		}
		
	default:
		return fmt.Errorf("unsupported algorithm: %s", algorithm)
	}
	
	return nil
}