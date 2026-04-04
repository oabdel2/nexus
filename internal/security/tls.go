package security

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

// TLSConfig holds TLS/mTLS configuration.
type TLSConfig struct {
	Enabled        bool   `yaml:"enabled"`
	CertFile       string `yaml:"cert_file"`
	KeyFile        string `yaml:"key_file"`
	CAFile         string `yaml:"ca_file"`
	MinVersion     string `yaml:"min_version"`
	MutualTLS      bool   `yaml:"mutual_tls"`
	AutoCert       bool   `yaml:"auto_cert"`
	AutoCertDomain string `yaml:"auto_cert_domain"`
}

// BuildTLSConfig creates a *tls.Config from our configuration.
func BuildTLSConfig(cfg TLSConfig) (*tls.Config, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	tlsCfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		},
	}

	// Set minimum TLS version
	switch cfg.MinVersion {
	case "1.3":
		tlsCfg.MinVersion = tls.VersionTLS13
	default:
		tlsCfg.MinVersion = tls.VersionTLS12
	}

	// Load server certificate
	if cfg.CertFile != "" && cfg.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load TLS cert/key: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}

	// Mutual TLS: require and verify client certificates
	if cfg.MutualTLS && cfg.CAFile != "" {
		caCert, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA file: %w", err)
		}
		caPool := x509.NewCertPool()
		if !caPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA certificate")
		}
		tlsCfg.ClientCAs = caPool
		tlsCfg.ClientAuth = tls.RequireAndVerifyClientCert
	}

	return tlsCfg, nil
}
