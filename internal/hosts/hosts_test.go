package hosts

import (
	"bytes"
	"errors"
	"fmt"
	"testing"
)

func TestParseMapsAndLooksUp(t *testing.T) {
	data := []byte(`
# local
10.0.0.5   api.example.com
10.0.0.6   cdn.example.com static.example.com  # trailing comment
::1        v6.example.com

127.0.0.1  Example.COM.
`)
	table, warnings, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings: %+v", warnings)
	}
	want := map[string]string{
		"api.example.com":    "10.0.0.5",
		"cdn.example.com":    "10.0.0.6",
		"static.example.com": "10.0.0.6",
		"v6.example.com":     "::1",
		"example.com":        "127.0.0.1",
	}
	if table.Len() != len(want) {
		t.Fatalf("Len()=%d", table.Len())
	}
	for host, ip := range want {
		got, ok := table.Lookup(host)
		if !ok || got.String() != ip {
			t.Fatalf("%s => %v %v", host, got, ok)
		}
	}
	if _, ok := table.Lookup("missing.example.com"); ok {
		t.Fatal("unexpected hit")
	}
}

func TestParseFirstDuplicateWins(t *testing.T) {
	table, warnings, err := Parse([]byte("10.0.0.5 api.example.com\n10.0.0.6 api.example.com\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := table.Lookup("api.example.com"); got.String() != "10.0.0.5" {
		t.Fatalf("got %s", got)
	}
	if table.Len() != 1 {
		t.Fatalf("Len()=%d", table.Len())
	}
	if len(warnings) != 1 || warnings[0] != (Warning{Domain: "api.example.com", IgnoredLine: 2, FirstLine: 1}) {
		t.Fatalf("warnings: %+v", warnings)
	}
}

func TestParseRejectsInvalid(t *testing.T) {
	cases := []struct {
		name, data string
		line       int
		msg        string
	}{
		{"missing ip", "api.example.com\n", 1, "missing field"},
		{"bad ip", "999.1.1.1 api.example.com\n", 1, "invalid IP"},
		{"bad name", "10.0.0.5 bad_host.example.com\n", 1, "invalid hostname"},
		{"wildcard", "10.0.0.5 *.example.com\n", 1, "invalid hostname"},
		{"port", "10.0.0.5 api.example.com:8443\n", 1, "invalid hostname"},
		{"ip name", "10.0.0.5 10.0.0.6\n", 1, "invalid hostname"},
		{"bad line 3", "\n\n10.0.0.5\n", 3, "missing field"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := Parse([]byte(tc.data))
			var pe *ParseError
			if !errors.As(err, &pe) {
				t.Fatalf("err %v", err)
			}
			if pe.Line != tc.line || pe.Msg != tc.msg {
				t.Fatalf("%+v", pe)
			}
		})
	}
}

func TestParseLimitsAndCRLF(t *testing.T) {
	big := bytes.Repeat([]byte("a"), MaxFileBytes+1)
	if _, _, err := Parse(big); err == nil {
		t.Fatal("size limit not enforced")
	}

	var b bytes.Buffer
	for i := 0; i < MaxMappings+1; i++ {
		fmt.Fprintf(&b, "10.0.0.1 h%d.example.com\r\n", i)
	}
	_, _, err := Parse(b.Bytes())
	var pe *ParseError
	if !errors.As(err, &pe) || pe.Msg != "too many mappings" {
		t.Fatalf("err %v", err)
	}

	table, _, err := Parse([]byte("10.0.0.1 api.example.com\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := table.Lookup("api.example.com"); !ok {
		t.Fatal("CRLF line not parsed")
	}
}
