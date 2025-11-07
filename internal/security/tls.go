package security

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/hashicorp/raft"
)

// TLSConfig holds TLS configuration for servers and clients
type TLSConfig struct {
	// Server certificate and key
	CertFile string
	KeyFile  string

	// Client CA certificate for mTLS (mutual TLS)
	ClientCAFile string

	// Enable mutual TLS (client authentication)
	EnableMTLS bool

	// Minimum TLS version (default: TLS 1.2)
	MinVersion uint16
}

// DefaultTLSConfig returns a secure default TLS configuration
func DefaultTLSConfig() *TLSConfig {
	return &TLSConfig{
		MinVersion: tls.VersionTLS12,
		EnableMTLS: false,
	}
}

// LoadServerTLSConfig creates a tls.Config for servers
func LoadServerTLSConfig(config *TLSConfig) (*tls.Config, error) {
	if config == nil {
		return nil, fmt.Errorf("TLS config cannot be nil")
	}

	// Load server certificate and key
	cert, err := tls.LoadX509KeyPair(config.CertFile, config.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load server certificate: %w", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   config.MinVersion,
		CipherSuites: secureCipherSuites(),
	}

	// Enable mutual TLS if requested
	if config.EnableMTLS && config.ClientCAFile != "" {
		clientCAs, err := loadCAPool(config.ClientCAFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load client CA: %w", err)
		}

		tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
		tlsConfig.ClientCAs = clientCAs
	}

	return tlsConfig, nil
}

// LoadRaftTLSConfig creates a tls.Config for Raft inter-node communication
// This config works for both accepting (server) and dialing (client) connections
func LoadRaftTLSConfig(config *TLSConfig) (*tls.Config, error) {
	if config == nil {
		return nil, fmt.Errorf("TLS config cannot be nil")
	}

	// Load the node's certificate and key (for both server and client)
	cert, err := tls.LoadX509KeyPair(config.CertFile, config.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load certificate: %w", err)
	}

	// Load CA certificate pool for verifying peer certificates
	caCert, err := os.ReadFile(config.ClientCAFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA certificate: %w", err)
	}

	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to parse CA certificate")
	}

	tlsConfig := &tls.Config{
		// Server side: present our certificate to peers
		Certificates: []tls.Certificate{cert},

		// Client side: also present our certificate when dialing
		// (this is the same cert, satisfying mutual TLS)

		// Both sides: verify peer certificates using the CA pool
		ClientCAs:  caCertPool, // For verifying client certs when accepting
		RootCAs:    caCertPool, // For verifying server certs when dialing
		ClientAuth: tls.RequireAndVerifyClientCert, // Require mutual TLS

		MinVersion:   config.MinVersion,
		CipherSuites: secureCipherSuites(),

		// InsecureSkipVerify is false (default), we verify using CA pool
	}

	return tlsConfig, nil
}

// LoadClientTLSConfig creates a tls.Config for clients
func LoadClientTLSConfig(config *TLSConfig) (*tls.Config, error) {
	if config == nil {
		return nil, fmt.Errorf("TLS config cannot be nil")
	}

	tlsConfig := &tls.Config{
		MinVersion:   config.MinVersion,
		CipherSuites: secureCipherSuites(),
	}

	// Load server CA for verification
	if config.ClientCAFile != "" {
		serverCAs, err := loadCAPool(config.ClientCAFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load server CA: %w", err)
		}
		tlsConfig.RootCAs = serverCAs
	}

	// Load client certificate for mTLS
	if config.EnableMTLS && config.CertFile != "" && config.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(config.CertFile, config.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	return tlsConfig, nil
}

// loadCAPool loads a CA certificate pool from a PEM file
func loadCAPool(caFile string) (*x509.CertPool, error) {
	caPEM, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA file %s: %w", caFile, err)
	}

	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("failed to parse CA certificate from %s", caFile)
	}

	return caPool, nil
}

// secureCipherSuites returns a list of secure cipher suites
// Excludes weak ciphers and prefers ECDHE for forward secrecy
func secureCipherSuites() []uint16 {
	return []uint16{
		// TLS 1.3 cipher suites (preferred)
		tls.TLS_AES_128_GCM_SHA256,
		tls.TLS_AES_256_GCM_SHA384,
		tls.TLS_CHACHA20_POLY1305_SHA256,

		// TLS 1.2 cipher suites with forward secrecy
		tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
		tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
		tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
		tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
	}
}

// ValidateTLSConfig validates TLS configuration
func ValidateTLSConfig(config *TLSConfig) error {
	if config == nil {
		return fmt.Errorf("TLS config cannot be nil")
	}

	// Check if certificate and key files exist
	if config.CertFile == "" {
		return fmt.Errorf("certificate file path cannot be empty")
	}
	if config.KeyFile == "" {
		return fmt.Errorf("key file path cannot be empty")
	}

	if _, err := os.Stat(config.CertFile); os.IsNotExist(err) {
		return fmt.Errorf("certificate file does not exist: %s", config.CertFile)
	}
	if _, err := os.Stat(config.KeyFile); os.IsNotExist(err) {
		return fmt.Errorf("key file does not exist: %s", config.KeyFile)
	}

	// Check mTLS configuration
	if config.EnableMTLS {
		if config.ClientCAFile == "" {
			return fmt.Errorf("client CA file required for mTLS but not provided")
		}
		if _, err := os.Stat(config.ClientCAFile); os.IsNotExist(err) {
			return fmt.Errorf("client CA file does not exist: %s", config.ClientCAFile)
		}
	}

	// Validate TLS version
	if config.MinVersion < tls.VersionTLS12 {
		return fmt.Errorf("minimum TLS version must be 1.2 or higher (got %d)", config.MinVersion)
	}

	return nil
}

// TLSStreamLayer implements the raft.StreamLayer interface with TLS support
// This enables encrypted and authenticated Raft inter-node communication
type TLSStreamLayer struct {
	listener  net.Listener
	tlsConfig *tls.Config
	advertise net.Addr
}

// NewTLSStreamLayer creates a new TLS-enabled stream layer for Raft
func NewTLSStreamLayer(bindAddr string, advertise net.Addr, tlsConfig *tls.Config) (*TLSStreamLayer, error) {
	if tlsConfig == nil {
		return nil, fmt.Errorf("TLS config cannot be nil")
	}

	// Create TLS listener
	listener, err := tls.Listen("tcp", bindAddr, tlsConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create TLS listener: %w", err)
	}

	return &TLSStreamLayer{
		listener:  listener,
		tlsConfig: tlsConfig,
		advertise: advertise,
	}, nil
}

// Accept implements net.Listener interface
func (t *TLSStreamLayer) Accept() (net.Conn, error) {
	return t.listener.Accept()
}

// Close implements net.Listener interface
func (t *TLSStreamLayer) Close() error {
	return t.listener.Close()
}

// Addr implements net.Listener interface
func (t *TLSStreamLayer) Addr() net.Addr {
	if t.advertise != nil {
		return t.advertise
	}
	return t.listener.Addr()
}

// Dial implements raft.StreamLayer interface
func (t *TLSStreamLayer) Dial(address raft.ServerAddress, timeout time.Duration) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: timeout}
	return tls.DialWithDialer(dialer, "tcp", string(address), t.tlsConfig)
}
