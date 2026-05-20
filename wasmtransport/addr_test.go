package wasmtransport

import (
	"testing"

	"github.com/btcsuite/btcd/chaincfg"
)

func TestWithDefaultPort(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		addr string
		want string
	}{
		{
			name: "hostname with port",
			addr: "example.com:8333",
			want: "example.com:8333",
		},
		{
			name: "hostname without port",
			addr: "example.com",
			want: "example.com:8333",
		},
		{
			name: "ipv6 without port",
			addr: "2001:db8::1",
			want: "[2001:db8::1]:8333",
		},
		{
			name: "bracketed ipv6 without port",
			addr: "[2001:db8::1]",
			want: "[2001:db8::1]:8333",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := WithDefaultPort(test.addr, "8333")
			if got != test.want {
				t.Fatalf("expected %q, got %q", test.want, got)
			}
		})
	}
}

func TestNewAddrResolver(t *testing.T) {
	t.Parallel()

	resolver := NewAddrResolver(&chaincfg.SigNetParams)
	addr, err := resolver("seed.signet.example")
	if err != nil {
		t.Fatalf("resolve clearnet addr: %v", err)
	}
	if addr.String() != "seed.signet.example:38333" {
		t.Fatalf("unexpected addr: %s", addr)
	}

	_, err = resolver("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.onion:8333")
	if err == nil {
		t.Fatalf("expected onion address rejection")
	}
}
