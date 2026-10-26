package proxy

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"testing"
	"time"

	"httpproxy/internal/cert"
)

func medianDuration(samples []time.Duration) time.Duration {
	ordered := append([]time.Duration(nil), samples...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	return ordered[len(ordered)/2]
}

func BenchmarkWarmHTTPS(b *testing.B) {
	body := bytes.Repeat([]byte("a"), 1024)
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	b.Cleanup(upstream.Close)
	manager, err := cert.LoadOrCreate(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(manager.CACertPEM()) {
		b.Fatal("failed to trust proxy CA")
	}
	p := New("127.0.0.1:0", &memSink{}, 2048, manager)
	p.UpstreamTLS = &tls.Config{InsecureSkipVerify: true}
	errCh := make(chan error, 1)
	go func() { errCh <- p.ListenAndServe() }()
	b.Cleanup(func() {
		_ = p.Close()
		if err := <-errCh; err != nil {
			b.Errorf("ListenAndServe: %v", err)
		}
	})
	var addr string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if addr = p.Addr(); addr != "" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if addr == "" {
		b.Fatal("proxy did not start listening")
	}
	proxyURL, err := url.Parse("http://" + addr)
	if err != nil {
		b.Fatal(err)
	}
	through := &http.Client{Transport: &http.Transport{
		Proxy:              http.ProxyURL(proxyURL),
		TLSClientConfig:    &tls.Config{RootCAs: pool},
		DisableCompression: true,
	}}
	direct := &http.Client{Transport: &http.Transport{
		TLSClientConfig:    &tls.Config{InsecureSkipVerify: true},
		DisableCompression: true,
	}}
	sample := func(client *http.Client) time.Duration {
		start := time.Now()
		resp, err := client.Get(upstream.URL)
		if err != nil {
			b.Fatal(err)
		}
		defer resp.Body.Close()
		if _, err := io.Copy(io.Discard, resp.Body); err != nil {
			b.Fatal(err)
		}
		return time.Since(start)
	}
	_ = sample(through)
	_ = sample(direct)
	const n = 20
	proxied := make([]time.Duration, n)
	straight := make([]time.Duration, n)
	for i := 0; i < n; i++ {
		proxied[i] = sample(through)
		straight[i] = sample(direct)
	}
	overhead := medianDuration(proxied) - medianDuration(straight)
	fmt.Printf("warm https median overhead: %s\n", overhead)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = sample(through)
	}
}
