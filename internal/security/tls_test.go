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
	// Create temporary directory and dummy cert/key files for tests that need them
	tmpDir := t.TempDir()
	tmpCert := filepath.Join(tmpDir, "cert.pem")
	tmpKey := filepath.Join(tmpDir, "key.pem")

	// Create dummy files
	require.NoError(t, os.WriteFile(tmpCert, []byte("dummy cert"), 0600))
	require.NoError(t, os.WriteFile(tmpKey, []byte("dummy key"), 0600))

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
				KeyFile:  tmpKey,
			},
			wantErr: true,
			errMsg:  "certificate file does not exist",
		},
		{
			name: "mTLS without CA file",
			config: &TLSConfig{
				CertFile:   tmpCert,
				KeyFile:    tmpKey,
				EnableMTLS: true,
			},
			wantErr: true,
			errMsg:  "client CA file required for mTLS",
		},
		{
			name: "TLS version too low",
			config: &TLSConfig{
				CertFile:   tmpCert,
				KeyFile:    tmpKey,
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
// This test uses the actual test certificates from the certs directory
func TestLoadServerTLSConfig_WithValidCerts(t *testing.T) {
	// Use actual test certificates from certs directory
	certFile := filepath.Join("..", "..", "certs", "server-cert.pem")
	keyFile := filepath.Join("..", "..", "certs", "server-key.pem")

	// Skip test if certificates don't exist
	if _, err := os.Stat(certFile); os.IsNotExist(err) {
		t.Skip("Test certificates not found, skipping test")
	}

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
	// Use actual test certificates from certs directory
	certFile := filepath.Join("..", "..", "certs", "server-cert.pem")
	keyFile := filepath.Join("..", "..", "certs", "server-key.pem")
	caFile := filepath.Join("..", "..", "certs", "ca-cert.pem")

	// Skip test if certificates don't exist
	if _, err := os.Stat(certFile); os.IsNotExist(err) {
		t.Skip("Test certificates not found, skipping test")
	}

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
