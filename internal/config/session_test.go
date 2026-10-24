package config

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestEffective(t *testing.T) {
	file := Values{
		Proxy:    "127.0.0.1:9000",
		UI:       "127.0.0.1:9001",
		MaxFlows: 10,
		MaxBody:  20,
		Theme:    "light",
	}
	flags := Overrides{Proxy: true, MaxFlows: true}
	got := Effective(file, "10.0.0.1:1", "10.0.0.1:2", 30, 40, flags)
	want := file
	want.Proxy = "10.0.0.1:1"
	want.MaxFlows = 30
	if got != want {
		t.Fatalf("%+v", got)
	}

	got = Effective(file, "10.0.0.1:1", "10.0.0.1:2", 30, 40, Overrides{UI: true, MaxBody: true})
	want = file
	want.UI = "10.0.0.1:2"
	want.MaxBody = 40
	if got != want {
		t.Fatalf("%+v", got)
	}
}

func TestSession(t *testing.T) {
	const (
		flagProxy = "127.0.0.1:1"
		flagUI    = "127.0.0.1:2"
		flagFlows = 50
		flagBody  = int64(64)
	)

	t.Run("applies both limits when neither is overridden", func(t *testing.T) {
		d, file, err := Open(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		s := NewSession(d, file, Overrides{Proxy: true}, flagProxy, flagUI, flagFlows, flagBody)
		next := savedValues(file)
		applied, err := s.Save(next)
		if err != nil {
			t.Fatal(err)
		}
		assertConfigFile(t, d, next)

		gotFile, gotRuntime := s.Snapshot()
		if gotFile != next {
			t.Fatalf("file %+v", gotFile)
		}
		if gotRuntime.Proxy != flagProxy || gotRuntime.UI != file.UI {
			t.Fatalf("listen %+v", gotRuntime)
		}
		if !applied.SetMaxFlows || applied.MaxFlows != next.MaxFlows || gotRuntime.MaxFlows != next.MaxFlows {
			t.Fatalf("flows applied=%+v runtime=%+v", applied, gotRuntime)
		}
		if !applied.SetMaxBody || applied.MaxBody != next.MaxBody || gotRuntime.MaxBody != next.MaxBody {
			t.Fatalf("body applied=%+v runtime=%+v", applied, gotRuntime)
		}
		if gotRuntime.Theme != next.Theme {
			t.Fatalf("theme %s", gotRuntime.Theme)
		}
		if applied.File != gotFile || applied.Runtime != gotRuntime {
			t.Fatalf("applied %+v", applied)
		}
	})

	t.Run("applies limits that are not overridden", func(t *testing.T) {
		d, file, err := Open(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		s := NewSession(d, file, Overrides{MaxBody: true}, flagProxy, flagUI, flagFlows, flagBody)
		_, runtime := s.Snapshot()
		if runtime.Proxy != file.Proxy || runtime.UI != file.UI || runtime.MaxFlows != file.MaxFlows || runtime.MaxBody != flagBody || runtime.Theme != file.Theme {
			t.Fatalf("runtime %+v", runtime)
		}

		next := savedValues(file)
		applied, err := s.Save(next)
		if err != nil {
			t.Fatal(err)
		}
		assertConfigFile(t, d, next)

		gotFile, gotRuntime := s.Snapshot()
		if gotFile != next {
			t.Fatalf("file %+v", gotFile)
		}
		if gotRuntime.Proxy != file.Proxy || gotRuntime.UI != file.UI {
			t.Fatalf("listen %+v", gotRuntime)
		}
		if !applied.SetMaxFlows || applied.MaxFlows != next.MaxFlows || gotRuntime.MaxFlows != next.MaxFlows {
			t.Fatalf("flows applied=%+v runtime=%+v", applied, gotRuntime)
		}
		if applied.SetMaxBody || applied.MaxBody != 0 || gotRuntime.MaxBody != flagBody {
			t.Fatalf("body applied=%+v runtime=%+v", applied, gotRuntime)
		}
		if gotRuntime.Theme != next.Theme {
			t.Fatalf("theme %s", gotRuntime.Theme)
		}
		if applied.File != gotFile || applied.Runtime != gotRuntime {
			t.Fatalf("applied %+v", applied)
		}
	})

	t.Run("keeps overridden limits", func(t *testing.T) {
		d, file, err := Open(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		ov := Overrides{Proxy: true, UI: true, MaxFlows: true}
		s := NewSession(d, file, ov, flagProxy, flagUI, flagFlows, flagBody)
		_, runtime := s.Snapshot()
		if runtime.Proxy != flagProxy || runtime.UI != flagUI || runtime.MaxFlows != flagFlows || runtime.MaxBody != file.MaxBody || runtime.Theme != file.Theme {
			t.Fatalf("runtime %+v", runtime)
		}

		next := savedValues(file)
		applied, err := s.Save(next)
		if err != nil {
			t.Fatal(err)
		}
		assertConfigFile(t, d, next)

		gotFile, gotRuntime := s.Snapshot()
		if gotFile != next {
			t.Fatalf("file %+v", gotFile)
		}
		if gotRuntime.Proxy != flagProxy || gotRuntime.UI != flagUI {
			t.Fatalf("listen %+v", gotRuntime)
		}
		if applied.SetMaxFlows || applied.MaxFlows != 0 || gotRuntime.MaxFlows != flagFlows {
			t.Fatalf("flows applied=%+v runtime=%+v", applied, gotRuntime)
		}
		if !applied.SetMaxBody || applied.MaxBody != next.MaxBody || gotRuntime.MaxBody != next.MaxBody {
			t.Fatalf("body applied=%+v runtime=%+v", applied, gotRuntime)
		}
		if gotRuntime.Theme != next.Theme {
			t.Fatalf("theme %s", gotRuntime.Theme)
		}
		if applied.File != gotFile || applied.Runtime != gotRuntime {
			t.Fatalf("applied %+v", applied)
		}
	})

	t.Run("rejects illegal theme", func(t *testing.T) {
		d, file, err := Open(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		s := NewSession(d, file, Overrides{}, flagProxy, flagUI, flagFlows, flagBody)
		beforeFile, beforeRuntime := s.Snapshot()
		path := filepath.Join(d.Path(), "config.yaml")
		before, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}

		bad := file
		bad.Theme = "blue"
		applied, err := s.Save(bad)
		if !errors.Is(err, ErrTheme) {
			t.Fatalf("got %v", err)
		}
		if applied != (Applied{}) {
			t.Fatalf("applied %+v", applied)
		}
		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(after, before) {
			t.Fatalf("bytes %q", after)
		}
		gotFile, gotRuntime := s.Snapshot()
		if gotFile != beforeFile || gotRuntime != beforeRuntime {
			t.Fatalf("file %+v runtime %+v", gotFile, gotRuntime)
		}
	})
}

func savedValues(file Values) Values {
	next := file
	next.Proxy = "127.0.0.1:9090"
	next.UI = "127.0.0.1:9091"
	next.MaxFlows = 2
	next.MaxBody = 8
	next.Theme = "light"
	return next
}

func assertConfigFile(t *testing.T, d *Dir, want Values) {
	t.Helper()
	cfg, err := os.ReadFile(filepath.Join(d.Path(), "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseYAML(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("disk %+v", got)
	}
}
