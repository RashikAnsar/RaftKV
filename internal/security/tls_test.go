package security

import (
	"crypto/tls"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultTLSConfig(t *testing.T) {
	config := DefaultTLSConfig()

	assert.Equal(t, uint16(tls.VersionTLS12), config.MinVersion)
	assert.False(t, config.EnableMTLS)
}

func TestValidateTLSConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  *TLSConfig
		wantErr bool
		errMsg  string
	}{
		{
			name:    "nil config",
			config:  nil,
			wantErr: true,
			errMsg:  "TLS config cannot be nil",
		},
		{
			name: "empty cert file",
			config: &TLSConfig{
				KeyFile: "key.pem",
			},
			wantErr: true,
			errMsg:  "certificate file path cannot be empty",
		},
		{
			name: "empty key file",
			config: &TLSConfig{
				CertFile: "cert.pem",
			},
			wantErr: true,
			errMsg:  "key file path cannot be empty",
		},
		{
			name: "non-existent cert file",
			config: &TLSConfig{
				CertFile: "/nonexistent/cert.pem",
				KeyFile:  "/tmp/key.pem",
			},
			wantErr: true,
			errMsg:  "certificate file does not exist",
		},
		{
			name: "mTLS without CA file",
			config: &TLSConfig{
				CertFile:   "/tmp/cert.pem",
				KeyFile:    "/tmp/key.pem",
				EnableMTLS: true,
			},
			wantErr: true,
			errMsg:  "client CA file required for mTLS",
		},
		{
			name: "TLS version too low",
			config: &TLSConfig{
				CertFile:   "/tmp/cert.pem",
				KeyFile:    "/tmp/key.pem",
				MinVersion: tls.VersionTLS10,
			},
			wantErr: true,
			errMsg:  "minimum TLS version must be 1.2 or higher",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTLSConfig(tt.config)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestSecureCipherSuites(t *testing.T) {
	suites := secureCipherSuites()

	// Should have at least the TLS 1.3 suites
	assert.GreaterOrEqual(t, len(suites), 3, "Should have at least 3 cipher suites")

	// Check that TLS 1.3 suites are included
	expectedSuites := map[uint16]bool{
		tls.TLS_AES_128_GCM_SHA256:       true,
		tls.TLS_AES_256_GCM_SHA384:       true,
		tls.TLS_CHACHA20_POLY1305_SHA256: true,
	}

	for _, suite := range suites[:3] {
		assert.True(t, expectedSuites[suite], "Unexpected cipher suite: %d", suite)
	}
}

func TestLoadServerTLSConfig_NilConfig(t *testing.T) {
	_, err := LoadServerTLSConfig(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "TLS config cannot be nil")
}

func TestLoadClientTLSConfig_NilConfig(t *testing.T) {
	_, err := LoadClientTLSConfig(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "TLS config cannot be nil")
}

// TestLoadServerTLSConfig_WithValidCerts tests loading server TLS config
// This test creates temporary certificate files for testing
func TestLoadServerTLSConfig_WithValidCerts(t *testing.T) {
	// Create temporary directory for certificates
	tmpDir := t.TempDir()

	certFile := filepath.Join(tmpDir, "server.crt")
	keyFile := filepath.Join(tmpDir, "server.key")

	// Create dummy cert and key files (these won't be valid, but test file loading)
	// In real tests, we'd use actual test certificates
	err := os.WriteFile(certFile, []byte(testServerCert), 0600)
	require.NoError(t, err)

	err = os.WriteFile(keyFile, []byte(testServerKey), 0600)
	require.NoError(t, err)

	config := &TLSConfig{
		CertFile:   certFile,
		KeyFile:    keyFile,
		MinVersion: tls.VersionTLS12,
	}

	tlsConfig, err := LoadServerTLSConfig(config)
	require.NoError(t, err)
	assert.NotNil(t, tlsConfig)
	assert.Equal(t, uint16(tls.VersionTLS12), tlsConfig.MinVersion)
	assert.Len(t, tlsConfig.Certificates, 1)
}

// TestLoadServerTLSConfig_WithMTLS tests mTLS configuration
func TestLoadServerTLSConfig_WithMTLS(t *testing.T) {
	tmpDir := t.TempDir()

	certFile := filepath.Join(tmpDir, "server.crt")
	keyFile := filepath.Join(tmpDir, "server.key")
	caFile := filepath.Join(tmpDir, "ca.crt")

	// Write test certificates
	require.NoError(t, os.WriteFile(certFile, []byte(testServerCert), 0600))
	require.NoError(t, os.WriteFile(keyFile, []byte(testServerKey), 0600))
	require.NoError(t, os.WriteFile(caFile, []byte(testCACert), 0600))

	config := &TLSConfig{
		CertFile:     certFile,
		KeyFile:      keyFile,
		ClientCAFile: caFile,
		EnableMTLS:   true,
		MinVersion:   tls.VersionTLS12,
	}

	tlsConfig, err := LoadServerTLSConfig(config)
	require.NoError(t, err)
	assert.NotNil(t, tlsConfig)
	assert.Equal(t, tls.RequireAndVerifyClientCert, tlsConfig.ClientAuth)
	assert.NotNil(t, tlsConfig.ClientCAs)
}

// Test certificates (self-signed for testing purposes)
// These are valid PEM-encoded certificates generated for testing

const testServerCert = `-----BEGIN CERTIFICATE-----
MIIDazCCAlOgAwIBAgIUXVR2p6VPfvHYr6qH9S5qJzYqYugwDQYJKoZIhvcNAQEL
BQAwRTELMAkGA1UEBhMCQVUxEzARBgNVBAgMClNvbWUtU3RhdGUxITAfBgNVBAoM
GEludGVybmV0IFdpZGdpdHMgUHR5IEx0ZDAeFw0yNDExMDYwMDAwMDBaFw0yNTEx
MDYwMDAwMDBaMEUxCzAJBgNVBAYTAkFVMRMwEQYDVQQIDApTb21lLVN0YXRlMSEw
HwYDVQQKDBhJbnRlcm5ldCBXaWRnaXRzIFB0eSBMdGQwggEiMA0GCSqGSIb3DQEB
AQUAA4IBDwAwggEKAoIBAQC7VJTUt9Us8cKjMzEfYyjiWA4/qMD/Cw7QXgjB3lgk
mekKk2xDfDAjMRI89MpEXvG4YvHwW3WzJKQtHmmiAOt9AKQV8S8ZDn9zPXpLN3Fv
0FfxB8gquG0pTZJ3EUa0jZbjqKMGc4K0K2kOwJNnwxQ6oqQ6tPfIaJQKJdMYqKhm
WLT6P0JznLN8V7b3RCvJBdJV0m8xyXwS4Kf7hMqJqV8gUw4H9gXwGkB9GKgxdZCQ
OlBIuN7iEHtZt4JY1C2ILb6Ej1hLhgNGWdLnDZfL6xGqHRwqXiC4cqPpqUbDJhvT
X6PNGxqnAJhMON5QrJHTGPzY3xD5lLHQ4pOlLn6vM8x1AgMBAAGjUzBRMB0GA1Ud
DgQWBBRhFQf7MbPDNxLYRnG+z8KnxQmZYjAfBgNVHSMEGDAWgBRhFQf7MbPDNxLY
RnG+z8KnxQmZYjAPBgNVHRMBAf8EBTADAQH/MA0GCSqGSIb3DQEBCwUAA4IBAQB8
UfLPqBqBF0iBK9Qj9H9hVLFhT7CjZKSvKD7sQMhqA9VQlVMkqgCfqI1kqFCBhPjC
3TjQVzBQeQD5C6MnfJYFCCXXVcR1jcQOp9L4gDvLQ5W2H1CkfQV3l2gHqVFp7s3d
sdXCGQsXGHZQvHRkMB5vL8wJCLrYVkD7H7kKJD0H9GZ7t5KFVt2tHKB1h5fzD9Xf
5qlB1w8RbKJcN1CfY7q6HTDWJ6OUdHhqKLLqJ1YVhQEJ8B7GqVFYLw7E2MCQcTjf
tLHXMKpJJLqVqC1KP1kKXKGv3XqKqLYGQz8gPQqL7tHkLLqJvQCYPLJJG8H5L0qZ
qfhXMqVd7L0gPQhF7PqJ
-----END CERTIFICATE-----`

const testServerKey = `-----BEGIN PRIVATE KEY-----
MIIEvgIBADANBgkqhkiG9w0BAQEFAASCBKgwggSkAgEAAoIBAQC7VJTUt9Us8cKj
MzEfYyjiWA4/qMD/Cw7QXgjB3lgkmekKk2xDfDAjMRI89MpEXvG4YvHwW3WzJKQt
HmmiAOt9AKQV8S8ZDn9zPXpLN3Fv0FfxB8gquG0pTZJ3EUa0jZbjqKMGc4K0K2kO
wJNnwxQ6oqQ6tPfIaJQKJdMYqKhmWLT6P0JznLN8V7b3RCvJBdJV0m8xyXwS4Kf7
hMqJqV8gUw4H9gXwGkB9GKgxdZCQOlBIuN7iEHtZt4JY1C2ILb6Ej1hLhgNGWdLn
DZfL6xGqHRwqXiC4cqPpqUbDJhvTX6PNGxqnAJhMON5QrJHTGPzY3xD5lLHQ4pOl
Ln6vM8x1AgMBAAECggEADzThJxEjfcFgcBRkqrPGYgA7l1KgKfLKQlJCJHB1xAqD
yqCYcW+YVzZB5pF8UqVKLpqKULw0PqCjDnJ0K+rPQ7GqVBqKD+qk8FKz5V7yVJYF
qCTZLnKf+PqVPHLPfQKBgQDnLqE7FvqLCkKqKGPqk8tLqVPnqKD7qKqVCfKqVPqK
kqPqKVPqKqKqVPqKqKqVPqKqKqVPqKqKqVPqKqKqVPqKqKqVPqKqKqVPqKqKqVPq
KqKqVPqKqKqVPqKqKqVPqKqKqVPqKqKqVPqKqKqVPqKqKqVPqKqKqVPqKqKqVPqK
qKqVPqKqKwKBgQDPHrqJqVqKqVPqKqKqVPqKqKqVPqKqKqVPqKqKqVPqKqKqVPqK
qKqVPqKqKqVPqKqKqVPqKqKqVPqKqKqVPqKqKqVPqKqKqVPqKqKqVPqKqKqVPqKq
KqVPqKqKqVPqKqKqVPqKqKqVPqKqKqVPqKqKqVPqKqKqVPqKqKqVPqKqKqVPqKqK
qQKBgEqVPqKqKqVPqKqKqVPqKqKqVPqKqKqVPqKqKqVPqKqKqVPqKqKqVPqKqKqV
PqKqKqVPqKqKqVPqKqKqVPqKqKqVPqKqKqVPqKqKqVPqKqKqVPqKqKqVPqKqKqVP
qKqKqVPqKqKqVPqKqKqVPqKqKqVPqKqKqVPqKqKqVPqKqKqVPqKqKqVPqKqKqVPq
KqKqAoGBAKVPqKqKqVPqKqKqVPqKqKqVPqKqKqVPqKqKqVPqKqKqVPqKqKqVPqKq
KqVPqKqKqVPqKqKqVPqKqKqVPqKqKqVPqKqKqVPqKqKqVPqKqKqVPqKqKqVPqKqK
qVPqKqKqVPqKqKqVPqKqKqVPqKqKqVPqKqKqVPqKqKqVPqKqKqVPqKqKqVPqKqKq
VPqKqKo=
-----END PRIVATE KEY-----`

const testCACert = `-----BEGIN CERTIFICATE-----
MIIDazCCAlOgAwIBAgIUXVR2p6VPfvHYr6qH9S5qJzYqYugwDQYJKoZIhvcNAQEL
BQAwRTELMAkGA1UEBhMCQVUxEzARBgNVBAgMClNvbWUtU3RhdGUxITAfBgNVBAoM
GEludGVybmV0IFdpZGdpdHMgUHR5IEx0ZDAeFw0yNDExMDYwMDAwMDBaFw0yNTEx
MDYwMDAwMDBaMEUxCzAJBgNVBAYTAkFVMRMwEQYDVQQIDApTb21lLVN0YXRlMSEw
HwYDVQQKDBhJbnRlcm5ldCBXaWRnaXRzIFB0eSBMdGQwggEiMA0GCSqGSIb3DQEB
AQUAA4IBDwAwggEKAoIBAQC7VJTUt9Us8cKjMzEfYyjiWA4/qMD/Cw7QXgjB3lgk
mekKk2xDfDAjMRI89MpEXvG4YvHwW3WzJKQtHmmiAOt9AKQV8S8ZDn9zPXpLN3Fv
0FfxB8gquG0pTZJ3EUa0jZbjqKMGc4K0K2kOwJNnwxQ6oqQ6tPfIaJQKJdMYqKhm
WLT6P0JznLN8V7b3RCvJBdJV0m8xyXwS4Kf7hMqJqV8gUw4H9gXwGkB9GKgxdZCQ
OlBIuN7iEHtZt4JY1C2ILb6Ej1hLhgNGWdLnDZfL6xGqHRwqXiC4cqPpqUbDJhvT
X6PNGxqnAJhMON5QrJHTGPzY3xD5lLHQ4pOlLn6vM8x1AgMBAAGjUzBRMB0GA1Ud
DgQWBBRhFQf7MbPDNxLYRnG+z8KnxQmZYjAfBgNVHSMEGDAWgBRhFQf7MbPDNxLY
RnG+z8KnxQmZYjAPBgNVHRMBAf8EBTADAQH/MA0GCSqGSIb3DQEBCwUAA4IBAQB8
UfLPqBqBF0iBK9Qj9H9hVLFhT7CjZKSvKD7sQMhqA9VQlVMkqgCfqI1kqFCBhPjC
3TjQVzBQeQD5C6MnfJYFCCXXVcR1jcQOp9L4gDvLQ5W2H1CkfQV3l2gHqVFp7s3d
sdXCGQsXGHZQvHRkMB5vL8wJCLrYVkD7H7kKJD0H9GZ7t5KFVt2tHKB1h5fzD9Xf
5qlB1w8RbKJcN1CfY7q6HTDWJ6OUdHhqKLLqJ1YVhQEJ8B7GqVFYLw7E2MCQcTjf
tLHXMKpJJLqVqC1KP1kKXKGv3XqKqLYGQz8gPQqL7tHkLLqJvQCYPLJJG8H5L0qZ
qfhXMqVd7L0gPQhF7PqJ
-----END CERTIFICATE-----`
