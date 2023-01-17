//go:build js && wasm

package transport

import (
	"nhooyr.io/websocket"
)

func initWsDialer(opts *ConnectOptions) *websocket.DialOptions {
	dialer := &websocket.DialOptions{}
	// Set websocket dialer subprotocol - this SHOULD also set
	// the Sec-WebSocket-Protocol header when passed into the javascript api
	dialer.Subprotocols = []string{"grpc-websockets"}
	return dialer
}
