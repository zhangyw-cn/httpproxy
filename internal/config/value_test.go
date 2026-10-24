package config

import (
	"errors"
	"strings"
	"testing"
)

func TestDefaultsAndRoundTrip(t *testing.T) {
	got, err := ParseYAML(MarshalYAML(Defaults()))
	if err != nil {
		t.Fatal(err)
	}
	if got != Defaults() {
		t.Fatalf("%+v", got)
	}
	text := string(MarshalYAML(Defaults()))
	for i, key := range []string{"proxy:", "ui:", "max_flows:", "max_body:", "theme:"} {
		if strings.Index(text, key) < 0 {
			t.Fatalf("missing %s", key)
		}
		if i > 0 && strings.Index(text, key) < strings.Index(text, []string{"proxy:", "ui:", "max_flows:", "max_body:", "theme:"}[i-1]) {
			t.Fatalf("order %s", text)
		}
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
	raw := []byte(`{"proxy":"127.0.0.1:8080","ui":"127.0.0.1:8081","max_flows":2,"max_body":3,"theme":"dark"}`)
	got, err := ParseJSON(raw)
	if err != nil || got.MaxFlows != 2 || got.MaxBody != 3 {
		t.Fatalf("%+v %v", got, err)
	}
	for _, body := range [][]byte{
		[]byte(`{"proxy":"127.0.0.1:8080","ui":"127.0.0.1:8081","max_flows":1.5,"max_body":1,"theme":"dark"}`),
		[]byte(`not json`),
		[]byte(`{"proxy":null,"ui":"127.0.0.1:8081","max_flows":1,"max_body":1,"theme":"dark"}`),
		[]byte(`{"proxy":"127.0.0.1:8080","ui":"127.0.0.1:8081","max_flows":1,"max_body":1,"theme":"dark","x":1}`),
	} {
		if _, err := ParseJSON(body); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
	_, err = ParseJSON([]byte(`{"ui":"127.0.0.1:8081","max_flows":1,"max_body":1,"theme":"dark"}`))
	if !errors.Is(err, ErrMissingField) {
		t.Fatal(err)
	}
	_, err = ParseJSON([]byte(`{"proxy":null,"ui":"nope","max_flows":1,"max_body":1,"theme":"dark"}`))
	if !errors.Is(err, ErrUIAddress) {
		t.Fatalf("present UI error wins over a missing proxy, got %v", err)
	}
}
