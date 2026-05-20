package wasmtransport

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestDoHResolverLookupIP(t *testing.T) {
	t.Parallel()

	var queries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		if accept := r.Header.Get("accept"); accept != "application/dns-json" {
			t.Fatalf("expected application/dns-json accept header, got %q", accept)
		}

		var answers []map[string]any
		switch r.URL.Query().Get("type") {
		case "A":
			answers = append(answers, map[string]any{
				"type": 1,
				"data": "203.0.113.7",
			})

		case "AAAA":
			answers = append(answers, map[string]any{
				"type": 28,
				"data": "2001:db8::7",
			})
		}

		if err := json.NewEncoder(w).Encode(map[string]any{
			"Answer": answers,
		}); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer server.Close()

	resolver := &DoHResolver{
		Endpoint: server.URL,
		Client:   server.Client(),
	}
	ips, err := resolver.LookupIP("x49.seed.signet.bitcoin.sprovoost.nl")
	if err != nil {
		t.Fatalf("lookup ip: %v", err)
	}

	want := []net.IP{
		net.ParseIP("203.0.113.7"),
		net.ParseIP("2001:db8::7"),
	}
	if len(ips) != len(want) {
		t.Fatalf("expected %d ips, got %d", len(want), len(ips))
	}
	for i := range want {
		if !ips[i].Equal(want[i]) {
			t.Fatalf("ip %d: expected %s, got %s", i, want[i], ips[i])
		}
	}

	if len(queries) != 2 {
		t.Fatalf("expected A and AAAA queries, got %d", len(queries))
	}
}

func TestDoHResolverLiteralIP(t *testing.T) {
	t.Parallel()

	resolver := &DoHResolver{}
	ips, err := resolver.LookupIP("198.51.100.9")
	if err != nil {
		t.Fatalf("lookup literal ip: %v", err)
	}
	if len(ips) != 1 || !ips[0].Equal(net.ParseIP("198.51.100.9")) {
		t.Fatalf("unexpected literal result: %v", ips)
	}
}

func TestDoHResolverLiveSigNetSeed(t *testing.T) {
	if os.Getenv("WASMTRANSPORT_LIVE_DOH") != "1" {
		t.Skip("set WASMTRANSPORT_LIVE_DOH=1 to query public DNS seeds")
	}

	resolver := NewDoHNameResolver(DefaultDoHEndpoint)
	ips, err := resolver("x49.seed.signet.bitcoin.sprovoost.nl")
	if err != nil {
		t.Fatalf("lookup live signet seed: %v", err)
	}
	if len(ips) == 0 {
		t.Fatalf("expected at least one live signet seed address")
	}
}
