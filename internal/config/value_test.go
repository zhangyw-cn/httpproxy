package config

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestDefaultsAndRoundTrip(t *testing.T) {
	got, err := ParseYAML(MarshalYAML(Defaults()))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, Defaults()) {
		t.Fatalf("%+v", got)
	}
	text := string(MarshalYAML(Defaults()))
	for i, key := range []string{"proxy:", "ui:", "max_flows:", "max_body:", "theme:", "capture:", "capture_empty:", "capture_https:", "capture_hosts:", "hosts_profile:"} {
		if strings.Index(text, key) < 0 {
			t.Fatalf("missing %s", key)
		}
		if i > 0 && strings.Index(text, key) < strings.Index(text, []string{"proxy:", "ui:", "max_flows:", "max_body:", "theme:", "capture:", "capture_empty:", "capture_https:", "capture_hosts:", "hosts_profile:"}[i-1]) {
			t.Fatalf("order %s", text)
		}
	}
}

func TestDefaultsCaptureOffAndHostsProfileEmpty(t *testing.T) {
	got := Defaults()
	if got.Capture {
		t.Fatal("capture should default to false")
	}
	if got.HostsProfile != "" {
		t.Fatalf("hosts_profile = %q", got.HostsProfile)
	}
	text := string(MarshalYAML(got))
	if !strings.Contains(text, "hosts_profile: \"\"\n") {
		t.Fatalf("%s", text)
	}
}

func TestParseYAMLDefaultsHostsProfile(t *testing.T) {
	raw := []byte("proxy: \"127.0.0.1:8080\"\nui: \"127.0.0.1:8081\"\nmax_flows: 1\nmax_body: 1\ntheme: dark\n")
	got, err := ParseYAML(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.HostsProfile != "" {
		t.Fatalf("%q", got.HostsProfile)
	}
	if got.Capture {
		t.Fatal("capture default")
	}
}

func TestParseJSONRequiresAndReadsHostsProfile(t *testing.T) {
	base := `{"proxy":"127.0.0.1:8080","ui":"127.0.0.1:8081","max_flows":1,"max_body":1,"theme":"dark","capture":false,"capture_empty":"all","capture_https":"decrypt","capture_hosts":[],"hosts_profile":"测试"}`
	got, err := ParseJSON([]byte(base))
	if err != nil || got.HostsProfile != "测试" {
		t.Fatalf("%+v %v", got, err)
	}
	missing := strings.Replace(base, `"hosts_profile":"测试"`, `"hosts_profile":null`, 1)
	if _, err := ParseJSON([]byte(missing)); !errors.Is(err, ErrMissingField) {
		t.Fatalf("%v", err)
	}
}

func TestParseYAMLAcceptsListenForms(t *testing.T) {
	raw := []byte("proxy: \":8080\"\nui: \"[::1]:8081\"\nmax_flows: 1\nmax_body: 0\ntheme: light\n")
	got, err := ParseYAML(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Proxy != ":8080" || got.UI != "[::1]:8081" || got.MaxFlows != 1 || got.MaxBody != 0 || got.Theme != "light" {
		t.Fatalf("%+v", got)
	}
}

func TestParseYAMLRejectsShape(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want error
	}{
		{"float", "proxy: 127.0.0.1:8080\nui: 127.0.0.1:8081\nmax_flows: 1.5\nmax_body: 1\ntheme: dark\n", ErrMaxFlows},
		{"quoted count", "proxy: 127.0.0.1:8080\nui: 127.0.0.1:8081\nmax_flows: \"1\"\nmax_body: 1\ntheme: dark\n", ErrMaxFlows},
		{"unknown", "proxy: 127.0.0.1:8080\nui: 127.0.0.1:8081\nmax_flows: 1\nmax_body: 1\ntheme: dark\nextra: 1\n", ErrUnknownField},
		{"missing", "proxy: 127.0.0.1:8080\nui: 127.0.0.1:8081\nmax_flows: 1\nmax_body: 1\n", ErrMissingField},
		{"bad proxy", "proxy: nope\nui: 127.0.0.1:8081\nmax_flows: 1\nmax_body: 1\ntheme: dark\n", ErrProxyAddress},
		{"bad ui", "proxy: 127.0.0.1:8080\nui: nope\nmax_flows: 1\nmax_body: 1\ntheme: dark\n", ErrUIAddress},
		{"zero flows", "proxy: 127.0.0.1:8080\nui: 127.0.0.1:8081\nmax_flows: 0\nmax_body: 1\ntheme: dark\n", ErrMaxFlows},
		{"negative body", "proxy: 127.0.0.1:8080\nui: 127.0.0.1:8081\nmax_flows: 1\nmax_body: -1\ntheme: dark\n", ErrMaxBody},
		{"theme", "proxy: 127.0.0.1:8080\nui: 127.0.0.1:8081\nmax_flows: 1\nmax_body: 1\ntheme: blue\n", ErrTheme},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseYAML([]byte(tc.yaml))
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestParseJSON(t *testing.T) {
	raw := []byte(`{"proxy":"127.0.0.1:8080","ui":"127.0.0.1:8081","max_flows":2,"max_body":3,"theme":"dark","capture":true,"capture_empty":"all","capture_https":"decrypt","capture_hosts":[],"hosts_profile":""}`)
	got, err := ParseJSON(raw)
	if err != nil || got.MaxFlows != 2 || got.MaxBody != 3 {
		t.Fatalf("%+v %v", got, err)
	}
	for _, body := range [][]byte{
		[]byte(`{"proxy":"127.0.0.1:8080","ui":"127.0.0.1:8081","max_flows":1.5,"max_body":1,"theme":"dark","capture":true,"capture_empty":"all","capture_https":"decrypt","capture_hosts":[],"hosts_profile":""}`),
		[]byte(`not json`),
		[]byte(`{"proxy":null,"ui":"127.0.0.1:8081","max_flows":1,"max_body":1,"theme":"dark","capture":true,"capture_empty":"all","capture_https":"decrypt","capture_hosts":[],"hosts_profile":""}`),
		[]byte(`{"proxy":"127.0.0.1:8080","ui":"127.0.0.1:8081","max_flows":1,"max_body":1,"theme":"dark","x":1,"capture":true,"capture_empty":"all","capture_https":"decrypt","capture_hosts":[],"hosts_profile":""}`),
	} {
		if _, err := ParseJSON(body); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
	_, err = ParseJSON([]byte(`{"ui":"127.0.0.1:8081","max_flows":1,"max_body":1,"theme":"dark"}`))
	if !errors.Is(err, ErrMissingField) {
		t.Fatal(err)
	}
	_, err = ParseJSON([]byte(`{"proxy":null,"ui":"nope","max_flows":1,"max_body":1,"theme":"dark","capture":true,"capture_empty":"all","capture_https":"decrypt","capture_hosts":[],"hosts_profile":""}`))
	if !errors.Is(err, ErrUIAddress) {
		t.Fatalf("present UI error wins over a missing proxy, got %v", err)
	}
}

func TestParseJSONRejectsTrailingBytes(t *testing.T) {
	valid := `{"proxy":"127.0.0.1:8080","ui":"127.0.0.1:8081","max_flows":1,"max_body":1,"theme":"dark","capture":true,"capture_empty":"all","capture_https":"decrypt","capture_hosts":[],"hosts_profile":""}`
	for _, suffix := range []string{"]", "}{", valid} {
		_, err := ParseJSON([]byte(valid + suffix))
		if !errors.Is(err, ErrJSON) {
			t.Fatalf("suffix %q: got %v", suffix, err)
		}
	}
}

func TestParseYAMLDefaultsCaptureFields(t *testing.T) {
	raw := []byte("proxy: \"127.0.0.1:8080\"\nui: \"127.0.0.1:8081\"\nmax_flows: 1000\nmax_body: 1048576\ntheme: dark\n")
	got, err := ParseYAML(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Capture || got.CaptureEmpty != "all" || got.CaptureHTTPS != "decrypt" || len(got.CaptureHosts) != 0 || got.CaptureHosts == nil {
		t.Fatalf("%+v", got)
	}
}

func TestParseYAMLRejectsBadCapture(t *testing.T) {
	base := "proxy: \"127.0.0.1:8080\"\nui: \"127.0.0.1:8081\"\nmax_flows: 1\nmax_body: 1\ntheme: dark\n"
	cases := []struct {
		extra string
		want  error
	}{
		{"capture: \"true\"\ncapture_empty: all\ncapture_https: decrypt\ncapture_hosts: []\n", ErrCapture},
		{"capture: true\ncapture_empty: maybe\ncapture_https: decrypt\ncapture_hosts: []\n", ErrCaptureEmpty},
		{"capture: true\ncapture_empty: all\ncapture_https: pass\ncapture_hosts: []\n", ErrCaptureHTTPS},
		{"capture: true\ncapture_empty: all\ncapture_https: decrypt\ncapture_hosts: example.com\n", ErrCaptureHosts},
		{"capture: true\ncapture_empty: all\ncapture_https: decrypt\ncapture_hosts: [\"*.example.com\", \"*.Example.com\"]\n", ErrCaptureHostDup},
	}
	for _, tc := range cases {
		_, err := ParseYAML([]byte(base + tc.extra))
		if !errors.Is(err, tc.want) {
			t.Fatalf("extra %q err %v", tc.extra, err)
		}
	}
}

func TestParseJSONRequiresCaptureFields(t *testing.T) {
	_, err := ParseJSON([]byte(`{"proxy":"127.0.0.1:8080","ui":"127.0.0.1:8081","max_flows":1,"max_body":1,"theme":"dark"}`))
	if !errors.Is(err, ErrMissingField) {
		t.Fatal(err)
	}
	raw := []byte(`{"proxy":"127.0.0.1:8080","ui":"127.0.0.1:8081","max_flows":1,"max_body":1,"theme":"dark","capture":true,"capture_empty":"all","capture_https":"decrypt","capture_hosts":["Example.com"],"hosts_profile":""}`)
	got, err := ParseJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Capture || got.CaptureHosts[0] != "Example.com" {
		t.Fatalf("%+v", got)
	}
	text := string(MarshalYAML(got))
	if !strings.Contains(text, "capture: true\n") || !strings.Contains(text, "capture_hosts:\n") || !strings.Contains(text, "\"Example.com\"") {
		t.Fatalf("%s", text)
	}
}
