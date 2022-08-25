package transport

import "time"

type ConnectOptions struct {
	WithTLS               bool
	TLSCertificate        []byte
	IdleConnTimeout       time.Duration
	TlsHandshakeTimeout   time.Duration
	ExpectContinueTimeout time.Duration
	TlsInsecureSkipVerify bool
}
