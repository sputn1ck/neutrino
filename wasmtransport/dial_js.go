//go:build js && wasm

package wasmtransport

import (
	"fmt"
	"io"
	"net"
	"net/url"
	"sync"
	"syscall/js"
	"time"
)

// NewProxyDialer returns a dialer that tunnels a Bitcoin P2P TCP stream over a
// browser WebSocket proxy. The proxy URL receives target=<host:port>.
func NewProxyDialer(proxyURL string) func(net.Addr) (net.Conn, error) {
	return func(addr net.Addr) (net.Conn, error) {
		return dial(proxyURL, addr.String())
	}
}

type conn struct {
	ws js.Value

	local  net.Addr
	remote net.Addr

	mu     sync.Mutex
	buf    []byte
	readCh chan []byte

	closeOnce   sync.Once
	releaseOnce sync.Once
	closed      chan struct{}
	err         error

	callbacks []js.Func
}

func dial(proxyURL, target string) (net.Conn, error) {
	wsURL, err := withTarget(proxyURL, target)
	if err != nil {
		return nil, err
	}

	wsCtor := js.Global().Get("WebSocket")
	if wsCtor.IsUndefined() {
		return nil, fmt.Errorf("browser WebSocket API unavailable")
	}

	c := &conn{
		ws:     wsCtor.New(wsURL),
		local:  NewAddr("wasm"),
		remote: NewAddr(target),
		readCh: make(chan []byte, 32),
		closed: make(chan struct{}),
	}
	c.ws.Set("binaryType", "arraybuffer")

	opened := make(chan error, 1)
	onOpen := js.FuncOf(func(js.Value, []js.Value) any {
		opened <- nil
		return nil
	})
	onError := js.FuncOf(func(js.Value, []js.Value) any {
		err := fmt.Errorf("websocket proxy connection failed")
		c.setError(err)
		opened <- err
		return nil
	})
	onClose := js.FuncOf(func(js.Value, []js.Value) any {
		c.closeFromRemote()
		return nil
	})
	onMessage := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}

		data := args[0].Get("data")
		uint8Array := js.Global().Get("Uint8Array").New(data)
		buf := make([]byte, uint8Array.Get("byteLength").Int())
		js.CopyBytesToGo(buf, uint8Array)

		select {
		case c.readCh <- buf:
		case <-c.closed:
		}

		return nil
	})

	c.callbacks = []js.Func{onOpen, onError, onClose, onMessage}
	c.ws.Set("onopen", onOpen)
	c.ws.Set("onerror", onError)
	c.ws.Set("onclose", onClose)
	c.ws.Set("onmessage", onMessage)

	select {
	case err := <-opened:
		if err != nil {
			c.Close()
			return nil, err
		}

		return c, nil

	case <-time.After(30 * time.Second):
		c.Close()
		return nil, fmt.Errorf("websocket proxy connection timed out")
	}
}

func withTarget(proxyURL, target string) (string, error) {
	u, err := url.Parse(proxyURL)
	if err != nil {
		return "", err
	}

	q := u.Query()
	q.Set("target", target)
	u.RawQuery = q.Encode()

	return u.String(), nil
}

func (c *conn) Read(p []byte) (int, error) {
	c.mu.Lock()
	if len(c.buf) > 0 {
		n := copy(p, c.buf)
		c.buf = c.buf[n:]
		c.mu.Unlock()
		return n, nil
	}
	c.mu.Unlock()

	select {
	case b := <-c.readCh:
		n := copy(p, b)
		if n < len(b) {
			c.mu.Lock()
			c.buf = append(c.buf, b[n:]...)
			c.mu.Unlock()
		}

		return n, nil

	case <-c.closed:
		if c.err != nil {
			return 0, c.err
		}

		return 0, io.EOF
	}
}

func (c *conn) Write(p []byte) (int, error) {
	select {
	case <-c.closed:
		return 0, io.ErrClosedPipe
	default:
	}

	if c.ws.Get("readyState").Int() != 1 {
		return 0, io.ErrClosedPipe
	}

	buf := js.Global().Get("Uint8Array").New(len(p))
	js.CopyBytesToJS(buf, p)
	c.ws.Call("send", buf)

	return len(p), nil
}

func (c *conn) Close() error {
	c.closeOnce.Do(func() {
		if c.ws.Truthy() && c.ws.Get("readyState").Int() < 2 {
			c.ws.Call("close")
		}
		c.closeFromRemote()
	})

	return nil
}

func (c *conn) LocalAddr() net.Addr {
	return c.local
}

func (c *conn) RemoteAddr() net.Addr {
	return c.remote
}

func (c *conn) SetDeadline(time.Time) error {
	return nil
}

func (c *conn) SetReadDeadline(time.Time) error {
	return nil
}

func (c *conn) SetWriteDeadline(time.Time) error {
	return nil
}

func (c *conn) setError(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.err == nil {
		c.err = err
	}
}

func (c *conn) closeFromRemote() {
	select {
	case <-c.closed:
		return
	default:
		close(c.closed)
		c.releaseCallbacks()
	}
}

func (c *conn) releaseCallbacks() {
	c.releaseOnce.Do(func() {
		if c.ws.Truthy() {
			c.ws.Set("onopen", js.Null())
			c.ws.Set("onerror", js.Null())
			c.ws.Set("onclose", js.Null())
			c.ws.Set("onmessage", js.Null())
		}

		callbacks := append([]js.Func(nil), c.callbacks...)
		c.callbacks = nil

		release := js.FuncOf(func(js.Value, []js.Value) any {
			for _, cb := range callbacks {
				cb.Release()
			}
			return nil
		})

		js.Global().Call("setTimeout", release, 0)
	})
}
