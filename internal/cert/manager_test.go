package cert_test

import (
	"crypto/tls"
	"crypto/x509"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zhangyw-cn/httpproxy/internal/cert"
)

func TestLoadOrCreateAndMITMHandshake(t *testing.T) {
	dir := t.TempDir()
	manager, err := cert.LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}

	reloaded, err := cert.LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	if string(manager.CACertPEM()) != string(reloaded.CACertPEM()) {
		t.Fatal("CA should be stable across loads")
	}
	keyInfo, err := os.Stat(filepath.Join(dir, "ca.key"))
	if err != nil {
		t.Fatal(err)
	}
	if got := keyInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("ca.key mode = %o, want 600", got)
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(manager.CACertPEM()) {
		t.Fatal("append generated CA")
	}
	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		GetCertificate: manager.GetCertificate,
		NextProtos:     []string{"http/1.1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	})}
	defer server.Close()
	go func() {
		_ = server.Serve(listener)
	}()

	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		RootCAs:    pool,
		ServerName: "example.com",
	}}}
	defer client.CloseIdleConnections()
	response, err := client.Get("https://" + listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || string(body) != "ok" {
		t.Fatalf("response = %d %q, want 200 %q", response.StatusCode, body, "ok")
	}
}

func TestGetCertificateCachesLeafWithDNSName(t *testing.T) {
	manager, err := cert.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hello := &tls.ClientHelloInfo{ServerName: "api.example.com"}
	first, err := manager.GetCertificate(hello)
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.GetCertificate(hello)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("expected leaf certificate to be cached by hostname")
	}
	leaf, err := x509.ParseCertificate(first.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(leaf.DNSNames) != 1 || leaf.DNSNames[0] != hello.ServerName {
		t.Fatalf("DNSNames = %v, want [%q]", leaf.DNSNames, hello.ServerName)
	}
}

func TestLoadOrCreateRejectsMismatchedCertificateAndKey(t *testing.T) {
	firstDir := t.TempDir()
	secondDir := t.TempDir()
	if _, err := cert.LoadOrCreate(firstDir); err != nil {
		t.Fatal(err)
	}
	if _, err := cert.LoadOrCreate(secondDir); err != nil {
		t.Fatal(err)
	}

	secondKey, err := os.ReadFile(filepath.Join(secondDir, "ca.key"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(firstDir, "ca.key"), secondKey, 0o600); err != nil {
		t.Fatal(err)
	}

	_, err = cert.LoadOrCreate(firstDir)
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("LoadOrCreate error = %v, want clear key mismatch error", err)
	}
}

func TestLoadOrCreateRejectsIncompletePair(t *testing.T) {
	dir := t.TempDir()
	if _, err := cert.LoadOrCreate(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "ca.key")); err != nil {
		t.Fatal(err)
	}

	_, err := cert.LoadOrCreate(dir)
	if err == nil || !strings.Contains(err.Error(), "incomplete CA files") {
		t.Fatalf("LoadOrCreate error = %v, want incomplete pair error", err)
	}
}

func TestLoadOrCreateRejectsCorruptPEM(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ca.crt"), []byte("not PEM"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ca.key"), []byte("not PEM"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := cert.LoadOrCreate(dir)
	if err == nil || !strings.Contains(err.Error(), "parse CA certificate") {
		t.Fatalf("LoadOrCreate error = %v, want certificate parse error", err)
	}
}
