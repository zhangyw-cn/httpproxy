package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"

	"httpproxy/internal/api"
	"httpproxy/internal/cert"
	"httpproxy/internal/config"
	"httpproxy/internal/proxy"
	"httpproxy/internal/store"
	"httpproxy/internal/ui"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	opt, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 2
	}

	candidate := config.Defaults()
	if opt.proxySet {
		candidate.Proxy = opt.proxy
	}
	if opt.uiSet {
		candidate.UI = opt.ui
	}
	if opt.maxFlowsSet {
		candidate.MaxFlows = opt.maxFlows
	}
	if opt.maxBodySet {
		candidate.MaxBody = opt.maxBody
	}
	if _, err := config.ParseYAML(config.MarshalYAML(candidate)); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 2
	}

	workdir, code, err := resolveWorkdir(opt.workdir)
	if err != nil {
		if code == 1 {
			fmt.Fprintf(os.Stderr, "getcwd: %v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "workdir: %v\n", err)
		}
		return code
	}

	dir, file, err := config.Open(workdir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}
	ca, err := cert.LoadOrCreate(dir.Path())
	if err != nil {
		fmt.Fprintf(os.Stderr, "ca: %v\n", err)
		return 1
	}
	if err := config.ChmodPrivate(dir.Path()); err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}

	session := config.NewSession(dir, file, config.Overrides{
		Proxy:    opt.proxySet,
		UI:       opt.uiSet,
		MaxFlows: opt.maxFlowsSet,
		MaxBody:  opt.maxBodySet,
	}, opt.proxy, opt.ui, opt.maxFlows, opt.maxBody)
	_, runtime := session.Snapshot()
	st := store.New(runtime.MaxFlows)
	apiServer := api.New(st, ca, session, nil)
	proxyServer := proxy.New(runtime.Proxy, apiServer.Sink(), runtime.MaxBody, ca)
	apiServer.SetProxy(proxyServer)

	root := http.NewServeMux()
	apiHandler := apiServer.Handler()
	root.Handle("/api/", apiHandler)
	root.Handle("/ws/flows", apiHandler)
	root.Handle("/", ui.Handler())

	handler := api.LocalOnly(root, listenHost(runtime.UI), runtime.UI)

	errc := make(chan error, 2)
	go func() {
		log.Printf("UI on %s (loopback-restricted Host check)", runtime.UI)
		errc <- http.ListenAndServe(runtime.UI, handler)
	}()
	log.Printf("proxy on %s (CA %s)", runtime.Proxy, dir.Path())
	go func() {
		errc <- proxyServer.ListenAndServe()
	}()
	if err := <-errc; err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	return 0
}

func listenHost(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return strings.TrimSpace(addr)
	}
	return host
}
