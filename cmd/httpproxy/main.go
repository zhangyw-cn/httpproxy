package main

import (
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"httpproxy/internal/api"
	"httpproxy/internal/cert"
	"httpproxy/internal/proxy"
	"httpproxy/internal/store"
	"httpproxy/internal/ui"
)

func main() {
	proxyAddr := flag.String("proxy", "127.0.0.1:8080", "proxy listen address")
	uiAddr := flag.String("ui", "127.0.0.1:8081", "web UI listen address (default loopback)")
	maxFlows := flag.Int("max-flows", 1000, "max retained flows")
	maxBody := flag.Int64("max-body", 1<<20, "max stored body bytes per side")
	caDir := flag.String("ca-dir", "", "CA directory (default ~/.httpproxy)")
	flag.Parse()

	dir := *caDir
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			log.Fatal(err)
		}
		dir = filepath.Join(home, ".httpproxy")
	}

	ca, err := cert.LoadOrCreate(dir)
	if err != nil {
		log.Fatalf("ca: %v", err)
	}
	st := store.New(*maxFlows)
	apiServer := api.New(st, ca)
	proxyServer := proxy.New(*proxyAddr, apiServer.Sink(), *maxBody, ca)

	root := http.NewServeMux()
	apiHandler := apiServer.Handler()
	root.Handle("/api/", apiHandler)
	root.Handle("/ws/flows", apiHandler)
	root.Handle("/", ui.Handler())

	uiHost := listenHost(*uiAddr)
	handler := api.LocalOnly(root, uiHost, *uiAddr)

	go func() {
		log.Printf("UI on %s (loopback-restricted Host check)", *uiAddr)
		log.Fatal(http.ListenAndServe(*uiAddr, handler))
	}()

	log.Printf("proxy on %s (CA %s)", *proxyAddr, dir)
	log.Fatal(proxyServer.ListenAndServe())
}

func listenHost(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return strings.TrimSpace(addr)
	}
	return host
}
