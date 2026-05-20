//go:build js && wasm

package main

import (
	"context"
	"fmt"
	"net"
	"strings"
	"syscall/js"
	"time"

	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/wire"
	sqldbv2 "github.com/lightningnetwork/lnd/sqldb/v2"

	"github.com/lightninglabs/neutrino"
	"github.com/lightninglabs/neutrino/sqldb"
	"github.com/lightninglabs/neutrino/wasmtransport"
)

var (
	svc        *neutrino.ChainService
	svcStarted bool
	svcProxy   string
)

func main() {
	neutrino.DisableDNSSeed = true
	neutrino.TargetOutbound = 3

	api := map[string]any{
		"initStorage": js.FuncOf(initStorage),
		"connect":     js.FuncOf(connect),
		"bestBlock":   js.FuncOf(bestBlock),
		"fetchTip":    js.FuncOf(fetchTip),
		"listPeers":   js.FuncOf(listPeers),
		"stop":        js.FuncOf(stop),
	}

	js.Global().Set("neutrinoDemo", js.ValueOf(api))
	log("wasm runtime ready")
	go publishPeerCount()
	select {}
}

func initStorage(_ js.Value, args []js.Value) any {
	proxy := defaultProxy()
	if len(args) > 0 && strings.TrimSpace(args[0].String()) != "" {
		proxy = normalizeProxyURL(args[0].String())
	}

	go func() {
		status("initializing storage")
		if err := ensureService(proxy); err != nil {
			status("storage init failed")
			logf("storage init could not complete: %v", err)
			return
		}

		status("storage ready")
		log("storage initialized")
		logBestBlock()
	}()

	return nil
}

func connect(_ js.Value, args []js.Value) any {
	peer := "116.202.84.94:8333"
	proxy := defaultProxy()
	if len(args) > 0 && strings.TrimSpace(args[0].String()) != "" {
		peer = strings.TrimSpace(args[0].String())
	}
	if len(args) > 1 && strings.TrimSpace(args[1].String()) != "" {
		proxy = normalizeProxyURL(args[1].String())
	}

	go func() {
		status("connecting")
		logf("requesting peer connection: %s", peer)
		if isOnionTarget(peer) {
			status("onion peer skipped")
			log("onion peer skipped; browser demo only dials clearnet peers")
			return
		}

		if err := ensureService(proxy); err != nil {
			status("connect setup failed")
			logf("connection setup could not complete: %v", err)
			return
		}

		if !svcStarted {
			if err := svc.Start(context.Background()); err != nil {
				status("service start failed")
				logf("chain service could not start: %v", err)
				return
			}
			svcStarted = true
			log("chain service started; discovery is active")
		}

		if err := svc.ConnectNode(peer, true); err != nil {
			status("peer request skipped")
			logf("peer connection was not queued: %v", err)
			return
		}
		log("peer connection requested; waiting for handshake")

		for i := 0; i < 30; i++ {
			count := emitPeers(false)
			if count > 0 {
				statusf("%d peer(s) connected", count)
				return
			}

			time.Sleep(time.Second)
		}

		status("waiting for peers")
		log("no completed peer handshake yet; discovery may keep trying")
	}()

	return nil
}

func bestBlock(js.Value, []js.Value) any {
	go func() {
		status("reading best block")
		log("best block lookup started")
		logBestBlock()
	}()
	return nil
}

func fetchTip(js.Value, []js.Value) any {
	go func() {
		status("fetching tip filter")
		if svc == nil {
			status("service not initialized")
			log("chain service is not initialized")
			return
		}

		stamp, err := svc.BestBlock()
		if err != nil {
			status("best block unavailable")
			logf("best block lookup could not complete: %v", err)
			return
		}

		filter, err := svc.GetCFilter(stamp.Hash, wire.GCSFilterRegular)
		if err != nil {
			status("tip filter unavailable")
			logf("tip filter is not available yet: %v", err)
			return
		}

		encoded, err := filter.NBytes()
		if err != nil {
			status("tip filter encode failed")
			logf("tip filter encode could not complete: %v", err)
			return
		}

		status("tip filter ready")
		logf("tip filter bytes=%d height=%d", len(encoded), stamp.Height)
	}()

	return nil
}

func listPeers(js.Value, []js.Value) any {
	go emitPeers(true)
	return nil
}

func stop(js.Value, []js.Value) any {
	go func() {
		if svc == nil {
			log("chain service is not running")
			status("stopped")
			return
		}

		if err := svc.Stop(); err != nil {
			status("stop failed")
			logf("chain service stop could not complete: %v", err)
		} else {
			status("stopped")
			log("chain service stopped")
		}
		svc = nil
		svcStarted = false
		svcProxy = ""
		setPeerCount(0)
		setPeerList("No connected peers.")
	}()

	return nil
}

func ensureService(proxy string) error {
	if svc != nil {
		if svcProxy != proxy {
			return fmt.Errorf("service already initialized with proxy %s", svcProxy)
		}

		return nil
	}

	cfg := neutrino.Config{
		DataDir: "/",
		SQLConfig: &sqldb.Config{
			Backend: sqldb.BackendSqlite,
			Sqlite: &sqldbv2.SqliteConfig{
				BusyTimeout: 5 * time.Second,
			},
			SqliteFilename:      demoDBName(),
			SkipLegacyMigration: true,
		},
		ChainParams: chaincfg.MainNetParams,
		AddrResolver: func(addr string) (net.Addr, error) {
			if isOnionTarget(addr) {
				return nil, fmt.Errorf("onion peer skipped by browser demo")
			}

			return wasmtransport.NewAddr(addr), nil
		},
		Dialer: wasmtransport.NewProxyDialer(proxy),
	}

	next, err := neutrino.NewChainService(cfg)
	if err != nil {
		return err
	}
	svc = next
	svcProxy = proxy

	return nil
}

func logBestBlock() {
	if svc == nil {
		log("chain service is not initialized")
		return
	}

	stamp, err := svc.BestBlock()
	if err != nil {
		status("best block unavailable")
		logf("best block lookup could not complete: %v", err)
		return
	}

	status("best block ready")
	logf("best block height=%d hash=%s time=%s",
		stamp.Height, stamp.Hash, stamp.Timestamp.Format(time.RFC3339))
}

func emitPeers(verbose bool) int {
	if svc == nil {
		setPeerCount(0)
		setPeerList("Chain service is not initialized.")
		if verbose {
			log("chain service is not initialized")
		}
		return 0
	}

	peers := svc.Peers()
	setPeerCount(len(peers))
	if len(peers) == 0 {
		setPeerList("No connected peers.")
		if verbose {
			log("connected peers: 0")
		}
		return 0
	}

	lines := make([]string, 0, len(peers))
	for _, peer := range peers {
		lines = append(lines, fmt.Sprintf("%s\n  last_block=%d services=%v",
			peer.Addr(), peer.LastBlock(), peer.Services()))
	}
	setPeerList(strings.Join(lines, "\n\n"))

	if verbose {
		logf("connected peers: %d", len(peers))
		for _, peer := range peers {
			logf("peer %s last_block=%d services=%v",
				peer.Addr(), peer.LastBlock(), peer.Services())
		}
	}

	return len(peers)
}

func publishPeerCount() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for range ticker.C {
		if svc == nil {
			setPeerCount(0)
			continue
		}

		emitPeers(false)
	}
}

func normalizeProxyURL(proxy string) string {
	return strings.ReplaceAll(strings.TrimSpace(proxy), ",", ".")
}

func defaultProxy() string {
	return "wss://konwss.tunn.dev/peer-proxy"
}

func demoDBName() string {
	name := strings.TrimSpace(js.Global().Get("neutrinoDemoDBName").String())
	if name == "" || name == "<undefined>" || name == "<null>" {
		return "neutrino-wasm-demo.sqlite"
	}

	return name
}

func isOnionTarget(target string) bool {
	host := strings.TrimSpace(target)
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
	} else if idx := strings.LastIndex(host, ":"); idx > 0 {
		host = host[:idx]
	}

	return strings.HasSuffix(strings.ToLower(strings.Trim(host, "[]")), ".onion")
}

func log(msg string) {
	cb := js.Global().Get("neutrinoLog")
	if cb.Type() == js.TypeFunction {
		cb.Invoke(msg)
		return
	}

	js.Global().Get("console").Call("log", msg)
}

func logf(format string, args ...any) {
	log(fmt.Sprintf(format, args...))
}

func status(msg string) {
	cb := js.Global().Get("neutrinoStatus")
	if cb.Type() == js.TypeFunction {
		cb.Invoke(msg)
	}
}

func statusf(format string, args ...any) {
	status(fmt.Sprintf(format, args...))
}

func setPeerCount(count int) {
	cb := js.Global().Get("neutrinoPeerCount")
	if cb.Type() == js.TypeFunction {
		cb.Invoke(count)
	}
}

func setPeerList(text string) {
	cb := js.Global().Get("neutrinoPeerList")
	if cb.Type() == js.TypeFunction {
		cb.Invoke(text)
	}
}
