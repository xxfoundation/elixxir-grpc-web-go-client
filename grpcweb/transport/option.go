package transport

import "time"

// ConnectOptions struct contains configuration parameters for DialContext
type ConnectOptions struct {
	// Toggle tls on/off
	WithTLS bool
	// Certificate for TLS connections
	TLSCertificate []byte
	// IdleConnTimeout is the maximum amount of time an idle
	// (keep-alive) connection will remain idle before closing
	// itself.
	// Zero means no limit.
	IdleConnTimeout time.Duration
	// TLSHandshakeTimeout specifies the maximum amount of time waiting to
	// wait for a TLS handshake. Zero means no timeout.
	TlsHandshakeTimeout time.Duration
	// ExpectContinueTimeout, if non-zero, specifies the amount of
	// time to wait for a server's first response headers after fully
	// writing the request headers
	ExpectContinueTimeout time.Duration
	// Skip standard tls certificate verifications
	TlsInsecureSkipVerify bool

	Timeout time.Duration
}
