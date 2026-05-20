package wasmtransport

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
