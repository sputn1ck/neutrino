package wasmtransport

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"
)

const (
	// DefaultDoHEndpoint is a public DNS-over-HTTPS JSON endpoint that works
	// from browser fetch/http clients.
	DefaultDoHEndpoint = "https://cloudflare-dns.com/dns-query"

	defaultDoHTimeout = 15 * time.Second
)

// DoHResolver resolves host names through a DNS-over-HTTPS JSON endpoint.
// It is intended for browser wasm builds, where raw DNS is unavailable, but is
// kept portable so it can be unit-tested on normal Go runtimes.
type DoHResolver struct {
	Endpoint string
	Client   *http.Client
}

// NewDoHNameResolver returns a neutrino-compatible name resolver backed by a
// DNS-over-HTTPS JSON endpoint. Empty endpoint uses DefaultDoHEndpoint.
func NewDoHNameResolver(endpoint string) func(string) ([]net.IP, error) {
	resolver := &DoHResolver{
		Endpoint: endpoint,
		Client: &http.Client{
			Timeout: defaultDoHTimeout,
		},
	}

	return resolver.LookupIP
}

// LookupIP resolves host through A and AAAA DoH queries.
func (r *DoHResolver) LookupIP(host string) ([]net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		return []net.IP{ip}, nil
	}

	var result []net.IP
	for _, recordType := range []string{"A", "AAAA"} {
		ips, err := r.lookup(context.Background(), host, recordType)
		if err != nil {
			return nil, err
		}

		result = append(result, ips...)
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("no DNS answers for %s", host)
	}

	return result, nil
}

func (r *DoHResolver) lookup(ctx context.Context, host,
	recordType string) ([]net.IP, error) {

	endpoint := r.Endpoint
	if endpoint == "" {
		endpoint = DefaultDoHEndpoint
	}
	client := r.Client
	if client == nil {
		client = &http.Client{Timeout: defaultDoHTimeout}
	}

	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}

	q := u.Query()
	q.Set("name", host)
	q.Set("type", recordType)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("accept", "application/dns-json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("dns lookup %s %s failed: %s",
			host, recordType, resp.Status)
	}

	var dnsResp struct {
		Answer []struct {
			Type int    `json:"type"`
			Data string `json:"data"`
		} `json:"Answer"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&dnsResp); err != nil {
		return nil, err
	}

	wantType := 1
	if recordType == "AAAA" {
		wantType = 28
	}

	var ips []net.IP
	for _, answer := range dnsResp.Answer {
		if answer.Type != wantType {
			continue
		}

		ip := net.ParseIP(answer.Data)
		if ip == nil {
			continue
		}

		ips = append(ips, ip)
	}

	return ips, nil
}
