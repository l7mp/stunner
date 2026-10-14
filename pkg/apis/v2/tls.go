package v2

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"strings"
)

// TLSConfig is the TLS or DTLS of a listener, a cluster or a tunnel. Cert and Key are our identity,
// CA is how we verify the peer; SNI and Insecure apply only when we dial, PQCMode only when we
// accept.
type TLSConfig struct {
	// Cert is our base64-encoded PEM certificate: mandatory when we accept, optional when we
	// dial (mutual TLS).
	Cert string `json:"cert,omitempty"`
	// Key is our base64-encoded PEM key.
	Key string `json:"key,omitempty"`
	// CA is the base64-encoded PEM bundle we verify the peer by when we dial. Default is the
	// system roots.
	CA string `json:"ca,omitempty"`
	// SNI is the server name we send and verify the peer certificate against when we dial.
	// Default is the host dialed (an IP is verified but not sent).
	SNI string `json:"sni,omitempty"`
	// Insecure skips the verification of the peer when we dial.
	Insecure bool `json:"insecure,omitempty"`
	// PQCMode is the post-quantum key exchange policy when we accept over TLS: "default",
	// "preferred" or "enforced".
	PQCMode string `json:"pqc_mode,omitempty"`
}

// Validate checks a TLS configuration and normalizes the PQC mode.
func (t *TLSConfig) Validate() error {
	for _, f := range [][2]string{{"cert", t.Cert}, {"key", t.Key}, {"CA", t.CA}} {
		if _, err := base64.StdEncoding.DecodeString(f[1]); err != nil {
			return fmt.Errorf("invalid TLS %s: base64-decode error: %w", f[0], err)
		}
	}
	mode, err := NewPQCMode(t.PQCMode)
	if err != nil {
		return err
	}
	t.PQCMode = ""
	if mode != PQCModeDefault {
		t.PQCMode = mode.String()
	}
	return nil
}

// DeepCopy copies a TLS configuration.
func (t *TLSConfig) DeepCopy() *TLSConfig {
	ret := *t
	return &ret
}

// String stringifies a TLS configuration, hiding the key material.
func (t *TLSConfig) String() string {
	opts := []string{}
	for _, f := range [][2]string{{"cert", t.Cert}, {"key", t.Key}, {"ca", t.CA}} {
		if f[1] != "" {
			opts = append(opts, f[0]+"=<SECRET>")
		}
	}
	if t.SNI != "" {
		opts = append(opts, "sni="+t.SNI)
	}
	if t.Insecure {
		opts = append(opts, "insecure")
	}
	if t.PQCMode != "" {
		opts = append(opts, "pqc="+t.PQCMode)
	}
	return "tls={" + strings.Join(opts, ",") + "}"
}

// DialConfig returns the TLS client configuration of dialing host: the peer is verified by CA
// (the system roots if unset) against SNI (host if unset), and our certificate is presented if
// set. A nil TLSConfig is the defaults.
func (t *TLSConfig) DialConfig(host string) (*tls.Config, error) {
	if t == nil {
		t = &TLSConfig{}
	}
	conf := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: host,
		InsecureSkipVerify: t.Insecure} //nolint:gosec
	if t.SNI != "" {
		conf.ServerName = t.SNI
	}
	if t.CA != "" {
		pem, err := base64.StdEncoding.DecodeString(t.CA)
		if err != nil {
			return nil, fmt.Errorf("invalid TLS CA: base64-decode error: %w", err)
		}
		conf.RootCAs = x509.NewCertPool()
		if !conf.RootCAs.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("invalid TLS CA: no certificate in the bundle")
		}
	}
	if t.Cert != "" || t.Key != "" {
		cert, err := base64.StdEncoding.DecodeString(t.Cert)
		if err != nil {
			return nil, fmt.Errorf("invalid TLS cert: base64-decode error: %w", err)
		}
		key, err := base64.StdEncoding.DecodeString(t.Key)
		if err != nil {
			return nil, fmt.Errorf("invalid TLS key: base64-decode error: %w", err)
		}
		kp, err := tls.X509KeyPair(cert, key)
		if err != nil {
			return nil, fmt.Errorf("invalid TLS cert/key pair: %w", err)
		}
		conf.Certificates = []tls.Certificate{kp}
	}
	return conf, nil
}
