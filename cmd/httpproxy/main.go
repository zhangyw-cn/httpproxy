package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"httpproxy/internal/api"
	"httpproxy/internal/cert"
	"httpproxy/internal/proxy"
	"httpproxy/internal/store"
	"httpproxy/internal/ui"
)

func main() {
	proxyAddr := flag.String("proxy", ":8080", "proxy listen address")
	uiAddr := flag.String("ui", ":8081", "web UI listen address")
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

	go func() {
		log.Printf("UI on %s", *uiAddr)
		log.Fatal(http.ListenAndServe(*uiAddr, root))
	}()

	log.Printf("proxy on %s (CA %s)", *proxyAddr, dir)
	log.Fatal(proxyServer.ListenAndServe())
}
