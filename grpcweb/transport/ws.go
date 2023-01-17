//go:build !js || !wasm
// +build !js !wasm

package transport

import (
	"net/http"
	"nhooyr.io/websocket"
)

func initWsDialer() *websocket.DialOptions {
	h := http.Header{}
	h.Set("Sec-WebSocket-Protocol", "grpc-websockets")
	dialer := &websocket.DialOptions{}
	dialer.HTTPClient = http.DefaultClient
	// Set weebsocket dialer http header
	dialer.HTTPHeader = h
	// Set websocket dialer subprotocol
	dialer.Subprotocols = []string{"grpc-websockets"}
	return dialer
}
