package config

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
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
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%+v", got)
	}

	got = Effective(file, "10.0.0.1:1", "10.0.0.1:2", 30, 40, Overrides{UI: true, MaxBody: true})
	want = file
	want.UI = "10.0.0.1:2"
	want.MaxBody = 40
	if !reflect.DeepEqual(got, want) {
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
		if !reflect.DeepEqual(gotFile, next) {
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
		if !reflect.DeepEqual(applied.File, gotFile) || !reflect.DeepEqual(applied.Runtime, gotRuntime) {
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
		if !reflect.DeepEqual(gotFile, next) {
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
		if !reflect.DeepEqual(applied.File, gotFile) || !reflect.DeepEqual(applied.Runtime, gotRuntime) {
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
		if !reflect.DeepEqual(gotFile, next) {
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
		if !reflect.DeepEqual(applied.File, gotFile) || !reflect.DeepEqual(applied.Runtime, gotRuntime) {
			t.Fatalf("applied %+v", applied)
		}
	})

	t.Run("persists capture fields to disk and runtime", func(t *testing.T) {
		d, file, err := Open(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		s := NewSession(d, file, Overrides{}, flagProxy, flagUI, flagFlows, flagBody)

		next := file
		next.Capture = false
		next.CaptureEmpty = "none"
		next.CaptureHTTPS = "tunnel"
		next.CaptureHosts = []string{"Example.com"}
		next.Theme = "light"

		wantCapture := Values{
			Capture:      false,
			CaptureEmpty: "none",
			CaptureHTTPS: "tunnel",
			CaptureHosts: []string{"Example.com"},
		}

		if _, err := s.Save(next); err != nil {
			t.Fatal(err)
		}

		gotFile, gotRuntime := s.Snapshot()
		for _, label := range []struct {
			name string
			got  Values
		}{
			{"file", gotFile},
			{"runtime", gotRuntime},
		} {
			got := Values{
				Capture:      label.got.Capture,
				CaptureEmpty: label.got.CaptureEmpty,
				CaptureHTTPS: label.got.CaptureHTTPS,
				CaptureHosts: label.got.CaptureHosts,
			}
			if !reflect.DeepEqual(got, wantCapture) {
				t.Fatalf("%s capture %+v", label.name, got)
			}
			if label.got.CaptureHosts == nil {
				t.Fatalf("%s CaptureHosts nil", label.name)
			}
		}
		if gotFile.Theme != next.Theme {
			t.Fatalf("file theme %s", gotFile.Theme)
		}

		cfg, err := os.ReadFile(filepath.Join(d.Path(), "config.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		fromDisk, err := ParseYAML(cfg)
		if err != nil {
			t.Fatal(err)
		}
		gotDisk := Values{
			Capture:      fromDisk.Capture,
			CaptureEmpty: fromDisk.CaptureEmpty,
			CaptureHTTPS: fromDisk.CaptureHTTPS,
			CaptureHosts: fromDisk.CaptureHosts,
		}
		if !reflect.DeepEqual(gotDisk, wantCapture) {
			t.Fatalf("disk capture %+v", gotDisk)
		}
		if fromDisk.CaptureHosts == nil {
			t.Fatal("disk CaptureHosts nil")
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
		if !reflect.DeepEqual(applied, Applied{}) {
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
		if !reflect.DeepEqual(gotFile, beforeFile) || !reflect.DeepEqual(gotRuntime, beforeRuntime) {
			t.Fatalf("file %+v runtime %+v", gotFile, gotRuntime)
		}
	})
}

func TestSaveAndApplyBlocksASecondSave(t *testing.T) {
	d, file, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := NewSession(d, file, Overrides{}, file.Proxy, file.UI, file.MaxFlows, file.MaxBody)
	first := savedValues(file)
	second := first
	second.Theme = "dark"
	second.MaxFlows = 3

	started := make(chan struct{})
	release := make(chan struct{})
	go func() {
		_, err := s.SaveAndApply(first, func(Applied) {
			close(started)
			<-release
		})
		if err != nil {
			t.Errorf("first save: %v", err)
		}
	}()
	<-started

	done := make(chan struct{})
	go func() {
		if _, err := s.Save(second); err != nil {
			t.Errorf("second save: %v", err)
		}
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("second save ran while the first apply still held the lock")
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("second save did not finish after the first apply released the lock")
	}
	got, _ := s.Snapshot()
	if got.Theme != second.Theme || got.MaxFlows != second.MaxFlows {
		t.Fatalf("file after second save: %+v", got)
	}
}

func TestSetCaptureWaitsForABlockingSave(t *testing.T) {
	d, file, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := NewSession(d, file, Overrides{}, file.Proxy, file.UI, file.MaxFlows, file.MaxBody)
	first := savedValues(file)
	first.CaptureHosts = []string{"new.example"}
	wantCapture := !first.Capture

	started := make(chan struct{})
	release := make(chan struct{})
	go func() {
		_, err := s.SaveAndApply(first, func(Applied) {
			close(started)
			<-release
		})
		if err != nil {
			t.Errorf("first save: %v", err)
		}
	}()
	<-started

	done := make(chan struct{})
	go func() {
		if _, err := s.SetCapture(wantCapture, nil); err != nil {
			t.Errorf("set capture: %v", err)
		}
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("capture save ran while the first apply still held the lock")
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("capture save did not finish after the first apply released the lock")
	}
	gotFile, gotRuntime := s.Snapshot()
	wantHosts := []string{"new.example"}
	if !reflect.DeepEqual(gotFile.CaptureHosts, wantHosts) || !reflect.DeepEqual(gotRuntime.CaptureHosts, wantHosts) {
		t.Fatalf("file hosts %+v runtime hosts %+v", gotFile.CaptureHosts, gotRuntime.CaptureHosts)
	}
	if gotFile.Capture != wantCapture || gotRuntime.Capture != wantCapture {
		t.Fatalf("file capture %v runtime capture %v", gotFile.Capture, gotRuntime.Capture)
	}
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
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("disk %+v", got)
	}
}
