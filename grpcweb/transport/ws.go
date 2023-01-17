//go:build !js || !wasm
// +build !js !wasm

package transport

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"nhooyr.io/websocket"
)

func initWsDialer(opts *ConnectOptions) *websocket.DialOptions {
	h := http.Header{}
	h.Set("Sec-WebSocket-Protocol", "grpc-websockets")
	dialer := &websocket.DialOptions{}
	dialer.HTTPClient = http.DefaultClient
	// Set weebsocket dialer http header
	dialer.HTTPHeader = h
	// Set websocket dialer subprotocol
	dialer.Subprotocols = []string{"grpc-websockets"}

	if opts.WithTLS {
		tlsConf := &tls.Config{}
		if opts.TLSCertificate != nil {
			certPool := x509.NewCertPool()
			decoded, _ := pem.Decode(opts.TLSCertificate)
			if decoded == nil {
				panic("failed to decode cert")
			}
			cert, err := x509.ParseCertificate(decoded.Bytes)
			if err != nil {
				panic(err)
			}
			certPool.AddCert(cert)
			tlsConf.RootCAs = certPool
			tlsConf.ServerName = cert.DNSNames[0]
		}

		tlsConf.InsecureSkipVerify = opts.TlsInsecureSkipVerify

		dialer.HTTPClient.Transport = &http.Transport{
			TLSClientConfig: tlsConf,
		}
	}
	return dialer
}
