package wasmtransport

import (
	"fmt"
	"net"
	"strings"

	"github.com/btcsuite/btcd/chaincfg"
)

// Addr preserves the original peer target string for proxy dialers.
type Addr struct {
	target string
}

// NewAddr returns an address that preserves target exactly as supplied.
func NewAddr(target string) *Addr {
	return &Addr{target: target}
}

// Network returns the logical network name.
func (a *Addr) Network() string {
	return "tcp"
}

// String returns the peer target host:port.
func (a *Addr) String() string {
	return a.target
}

// NewAddrResolver returns a resolver suitable for browser proxy transports. It
// preserves the user-supplied host name instead of resolving it locally, adds
// the chain's default port when omitted, and rejects onion addresses because
// browsers cannot dial them through a normal clearnet WebSocket TCP proxy.
func NewAddrResolver(params *chaincfg.Params) func(string) (net.Addr, error) {
	defaultPort := ""
	if params != nil {
		defaultPort = params.DefaultPort
	}

	return func(addr string) (net.Addr, error) {
		if IsOnionTarget(addr) {
			return nil, fmt.Errorf("onion peer %s is not reachable from browser wasm", addr)
		}

		return NewAddr(WithDefaultPort(addr, defaultPort)), nil
	}
}

// WithDefaultPort returns addr with defaultPort appended when addr has no port.
func WithDefaultPort(addr, defaultPort string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" || defaultPort == "" {
		return addr
	}

	if _, _, err := net.SplitHostPort(addr); err == nil {
		return addr
	}

	host := strings.TrimPrefix(strings.TrimSuffix(addr, "]"), "[")
	return net.JoinHostPort(host, defaultPort)
}

// IsOnionTarget reports whether addr names an onion host.
func IsOnionTarget(addr string) bool {
	host := strings.TrimSpace(addr)
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
	} else if strings.HasPrefix(host, "[") {
		if end := strings.Index(host, "]"); end >= 0 {
			host = host[1:end]
		}
	} else if before, _, ok := strings.Cut(host, ":"); ok {
		host = before
	}

	return strings.HasSuffix(strings.ToLower(strings.Trim(host, "[]")), ".onion")
}
