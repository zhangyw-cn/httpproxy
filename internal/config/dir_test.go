package config

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestOpen(t *testing.T) {
	t.Run("creates private dir", func(t *testing.T) {
		work := t.TempDir()
		d, got, err := Open(work)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, Defaults()) {
			t.Fatalf("%+v", got)
		}
		if !strings.HasSuffix(d.Path(), ".httpproxy") {
			t.Fatalf("path %s", d.Path())
		}

		info, err := os.Stat(d.Path())
		if err != nil {
			t.Fatal(err)
		}
		if !info.IsDir() || info.Mode().Perm() != 0o700 {
			t.Fatalf("mode %o", info.Mode().Perm())
		}

		cfg, err := os.ReadFile(filepath.Join(d.Path(), "config.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		if string(cfg) != string(MarshalYAML(Defaults())) {
			t.Fatalf("config %q", cfg)
		}

		gi, err := os.ReadFile(filepath.Join(d.Path(), ".gitignore"))
		if err != nil {
			t.Fatal(err)
		}
		if string(gi) != "*\n" {
			t.Fatalf("gitignore %q", gi)
		}
	})

	t.Run("keeps existing gitignore", func(t *testing.T) {
		work := t.TempDir()
		d, _, err := Open(work)
		if err != nil {
			t.Fatal(err)
		}
		giPath := filepath.Join(d.Path(), ".gitignore")
		if err := os.WriteFile(giPath, []byte("keep\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, _, err := Open(work); err != nil {
			t.Fatal(err)
		}
		gi, err := os.ReadFile(giPath)
		if err != nil {
			t.Fatal(err)
		}
		if string(gi) != "keep\n" {
			t.Fatalf("gitignore %q", gi)
		}
	})

	t.Run("tightens existing directory", func(t *testing.T) {
		work := t.TempDir()
		dir := filepath.Join(work, ".httpproxy")
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o755 {
			t.Fatalf("before %o", info.Mode().Perm())
		}
		if _, _, err := Open(work); err != nil {
			t.Fatal(err)
		}
		info, err = os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o700 {
			t.Fatalf("after %o", info.Mode().Perm())
		}
	})

	t.Run("rejects file", func(t *testing.T) {
		work := t.TempDir()
		path := filepath.Join(work, ".httpproxy")
		body := []byte("not-a-dir")
		if err := os.WriteFile(path, body, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, _, err := Open(work); err == nil || !strings.Contains(err.Error(), "is not a directory") {
			t.Fatalf("got %v", err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.IsDir() {
			t.Fatal("file replaced")
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, body) {
			t.Fatalf("bytes %q", got)
		}
	})

	t.Run("rejects bad config", func(t *testing.T) {
		work := t.TempDir()
		dir := filepath.Join(work, ".httpproxy")
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		cfgPath := filepath.Join(dir, "config.yaml")
		body := []byte("nope: 1\n")
		if err := os.WriteFile(cfgPath, body, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, _, err := Open(work); err == nil {
			t.Fatal("expected error")
		}
		got, err := os.ReadFile(cfgPath)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, body) {
			t.Fatalf("bytes %q", got)
		}
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o700 {
			t.Fatalf("mode %o", info.Mode().Perm())
		}
		gi, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
		if err != nil {
			t.Fatal(err)
		}
		if string(gi) != "*\n" {
			t.Fatalf("gitignore %q", gi)
		}
	})
}

func TestSave(t *testing.T) {
	d, _, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	v := Defaults()
	v.MaxFlows = 2
	v.Theme = "light"
	if err := d.Save(v); err != nil {
		t.Fatal(err)
	}
	cfg, err := os.ReadFile(filepath.Join(d.Path(), "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(cfg) != string(MarshalYAML(v)) {
		t.Fatalf("config %q", cfg)
	}
	got, err := ParseYAML(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, v) {
		t.Fatalf("%+v", got)
	}
	matches, err := filepath.Glob(filepath.Join(d.Path(), ".config-*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temps %v", matches)
	}
}
