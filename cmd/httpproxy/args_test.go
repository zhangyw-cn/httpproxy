package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseArgsNil(t *testing.T) {
	opt, err := parseArgs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if opt.workdir != "" {
		t.Fatalf("workdir %q", opt.workdir)
	}
	if opt.proxy != "" || opt.ui != "" || opt.maxFlows != 0 || opt.maxBody != 0 {
		t.Fatalf("%+v", opt)
	}
	if opt.proxySet || opt.uiSet || opt.maxFlowsSet || opt.maxBodySet {
		t.Fatalf("set flags %+v", opt)
	}
}

func TestParseArgsWorkdirAroundFlags(t *testing.T) {
	a, err := parseArgs([]string{"-proxy", "127.0.0.1:1", "/tmp/proj"})
	if err != nil {
		t.Fatal(err)
	}
	if a.workdir != "/tmp/proj" || a.proxy != "127.0.0.1:1" || !a.proxySet {
		t.Fatalf("%+v", a)
	}
	if a.uiSet || a.maxFlowsSet || a.maxBodySet {
		t.Fatalf("%+v", a)
	}

	b, err := parseArgs([]string{"/tmp/proj", "-max-flows", "50"})
	if err != nil {
		t.Fatal(err)
	}
	if b.workdir != "/tmp/proj" || b.maxFlows != 50 || !b.maxFlowsSet {
		t.Fatalf("%+v", b)
	}
	if b.proxySet || b.uiSet || b.maxBodySet {
		t.Fatalf("%+v", b)
	}
}

func TestParseArgsEqualsAndSeparate(t *testing.T) {
	opt, err := parseArgs([]string{"-proxy=127.0.0.1:2", "-ui", "127.0.0.1:3"})
	if err != nil {
		t.Fatal(err)
	}
	if opt.proxy != "127.0.0.1:2" || opt.ui != "127.0.0.1:3" || !opt.proxySet || !opt.uiSet {
		t.Fatalf("%+v", opt)
	}
	if opt.workdir != "" {
		t.Fatalf("workdir %q", opt.workdir)
	}
}

func TestParseArgsDashDash(t *testing.T) {
	opt, err := parseArgs([]string{"--", "-odd"})
	if err != nil {
		t.Fatal(err)
	}
	if opt.workdir != "-odd" {
		t.Fatalf("%+v", opt)
	}
	if opt.proxySet || opt.uiSet || opt.maxFlowsSet || opt.maxBodySet {
		t.Fatalf("%+v", opt)
	}
}

func TestParseArgsExtraPositional(t *testing.T) {
	_, err := parseArgs([]string{"a", "b"})
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "unexpected extra arguments" {
		t.Fatalf("error %q", err)
	}
}

func TestParseArgsUnknownCADir(t *testing.T) {
	if _, err := parseArgs([]string{"-ca-dir", "/tmp"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveWorkdirEmpty(t *testing.T) {
	abs, code, err := resolveWorkdir("")
	if err != nil || code != 0 {
		t.Fatalf("%q %d %v", abs, code, err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if abs != wd {
		t.Fatalf("got %q want %q", abs, wd)
	}
}

func TestResolveWorkdirMissingAndFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	if _, code, err := resolveWorkdir(missing); err == nil || code != 2 {
		t.Fatalf("missing: code=%d err=%v", code, err)
	}
	f := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, code, err := resolveWorkdir(f); err == nil || code != 2 {
		t.Fatalf("file: code=%d err=%v", code, err)
	}
}

func TestRunRejectsBadFlagBeforeCreate(t *testing.T) {
	workdir := t.TempDir()
	if code := run([]string{"-max-flows", "0", workdir}); code != 2 {
		t.Fatalf("max-flows: exit %d", code)
	}
	if _, err := os.Stat(filepath.Join(workdir, ".httpproxy")); !os.IsNotExist(err) {
		t.Fatalf(".httpproxy exists or stat err=%v", err)
	}

	missing := filepath.Join(t.TempDir(), "missing")
	if code := run([]string{missing}); code != 2 {
		t.Fatalf("missing: exit %d", code)
	}
}

func TestRunListenFailureCreatesPrivateDir(t *testing.T) {
	workdir := t.TempDir()
	code := run([]string{"-proxy", "127.0.0.1:1", "-ui", "127.0.0.1:1", workdir})
	if code != 1 {
		t.Fatalf("exit %d", code)
	}
	base := filepath.Join(workdir, ".httpproxy")
	if _, err := os.Stat(filepath.Join(base, "config.yaml")); err != nil {
		t.Fatal(err)
	}
	key, err := os.Stat(filepath.Join(base, "ca.key"))
	if err != nil {
		t.Fatal(err)
	}
	if key.Mode().Perm() != 0o600 {
		t.Fatalf("ca.key mode %o", key.Mode().Perm())
	}
	dir, err := os.Stat(base)
	if err != nil {
		t.Fatal(err)
	}
	if !dir.IsDir() || dir.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode %o", dir.Mode().Perm())
	}
}
