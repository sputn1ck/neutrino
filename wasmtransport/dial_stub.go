//go:build !js

package wasmtransport

import (
	"fmt"
	"net"
)

// NewProxyDialer is only available in js/wasm browser builds.
func NewProxyDialer(_ string) func(net.Addr) (net.Conn, error) {
	return func(net.Addr) (net.Conn, error) {
		return nil, fmt.Errorf("websocket proxy dialer requires js/wasm")
	}
}
