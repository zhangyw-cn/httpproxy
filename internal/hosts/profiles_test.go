package hosts

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestOpenProfilesCreatesHostsDir(t *testing.T) {
	root := t.TempDir()
	p, err := OpenProfiles(root)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(root, "hosts"))
	if err != nil || !info.IsDir() {
		t.Fatalf("dir: %v", err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}
	if p.Dir() != filepath.Join(root, "hosts") {
		t.Fatalf("Dir() = %q", p.Dir())
	}
}

func TestProfilesCRUD(t *testing.T) {
	p, _ := OpenProfiles(t.TempDir())
	if err := p.Create("测试 环境"); err != nil {
		t.Fatal(err)
	}
	if err := p.Write("测试 环境", []byte("10.0.0.5 api.example.com\n")); err != nil {
		t.Fatal(err)
	}
	data, err := p.Read("测试 环境")
	if err != nil || string(data) != "10.0.0.5 api.example.com\n" {
		t.Fatalf("%q %v", data, err)
	}
	if err := p.Rename("测试 环境", "预发"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Read("测试 环境"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old: %v", err)
	}
	data, err = p.Read("预发")
	if err != nil || string(data) != "10.0.0.5 api.example.com\n" {
		t.Fatalf("renamed: %q %v", data, err)
	}
	if err := p.Delete("预发"); err != nil {
		t.Fatal(err)
	}
	if err := p.Delete("预发"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete: %v", err)
	}
	if _, err := p.Read("预发"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("read deleted: %v", err)
	}
}

func TestProfilesListSortsAndIgnores(t *testing.T) {
	p, _ := OpenProfiles(t.TempDir())
	for _, name := range []string{"b", "A", "c"} {
		if err := p.Create(name); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(p.Dir(), "notes.txt"), []byte("x"), 0o644)
	os.Mkdir(filepath.Join(p.Dir(), "dir.hosts"), 0o755)
	os.WriteFile(filepath.Join(p.Dir(), ".hidden.hosts"), []byte("x"), 0o644)
	got, err := p.List()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"A", "b", "c"}) {
		t.Fatalf("%v", got)
	}
}

func TestProfilesListSkipsInvalidHostsNames(t *testing.T) {
	p, _ := OpenProfiles(t.TempDir())
	for _, name := range []string{"ok", "ok two"} {
		if err := p.Create(name); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range []string{
		"bad\tname.hosts",
		strings.Repeat("x", 65) + ".hosts",
		"trailing .hosts",
		"UPPER.HOSTS",
	} {
		if err := os.WriteFile(filepath.Join(p.Dir(), file), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := p.List()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"ok", "ok two"}) {
		t.Fatalf("%v", got)
	}
}

func TestProfilesListEmptyIsNotNil(t *testing.T) {
	p, _ := OpenProfiles(t.TempDir())
	got, err := p.List()
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("%#v", got)
	}
}

func TestProfilesRejectsNamesAndDuplicates(t *testing.T) {
	p, _ := OpenProfiles(t.TempDir())
	for _, name := range []string{"", ".hidden", "a/b", "..", strings.Repeat("x", 65)} {
		if err := p.Create(name); !errors.Is(err, ErrInvalidName) {
			t.Fatalf("%q => %v", name, err)
		}
	}
	if err := p.Create("Test"); err != nil {
		t.Fatal(err)
	}
	if err := p.Create("test"); !errors.Is(err, ErrExists) {
		t.Fatalf("case duplicate: %v", err)
	}
	if err := p.Rename("Test", "TEST"); !errors.Is(err, ErrExists) {
		t.Fatalf("rename duplicate: %v", err)
	}
}

func TestProfilesCreateTrimsName(t *testing.T) {
	p, _ := OpenProfiles(t.TempDir())
	if err := p.Create("  padded  "); err != nil {
		t.Fatal(err)
	}
	got, err := p.List()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"padded"}) {
		t.Fatalf("%v", got)
	}
	if err := p.Write("padded", []byte("10.0.0.1 a.example.com\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Read(" padded "); err != nil {
		t.Fatalf("read padded: %v", err)
	}
}

func TestProfilesWriteMissingProfile(t *testing.T) {
	p, _ := OpenProfiles(t.TempDir())
	if err := p.Write("missing", []byte("x")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("%v", err)
	}
}

func TestProfilesRenameMissingAndInvalid(t *testing.T) {
	p, _ := OpenProfiles(t.TempDir())
	if err := p.Rename("missing", "new"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing old: %v", err)
	}
	if err := p.Create("exists"); err != nil {
		t.Fatal(err)
	}
	if err := p.Rename("exists", "bad/name"); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("invalid new: %v", err)
	}
}

func TestProfilesWriteIsAtomic(t *testing.T) {
	p, _ := OpenProfiles(t.TempDir())
	if err := p.Create("live"); err != nil {
		t.Fatal(err)
	}
	oldData := bytes.Repeat([]byte("10.0.0.1 old.example.com\n"), 2048)
	newData := bytes.Repeat([]byte("10.0.0.2 new.example.com\n"), 2048)
	if err := p.Write("live", oldData); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(p.Dir(), "live.hosts"))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Fatalf("mode %o", got)
	}

	done := make(chan struct{})
	bad := make(chan string, 1)
	go func() {
		for {
			select {
			case <-done:
				return
			default:
			}
			got, err := p.Read("live")
			if err != nil {
				select {
				case bad <- err.Error():
				default:
				}
				return
			}
			if !bytes.Equal(got, oldData) && !bytes.Equal(got, newData) {
				select {
				case bad <- "observed partial content":
				default:
				}
				return
			}
		}
	}()

	for i := 0; i < 100; i++ {
		if err := p.Write("live", newData); err != nil {
			close(done)
			t.Fatal(err)
		}
		if err := p.Write("live", oldData); err != nil {
			close(done)
			t.Fatal(err)
		}
	}
	close(done)

	select {
	case msg := <-bad:
		t.Fatal(msg)
	default:
	}
	got, err := p.Read("live")
	if err != nil || !bytes.Equal(got, oldData) {
		t.Fatalf("final content: %v", err)
	}
	entries, err := os.ReadDir(p.Dir())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "live.hosts" {
		t.Fatalf("leftover files: %v", entries)
	}
}

func TestValidName(t *testing.T) {
	valid := []string{
		"a",
		"A",
		"测试 环境",
		"预发-1_v2.0",
		"a b",
		strings.Repeat("测", 64),
		" padded ",
	}
	for _, name := range valid {
		if !ValidName(name) {
			t.Errorf("ValidName(%q) = false, want true", name)
		}
	}
	invalid := []string{
		"",
		"   ",
		".",
		"..",
		".hidden",
		"...",
		"a/b",
		`a\b`,
		"a\nb",
		"a\tb",
		"a:b",
		"a*b",
		"😀",
		strings.Repeat("x", 65),
	}
	for _, name := range invalid {
		if ValidName(name) {
			t.Errorf("ValidName(%q) = true, want false", name)
		}
	}
}
