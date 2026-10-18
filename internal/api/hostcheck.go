package api

import (
	"net"
	"net/http"
	"strings"
)

// LocalOnly wraps h so only requests whose Host is loopback (or an explicit
// extra host) are accepted. This mitigates LAN exposure and DNS rebinding
// against a MITM UI that serves decrypted traffic.
func LocalOnly(h http.Handler, extraHosts ...string) http.Handler {
	allowed := map[string]struct{}{
		"localhost": {},
		"127.0.0.1": {},
		"::1":       {},
		"[::1]":     {},
	}
	for _, host := range extraHosts {
		host = strings.TrimSpace(strings.ToLower(host))
		if host == "" {
			continue
		}
		allowed[host] = struct{}{}
		if h, _, err := net.SplitHostPort(host); err == nil {
			allowed[strings.ToLower(h)] = struct{}{}
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		host = strings.Trim(strings.ToLower(host), "[]")
		if _, ok := allowed[host]; !ok {
			if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
				http.Error(w, "forbidden host", http.StatusForbidden)
				return
			}
		}
		h.ServeHTTP(w, r)
	})
}
