package main

import (
	"flag"
	"log"
	"net/http"

	wasmsqlite "github.com/sputn1ck/go-wasmsqlite"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8090", "listen address")
	dir := flag.String("dir", "wasm/demo/static", "static file directory")
	flag.Parse()

	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.Dir(*dir)))
	mux.Handle("/sqlite/", http.StripPrefix("/sqlite", wasmsqlite.AssetHandler()))
	mux.Handle("/sqlite-bridge.js", wasmsqlite.AssetHandler())
	mux.Handle("/sqlite-worker.js", wasmsqlite.AssetHandler())
	mux.Handle("/sqlite3.js", wasmsqlite.AssetHandler())
	mux.Handle("/sqlite3.wasm", wasmsqlite.AssetHandler())
	mux.Handle("/sqlite3-opfs-async-proxy.js", wasmsqlite.AssetHandler())

	handler := wasmsqlite.WithCrossOriginIsolation(mux)

	log.Printf("serving neutrino wasm demo on http://%s", *addr)
	log.Fatal(http.ListenAndServe(*addr, handler))
}
