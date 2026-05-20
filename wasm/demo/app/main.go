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
	svcDNS     string
	svcNetwork string

	syncMonitorCancel context.CancelFunc
)

func main() {
	neutrino.DisableDNSSeed = false

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
	dnsURL := defaultDNS()
	if len(args) > 1 && strings.TrimSpace(args[1].String()) != "" {
		dnsURL = strings.TrimSpace(args[1].String())
	}
	network := defaultNetwork()
	if len(args) > 2 && strings.TrimSpace(args[2].String()) != "" {
		network = normalizeNetwork(args[2].String())
	}

	go func() {
		status("initializing storage")
		if err := ensureService(proxy, dnsURL, network); err != nil {
			status("storage init failed")
			logf("storage init could not complete: %v", err)
			return
		}
		if err := startService(); err != nil {
			status("service start failed")
			logf("chain service could not start: %v", err)
			return
		}

		status("storage ready")
		log("storage initialized; DNS seed discovery is active")
		logBestBlock()
	}()

	return nil
}

func connect(_ js.Value, args []js.Value) any {
	peer := ""
	proxy := defaultProxy()
	if len(args) > 0 && strings.TrimSpace(args[0].String()) != "" {
		peer = strings.TrimSpace(args[0].String())
	}
	if len(args) > 1 && strings.TrimSpace(args[1].String()) != "" {
		proxy = normalizeProxyURL(args[1].String())
	}
	dnsURL := defaultDNS()
	if len(args) > 2 && strings.TrimSpace(args[2].String()) != "" {
		dnsURL = strings.TrimSpace(args[2].String())
	}
	network := defaultNetwork()
	if len(args) > 3 && strings.TrimSpace(args[3].String()) != "" {
		network = normalizeNetwork(args[3].String())
	}

	go func() {
		status("connecting")
		logf("requesting peer connection: %s", peer)
		if wasmtransport.IsOnionTarget(peer) {
			status("onion peer skipped")
			log("onion peer skipped; browser demo only dials clearnet peers")
			return
		}

		if err := ensureService(proxy, dnsURL, network); err != nil {
			status("connect setup failed")
			logf("connection setup could not complete: %v", err)
			return
		}

		if err := startService(); err != nil {
			status("service start failed")
			logf("chain service could not start: %v", err)
			return
		}

		if peer == "" {
			status("DNS discovery active")
			log("no explicit peer supplied; waiting for DNS seeded peers")
			return
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
		stopSyncMonitor()
		svc = nil
		svcStarted = false
		svcProxy = ""
		svcDNS = ""
		svcNetwork = ""
		setPeerCount(0)
		setPeerList("No connected peers.")
	}()

	return nil
}

func ensureService(proxy, dnsURL, network string) error {
	if svc != nil {
		if svcProxy != proxy || svcDNS != dnsURL || svcNetwork != network {
			return fmt.Errorf("service already initialized with proxy=%s dns=%s network=%s",
				svcProxy, svcDNS, svcNetwork)
		}

		return nil
	}

	params := paramsForNetwork(network)
	cfg := neutrino.Config{
		DataDir: "/",
		SQLConfig: &sqldb.Config{
			Backend: sqldb.BackendSqlite,
			Sqlite: &sqldbv2.SqliteConfig{
				BusyTimeout: 5 * time.Second,
			},
			SqliteFilename:      demoDBName(network),
			SkipLegacyMigration: true,
		},
		ChainParams:  params,
		NameResolver: loggingNameResolver(wasmtransport.NewDoHNameResolver(dnsURL)),
		AddrResolver: wasmtransport.NewAddrResolver(&params),
		Dialer:       wasmtransport.NewProxyDialer(proxy),
	}

	next, err := neutrino.NewChainService(cfg)
	if err != nil {
		return err
	}
	svc = next
	svcProxy = proxy
	svcDNS = dnsURL
	svcNetwork = network
	logf("chain service configured: network=%s proxy=%s dns=%s",
		network, proxy, dnsURL)

	return nil
}

func startService() error {
	if svcStarted {
		return nil
	}
	if err := svc.Start(context.Background()); err != nil {
		return err
	}
	svcStarted = true
	log("chain service started")
	startSyncMonitor()

	return nil
}

func startSyncMonitor() {
	stopSyncMonitor()

	ctx, cancel := context.WithCancel(context.Background())
	syncMonitorCancel = cancel

	go monitorSync(ctx)
}

func stopSyncMonitor() {
	if syncMonitorCancel != nil {
		syncMonitorCancel()
		syncMonitorCancel = nil
	}
}

func monitorSync(ctx context.Context) {
	logTips("sync initial state", true)

	sub, err := (&neutrino.RescanChainSource{
		ChainService: svc,
	}).Subscribe(0)
	if err != nil {
		logf("block notification subscription unavailable: %v", err)
	} else {
		defer sub.Cancel()
		go func() {
			for {
				select {
				case ntfn, ok := <-sub.Notifications:
					if !ok {
						return
					}
					if ntfn.Height()%2000 == 0 {
						logf("block notification: %s", ntfn)
					}

				case <-ctx.Done():
					return
				}
			}
		}()
	}

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	var lastHeaderHeight uint32
	var lastFilterHeight uint32
	var lastBestHeight int32 = -1
	var lastPeerCount = -1
	var initialized bool

	for {
		select {
		case <-ticker.C:
			headerHeight, filterHeight, bestHeight, peerCount, ok := syncState()
			if !ok {
				continue
			}

			if !initialized ||
				headerHeight != lastHeaderHeight ||
				filterHeight != lastFilterHeight ||
				bestHeight != lastBestHeight ||
				peerCount != lastPeerCount {

				logf("sync progress: peers=%d block_headers=%d filter_headers=%d best_usable=%d",
					peerCount, headerHeight, filterHeight, bestHeight)
				lastHeaderHeight = headerHeight
				lastFilterHeight = filterHeight
				lastBestHeight = bestHeight
				lastPeerCount = peerCount
				initialized = true
			}

		case <-ctx.Done():
			return
		}
	}
}

func logTips(prefix string, includePeers bool) {
	headerHeight, filterHeight, bestHeight, peerCount, ok := syncState()
	if !ok {
		return
	}

	if includePeers {
		logf("%s: peers=%d block_headers=%d filter_headers=%d best_usable=%d",
			prefix, peerCount, headerHeight, filterHeight, bestHeight)
		return
	}

	logf("%s: block_headers=%d filter_headers=%d best_usable=%d",
		prefix, headerHeight, filterHeight, bestHeight)
}

func syncState() (uint32, uint32, int32, int, bool) {
	if svc == nil {
		return 0, 0, 0, 0, false
	}

	_, headerHeight, err := svc.BlockHeaders.ChainTip()
	if err != nil {
		logf("block header tip unavailable: %v", err)
		return 0, 0, 0, 0, false
	}

	_, filterHeight, err := svc.RegFilterHeaders.ChainTip()
	if err != nil {
		logf("filter header tip unavailable: %v", err)
		return 0, 0, 0, 0, false
	}

	bestHeight := int32(-1)
	if stamp, err := svc.BestBlock(); err == nil {
		bestHeight = stamp.Height
	}

	return headerHeight, filterHeight, bestHeight, len(svc.Peers()), true
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

func defaultDNS() string {
	return wasmtransport.DefaultDoHEndpoint
}

func defaultNetwork() string {
	return "signet"
}

func normalizeNetwork(network string) string {
	switch strings.ToLower(strings.TrimSpace(network)) {
	case "main", "mainnet", "bitcoin":
		return "mainnet"
	case "test", "testnet", "testnet3":
		return "testnet"
	case "signet", "sig":
		return "signet"
	default:
		return defaultNetwork()
	}
}

func paramsForNetwork(network string) chaincfg.Params {
	switch normalizeNetwork(network) {
	case "mainnet":
		return chaincfg.MainNetParams
	case "testnet":
		return chaincfg.TestNet3Params
	default:
		return chaincfg.SigNetParams
	}
}

func demoDBName(network string) string {
	name := strings.TrimSpace(js.Global().Get("neutrinoDemoDBName").String())
	if name == "" || name == "<undefined>" || name == "<null>" {
		return fmt.Sprintf("neutrino-wasm-demo-%s.sqlite", network)
	}

	return name
}

func loggingNameResolver(resolve func(string) ([]net.IP, error)) func(string) ([]net.IP, error) {
	return func(host string) ([]net.IP, error) {
		logf("DNS seed lookup: %s", host)
		ips, err := resolve(host)
		if err != nil {
			logf("DNS seed lookup failed: %s: %v", host, err)
			return nil, err
		}

		logf("%d DNS seed address(es) found from %s", len(ips), host)
		return ips, nil
	}
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
