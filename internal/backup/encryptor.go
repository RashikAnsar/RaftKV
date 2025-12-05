package backup

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"
)

// EncryptionType represents the type of encryption
type EncryptionType string

const (
	EncryptionTypeNone      EncryptionType = "none"
	EncryptionTypeAES256GCM EncryptionType = "aes-256-gcm"
)

// Encryptor provides encryption functionality
type Encryptor interface {
	// Encrypt wraps a reader with encryption
	Encrypt(r io.Reader) (io.ReadCloser, error)

	// Decrypt wraps a reader with decryption
	Decrypt(r io.Reader) (io.ReadCloser, error)

	// Type returns the encryption type
	Type() EncryptionType
}

// NoEncryption provides a no-op encryptor
type NoEncryption struct{}

func (e *NoEncryption) Encrypt(r io.Reader) (io.ReadCloser, error) {
	return io.NopCloser(r), nil
}

func (e *NoEncryption) Decrypt(r io.Reader) (io.ReadCloser, error) {
	return io.NopCloser(r), nil
}

func (e *NoEncryption) Type() EncryptionType {
	return EncryptionTypeNone
}

// AES256GCMEncryptor provides AES-256-GCM encryption
type AES256GCMEncryptor struct {
	key []byte // 32 bytes for AES-256
}

// NewAES256GCMEncryptor creates a new AES-256-GCM encryptor
func NewAES256GCMEncryptor(key []byte) (*AES256GCMEncryptor, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("key must be 32 bytes for AES-256, got %d", len(key))
	}
	return &AES256GCMEncryptor{key: key}, nil
}

func (e *AES256GCMEncryptor) Encrypt(r io.Reader) (io.ReadCloser, error) {
	// Create AES cipher
	block, err := aes.NewCipher(e.key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	// Create GCM mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	pr, pw := io.Pipe()

	go func() {
		// Generate nonce
		nonce := make([]byte, gcm.NonceSize())
		if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
			pw.CloseWithError(fmt.Errorf("failed to generate nonce: %w", err))
			return
		}

		// Write nonce first
		if _, err := pw.Write(nonce); err != nil {
			pw.CloseWithError(fmt.Errorf("failed to write nonce: %w", err))
			return
		}

		// Read all data (for GCM, we need the full plaintext)
		plaintext, err := io.ReadAll(r)
		if err != nil {
			pw.CloseWithError(fmt.Errorf("failed to read plaintext: %w", err))
			return
		}

		// Encrypt
		ciphertext := gcm.Seal(nil, nonce, plaintext, nil)

		// Write encrypted data
		if _, err := pw.Write(ciphertext); err != nil {
			pw.CloseWithError(fmt.Errorf("failed to write ciphertext: %w", err))
			return
		}

		pw.Close()
	}()

	return pr, nil
}

func (e *AES256GCMEncryptor) Decrypt(r io.Reader) (io.ReadCloser, error) {
	// Create AES cipher
	block, err := aes.NewCipher(e.key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	// Create GCM mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	pr, pw := io.Pipe()

	go func() {
		// Read nonce
		nonce := make([]byte, gcm.NonceSize())
		if _, err := io.ReadFull(r, nonce); err != nil {
			pw.CloseWithError(fmt.Errorf("failed to read nonce: %w", err))
			return
		}

		// Read all encrypted data
		ciphertext, err := io.ReadAll(r)
		if err != nil {
			pw.CloseWithError(fmt.Errorf("failed to read ciphertext: %w", err))
			return
		}

		// Decrypt
		plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
		if err != nil {
			pw.CloseWithError(fmt.Errorf("decryption failed: %w", err))
			return
		}

		// Write decrypted data
		if _, err := pw.Write(plaintext); err != nil {
			pw.CloseWithError(fmt.Errorf("failed to write plaintext: %w", err))
			return
		}

		pw.Close()
	}()

	return pr, nil
}

func (e *AES256GCMEncryptor) Type() EncryptionType {
	return EncryptionTypeAES256GCM
}

// NewEncryptor creates an encryptor based on the type
func NewEncryptor(eType EncryptionType, key []byte) (Encryptor, error) {
	switch eType {
	case EncryptionTypeNone:
		return &NoEncryption{}, nil
	case EncryptionTypeAES256GCM:
		return NewAES256GCMEncryptor(key)
	default:
		return nil, fmt.Errorf("unsupported encryption type: %s", eType)
	}
}
