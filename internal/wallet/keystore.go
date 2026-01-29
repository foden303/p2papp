package wallet

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/libp2p/go-libp2p/core/crypto"
	"golang.org/x/crypto/pbkdf2"
)

const (
	saltSize   = 32
	keySize    = 32
	iterations = 100000
)

// EncryptedKeystore provides encrypted key storage
type EncryptedKeystore struct {
	path string
}

// EncryptedData represents the encrypted key file format
type EncryptedData struct {
	Salt       []byte `json:"salt"`
	Nonce      []byte `json:"nonce"`
	Ciphertext []byte `json:"ciphertext"`
}

// NewEncryptedKeystore creates a new encrypted keystore
func NewEncryptedKeystore(path string) *EncryptedKeystore {
	return &EncryptedKeystore{path: path}
}

// SaveKey saves a private key encrypted with a passphrase
func (k *EncryptedKeystore) SaveKey(privKey crypto.PrivKey, passphrase string) error {
	// Marshal the private key
	keyBytes, err := crypto.MarshalPrivateKey(privKey)
	if err != nil {
		return fmt.Errorf("failed to marshal private key: %w", err)
	}

	// Generate salt
	salt := make([]byte, saltSize)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return fmt.Errorf("failed to generate salt: %w", err)
	}

	// Derive encryption key from passphrase
	key := pbkdf2.Key([]byte(passphrase), salt, iterations, keySize, sha256.New)

	// Create AES-GCM cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("failed to create GCM: %w", err)
	}

	// Generate nonce
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Encrypt
	ciphertext := gcm.Seal(nil, nonce, keyBytes, nil)

	// Create encrypted data structure
	encData := EncryptedData{
		Salt:       salt,
		Nonce:      nonce,
		Ciphertext: ciphertext,
	}

	// Marshal to JSON
	data, err := json.MarshalIndent(encData, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal encrypted data: %w", err)
	}

	// Ensure directory exists
	dir := filepath.Dir(k.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Write file
	if err := os.WriteFile(k.path, data, 0600); err != nil {
		return fmt.Errorf("failed to write key file: %w", err)
	}

	return nil
}

// LoadKey loads and decrypts a private key using a passphrase
func (k *EncryptedKeystore) LoadKey(passphrase string) (crypto.PrivKey, error) {
	// Read file
	data, err := os.ReadFile(k.path)
	if err != nil {
		return nil, fmt.Errorf("failed to read key file: %w", err)
	}

	// Unmarshal JSON
	var encData EncryptedData
	if err := json.Unmarshal(data, &encData); err != nil {
		return nil, fmt.Errorf("failed to parse key file: %w", err)
	}

	// Derive key from passphrase
	key := pbkdf2.Key([]byte(passphrase), encData.Salt, iterations, keySize, sha256.New)

	// Create AES-GCM cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// Decrypt
	keyBytes, err := gcm.Open(nil, encData.Nonce, encData.Ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt key (wrong passphrase?): %w", err)
	}

	// Unmarshal private key
	privKey, err := crypto.UnmarshalPrivateKey(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal private key: %w", err)
	}

	return privKey, nil
}

// Exists checks if a key file exists
func (k *EncryptedKeystore) Exists() bool {
	_, err := os.Stat(k.path)
	return err == nil
}

// Delete removes the key file
func (k *EncryptedKeystore) Delete() error {
	return os.Remove(k.path)
}
