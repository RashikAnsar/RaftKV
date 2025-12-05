package backup

import (
	"bytes"
	"crypto/rand"
	"io"
	"testing"
)

func TestAESEncryptor_Encrypt(t *testing.T) {
	// Generate a 32-byte key
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	encryptor, err := NewEncryptor(EncryptionTypeAES256GCM, key)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}

	data := []byte("This is sensitive data that needs to be encrypted for security.")

	reader := bytes.NewReader(data)
	encryptedReader, err := encryptor.Encrypt(reader)
	if err != nil {
		t.Fatalf("encryption failed: %v", err)
	}

	encrypted, err := io.ReadAll(encryptedReader)
	if err != nil {
		t.Fatalf("failed to read encrypted data: %v", err)
	}

	// Encrypted data should not equal original
	if bytes.Equal(encrypted, data) {
		t.Fatalf("encrypted data equals original data")
	}

	// Encrypted data should be longer (nonce + ciphertext + tag)
	if len(encrypted) <= len(data) {
		t.Fatalf("encrypted data should be longer than original")
	}
}

func TestAESEncryptor_Decrypt(t *testing.T) {
	// Generate a 32-byte key
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	encryptor, err := NewEncryptor(EncryptionTypeAES256GCM, key)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}

	originalData := []byte("This is sensitive data that needs encryption and decryption round-trip.")

	// Encrypt
	reader := bytes.NewReader(originalData)
	encryptedReader, err := encryptor.Encrypt(reader)
	if err != nil {
		t.Fatalf("encryption failed: %v", err)
	}

	encrypted, err := io.ReadAll(encryptedReader)
	if err != nil {
		t.Fatalf("failed to read encrypted data: %v", err)
	}

	// Decrypt
	decryptedReader, err := encryptor.Decrypt(bytes.NewReader(encrypted))
	if err != nil {
		t.Fatalf("decryption failed: %v", err)
	}

	decrypted, err := io.ReadAll(decryptedReader)
	if err != nil {
		t.Fatalf("failed to read decrypted data: %v", err)
	}

	// Verify round-trip
	if !bytes.Equal(decrypted, originalData) {
		t.Fatalf("decrypted data doesn't match original.\nOriginal: %s\nDecrypted: %s",
			originalData, decrypted)
	}
}

func TestAESEncryptor_WrongKey(t *testing.T) {
	// Generate two different keys
	key1 := make([]byte, 32)
	key2 := make([]byte, 32)
	if _, err := rand.Read(key1); err != nil {
		t.Fatalf("failed to generate key1: %v", err)
	}
	if _, err := rand.Read(key2); err != nil {
		t.Fatalf("failed to generate key2: %v", err)
	}

	encryptor1, err := NewEncryptor(EncryptionTypeAES256GCM, key1)
	if err != nil {
		t.Fatalf("failed to create encryptor1: %v", err)
	}

	encryptor2, err := NewEncryptor(EncryptionTypeAES256GCM, key2)
	if err != nil {
		t.Fatalf("failed to create encryptor2: %v", err)
	}

	data := []byte("Secret data")

	// Encrypt with key1
	encryptedReader, err := encryptor1.Encrypt(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("encryption failed: %v", err)
	}

	encrypted, err := io.ReadAll(encryptedReader)
	if err != nil {
		t.Fatalf("failed to read encrypted data: %v", err)
	}

	// Try to decrypt with key2 (should fail)
	decryptedReader, err := encryptor2.Decrypt(bytes.NewReader(encrypted))
	if err != nil {
		// This is expected - decryption should fail with wrong key
		return
	}

	// If Decrypt didn't error, reading should fail
	_, err = io.ReadAll(decryptedReader)
	if err == nil {
		t.Fatalf("decryption with wrong key should have failed")
	}
}

func TestAESEncryptor_CorruptedData(t *testing.T) {
	// Generate a 32-byte key
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	encryptor, err := NewEncryptor(EncryptionTypeAES256GCM, key)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}

	data := []byte("This data will be corrupted after encryption")

	// Encrypt
	encryptedReader, err := encryptor.Encrypt(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("encryption failed: %v", err)
	}

	encrypted, err := io.ReadAll(encryptedReader)
	if err != nil {
		t.Fatalf("failed to read encrypted data: %v", err)
	}

	// Corrupt the data
	if len(encrypted) > 20 {
		encrypted[20] ^= 0xFF // Flip bits
	}

	// Try to decrypt corrupted data (should fail)
	decryptedReader, err := encryptor.Decrypt(bytes.NewReader(encrypted))
	if err != nil {
		// This is expected - decryption should fail
		return
	}

	// If Decrypt didn't error, reading should fail due to authentication tag mismatch
	_, err = io.ReadAll(decryptedReader)
	if err == nil {
		t.Fatalf("decryption of corrupted data should have failed")
	}
}

func TestNoEncryption(t *testing.T) {
	encryptor, err := NewEncryptor(EncryptionTypeNone, nil)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}

	data := []byte("data without encryption")

	// Encrypt (should be no-op)
	encryptedReader, err := encryptor.Encrypt(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("encryption failed: %v", err)
	}

	encrypted, err := io.ReadAll(encryptedReader)
	if err != nil {
		t.Fatalf("failed to read encrypted data: %v", err)
	}

	// Should be identical
	if !bytes.Equal(encrypted, data) {
		t.Fatalf("no-encryption output doesn't match input")
	}

	// Decrypt (should be no-op)
	decryptedReader, err := encryptor.Decrypt(bytes.NewReader(encrypted))
	if err != nil {
		t.Fatalf("decryption failed: %v", err)
	}

	decrypted, err := io.ReadAll(decryptedReader)
	if err != nil {
		t.Fatalf("failed to read decrypted data: %v", err)
	}

	if !bytes.Equal(decrypted, data) {
		t.Fatalf("no-decryption output doesn't match input")
	}
}

func TestAESEncryptor_LargeData(t *testing.T) {
	// Generate a 32-byte key
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	encryptor, err := NewEncryptor(EncryptionTypeAES256GCM, key)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}

	// Create 10MB of random data
	data := make([]byte, 10*1024*1024)
	if _, err := rand.Read(data); err != nil {
		t.Fatalf("failed to generate test data: %v", err)
	}

	// Encrypt
	encryptedReader, err := encryptor.Encrypt(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("encryption failed: %v", err)
	}

	encrypted, err := io.ReadAll(encryptedReader)
	if err != nil {
		t.Fatalf("failed to read encrypted data: %v", err)
	}

	t.Logf("Large data: original=%d, encrypted=%d", len(data), len(encrypted))

	// Decrypt
	decryptedReader, err := encryptor.Decrypt(bytes.NewReader(encrypted))
	if err != nil {
		t.Fatalf("decryption failed: %v", err)
	}

	decrypted, err := io.ReadAll(decryptedReader)
	if err != nil {
		t.Fatalf("failed to read decrypted data: %v", err)
	}

	// Verify
	if !bytes.Equal(decrypted, data) {
		t.Fatalf("large data round-trip failed")
	}
}

func TestAESEncryptor_EmptyData(t *testing.T) {
	// Generate a 32-byte key
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	encryptor, err := NewEncryptor(EncryptionTypeAES256GCM, key)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}

	data := []byte{}

	// Encrypt
	encryptedReader, err := encryptor.Encrypt(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("encryption failed: %v", err)
	}

	encrypted, err := io.ReadAll(encryptedReader)
	if err != nil {
		t.Fatalf("failed to read encrypted data: %v", err)
	}

	// Decrypt
	decryptedReader, err := encryptor.Decrypt(bytes.NewReader(encrypted))
	if err != nil {
		t.Fatalf("decryption failed: %v", err)
	}

	decrypted, err := io.ReadAll(decryptedReader)
	if err != nil {
		t.Fatalf("failed to read decrypted data: %v", err)
	}

	// Verify
	if !bytes.Equal(decrypted, data) {
		t.Fatalf("empty data round-trip failed")
	}
}

func TestInvalidKeySize(t *testing.T) {
	// Try with wrong key size
	key := make([]byte, 16) // Should be 32

	_, err := NewEncryptor(EncryptionTypeAES256GCM, key)
	if err == nil {
		t.Fatalf("expected error for invalid key size")
	}
}

func TestInvalidEncryptionType(t *testing.T) {
	key := make([]byte, 32)

	_, err := NewEncryptor(EncryptionType("invalid"), key)
	if err == nil {
		t.Fatalf("expected error for invalid encryption type")
	}
}

func TestMultipleEncryptions_DifferentNonces(t *testing.T) {
	// Generate a 32-byte key
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	encryptor, err := NewEncryptor(EncryptionTypeAES256GCM, key)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}

	data := []byte("Same data encrypted twice")

	// Encrypt twice
	encrypted1Reader, err := encryptor.Encrypt(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("first encryption failed: %v", err)
	}
	encrypted1, err := io.ReadAll(encrypted1Reader)
	if err != nil {
		t.Fatalf("failed to read first encrypted data: %v", err)
	}

	encrypted2Reader, err := encryptor.Encrypt(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("second encryption failed: %v", err)
	}
	encrypted2, err := io.ReadAll(encrypted2Reader)
	if err != nil {
		t.Fatalf("failed to read second encrypted data: %v", err)
	}

	// Should produce different ciphertext due to different nonces
	if bytes.Equal(encrypted1, encrypted2) {
		t.Fatalf("encrypting same data twice produced identical ciphertext (nonce reuse?)")
	}

	// But both should decrypt to the same plaintext
	decrypted1Reader, err := encryptor.Decrypt(bytes.NewReader(encrypted1))
	if err != nil {
		t.Fatalf("first decryption failed: %v", err)
	}
	decrypted1, err := io.ReadAll(decrypted1Reader)
	if err != nil {
		t.Fatalf("failed to read first decrypted data: %v", err)
	}

	decrypted2Reader, err := encryptor.Decrypt(bytes.NewReader(encrypted2))
	if err != nil {
		t.Fatalf("second decryption failed: %v", err)
	}
	decrypted2, err := io.ReadAll(decrypted2Reader)
	if err != nil {
		t.Fatalf("failed to read second decrypted data: %v", err)
	}

	if !bytes.Equal(decrypted1, data) || !bytes.Equal(decrypted2, data) {
		t.Fatalf("decrypted data doesn't match original")
	}
}
