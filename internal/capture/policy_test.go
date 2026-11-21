package capture

import (
	"errors"
	"strconv"
	"strings"
	"testing"
)

func TestDecideRecordsByDefault(t *testing.T) {
	if got := Default().Decide(true, "example.com:443"); got != Record {
		t.Fatalf("action %d", got)
	}
	if got := Default().Decide(false, "example.com"); got != Record {
		t.Fatalf("plain %d", got)
	}
}

func TestDecideHostRules(t *testing.T) {
	p, err := Compile(true, "all", "decrypt", []string{
		" Example.COM ",
		"*.Example.com",
		"example.com:8443",
		"[::1]",
	})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		https bool
		host  string
		want  Action
	}{
		{true, "example.com:443", Record},
		{true, "example.com:80", Record},
		{true, "example.com:8443", Record},
		{true, "example.com:8444", DecryptOnly},
		{true, "api.example.com:443", Record},
		{true, "a.b.example.com:443", Record},
		{true, "notexample.com:443", DecryptOnly},
		{false, "example.com", Record},
		{false, "example.com:443", Record},
		{false, "example.com:8444", DecryptOnly},
		{true, "[::1]:443", Record},
		{true, "[::1]:8443", DecryptOnly},
		{true, "[0:0:0:0:0:0:0:1]:80", Record},
	}
	for _, tc := range cases {
		if got := p.Decide(tc.https, tc.host); got != tc.want {
			t.Fatalf("Decide(%v, %s) = %d, want %d", tc.https, tc.host, got, tc.want)
		}
	}
}

func TestDecideEmptyListAndSwitch(t *testing.T) {
	all, err := Compile(true, "all", "tunnel", nil)
	if err != nil {
		t.Fatal(err)
	}
	if all.Decide(true, "a.test:443") != Record {
		t.Fatal("empty all should record")
	}
	none, err := Compile(true, "none", "tunnel", []string{})
	if err != nil {
		t.Fatal(err)
	}
	if none.Decide(true, "a.test:443") != Tunnel {
		t.Fatal("empty none https")
	}
	if none.Decide(false, "a.test") != DecryptOnly {
		t.Fatal("plain http must not tunnel")
	}
	off, err := Compile(false, "all", "decrypt", []string{"example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if off.Decide(true, "example.com:443") != Tunnel {
		t.Fatal("capture off must tunnel HTTPS")
	}
	if off.Decide(false, "example.com") != DecryptOnly {
		t.Fatal("plain HTTP still forwards without recording")
	}
	offTunnel, err := Compile(false, "all", "tunnel", nil)
	if err != nil {
		t.Fatal(err)
	}
	if offTunnel.Decide(true, "example.com:443") != Tunnel {
		t.Fatal("capture off ignores capture_https")
	}
}

func TestCompileRejectsRules(t *testing.T) {
	long := strings.Repeat("a", 254)
	many := make([]string, 101)
	for i := range many {
		many[i] = "h" + strconv.Itoa(i) + ".example.com"
	}
	cases := []struct {
		empty string
		https string
		hosts []string
		want  error
	}{
		{"all", "nope", nil, ErrCaptureHTTPS},
		{"nope", "decrypt", nil, ErrCaptureEmpty},
		{"all", "decrypt", []string{"https://example.com"}, ErrCaptureHost},
		{"all", "decrypt", []string{"example.com/a"}, ErrCaptureHost},
		{"all", "decrypt", []string{"*"}, ErrCaptureHost},
		{"all", "decrypt", []string{"*.[::1]"}, ErrCaptureHost},
		{"all", "decrypt", []string{"[example.com]"}, ErrCaptureHost},
		{"all", "decrypt", []string{"[example.com]:443"}, ErrCaptureHost},
		{"all", "decrypt", []string{"*.[example.com]"}, ErrCaptureHost},
		{"all", "decrypt", []string{"example.com:0"}, ErrCaptureHost},
		{"all", "decrypt", []string{"example.com:65536"}, ErrCaptureHost},
		{"all", "decrypt", []string{".example.com"}, ErrCaptureHost},
		{"all", "decrypt", []string{"ex ample.com"}, ErrCaptureHost},
		{"all", "decrypt", []string{"例.com"}, ErrCaptureHost},
		{"all", "decrypt", []string{long}, ErrCaptureHostLength},
		{"all", "decrypt", many, ErrCaptureHostsLimit},
		{"all", "decrypt", []string{"Example.com", "example.com"}, ErrCaptureHostDup},
		{"all", "decrypt", []string{"*.A.com", "*.a.com"}, ErrCaptureHostDup},
		{"all", "decrypt", []string{"example.com:"}, ErrCaptureHost},
		{"all", "decrypt", []string{"*.example.com:"}, ErrCaptureHost},
		{"all", "decrypt", []string{"[::1]:"}, ErrCaptureHost},
	}
	for _, tc := range cases {
		_, err := Compile(true, tc.empty, tc.https, tc.hosts)
		if !errors.Is(err, tc.want) {
			t.Fatalf("Compile(%q, %q, %v) err %v, want %v", tc.empty, tc.https, tc.hosts, err, tc.want)
		}
	}
	if _, err := Compile(true, "all", "decrypt", []string{"example.com", "example.com:443"}); err != nil {
		t.Fatal(err)
	}
}
