package capture

import (
	"errors"
	"net"
	"strconv"
	"strings"
	"unicode"
)

type Action int

const (
	Record Action = iota
	DecryptOnly
	Tunnel
)

var (
	ErrCapture           = errors.New("capture must be a boolean")
	ErrCaptureEmpty      = errors.New("capture_empty must be all or none")
	ErrCaptureHTTPS      = errors.New("capture_https must be decrypt or tunnel")
	ErrCaptureHosts      = errors.New("capture_hosts must be a list of strings")
	ErrCaptureHostsLimit = errors.New("capture_hosts must contain at most 100 rules")
	ErrCaptureHostLength = errors.New("a capture host rule must be at most 253 characters")
	ErrCaptureHost       = errors.New("invalid capture host rule")
	ErrCaptureHostDup    = errors.New("duplicate capture host rule")
)

type rule struct {
	wildcard bool
	host     string
	ip       net.IP
	ports    [2]int // [0] set; [1] is 0 when only ports[0] counts. 0,0 means 80 and 443.
	anyPort  bool
}

type Policy struct {
	capture  bool
	emptyAll bool
	tunnel   bool
	rules    []rule
}

func Default() Policy {
	return Policy{capture: true, emptyAll: true}
}

func Compile(capture bool, empty, httpsMode string, hosts []string) (Policy, error) {
	if empty != "all" && empty != "none" {
		return Policy{}, ErrCaptureEmpty
	}
	if httpsMode != "decrypt" && httpsMode != "tunnel" {
		return Policy{}, ErrCaptureHTTPS
	}
	if len(hosts) > 100 {
		return Policy{}, ErrCaptureHostsLimit
	}
	rules := make([]rule, 0, len(hosts))
	seen := make(map[string]struct{}, len(hosts))
	for _, raw := range hosts {
		trimmed := strings.TrimSpace(raw)
		if len(trimmed) > 253 {
			return Policy{}, ErrCaptureHostLength
		}
		parsed, key, err := parseRule(trimmed)
		if err != nil {
			return Policy{}, err
		}
		if _, ok := seen[key]; ok {
			return Policy{}, ErrCaptureHostDup
		}
		seen[key] = struct{}{}
		rules = append(rules, parsed)
	}
	return Policy{
		capture:  capture,
		emptyAll: empty == "all",
		tunnel:   httpsMode == "tunnel",
		rules:    rules,
	}, nil
}

func (p Policy) Decide(https bool, authority string) Action {
	if p.records(https, authority) {
		return Record
	}
	if https && p.tunnel {
		return Tunnel
	}
	return DecryptOnly
}

func (p Policy) records(https bool, authority string) bool {
	if !p.capture {
		return false
	}
	if len(p.rules) == 0 {
		return p.emptyAll
	}
	host, port := splitAuthority(https, authority)
	for _, item := range p.rules {
		if item.matches(host, port) {
			return true
		}
	}
	return false
}

func splitAuthority(https bool, authority string) (string, int) {
	port := 80
	if https {
		port = 443
	}
	if host, text, err := net.SplitHostPort(authority); err == nil {
		if n, err := strconv.Atoi(text); err == nil {
			return host, n
		}
		return host, port
	}
	host := authority
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = host[1 : len(host)-1]
	}
	return host, port
}

func parseRule(s string) (rule, string, error) {
	for _, r := range s {
		if r > unicode.MaxASCII || unicode.IsSpace(r) {
			return rule{}, "", ErrCaptureHost
		}
	}
	if strings.Contains(s, "://") || strings.Contains(s, "/") || strings.Contains(s, "@") || (strings.Contains(s, "*") && !strings.HasPrefix(s, "*.")) {
		return rule{}, "", ErrCaptureHost
	}
	wildcard := false
	rest := s
	if strings.HasPrefix(s, "*.") {
		wildcard = true
		rest = s[2:]
		if strings.Contains(rest, "*") {
			return rule{}, "", ErrCaptureHost
		}
	}
	host, portText, err := splitPattern(rest)
	if err != nil {
		return rule{}, "", err
	}
	parsed := rule{wildcard: wildcard, anyPort: portText == ""}
	if portText == "" {
		parsed.ports = [2]int{80, 443}
	} else {
		n, err := strconv.Atoi(portText)
		if err != nil || n < 1 || n > 65535 || portText != strconv.Itoa(n) {
			return rule{}, "", ErrCaptureHost
		}
		parsed.ports = [2]int{n, 0}
	}
	if ip := net.ParseIP(host); ip != nil {
		if wildcard {
			return rule{}, "", ErrCaptureHost
		}
		parsed.ip = ip
		parsed.host = ip.String()
	} else {
		if !validDNS(host) {
			return rule{}, "", ErrCaptureHost
		}
		parsed.host = strings.ToLower(host)
	}
	key := "exact"
	if wildcard {
		key = "wild"
	}
	key += " " + parsed.host + " " + strconv.Itoa(parsed.ports[0]) + " " + strconv.Itoa(parsed.ports[1])
	return parsed, key, nil
}

func splitPattern(s string) (string, string, error) {
	if strings.HasPrefix(s, "[") {
		end := strings.IndexByte(s, ']')
		if end <= 1 {
			return "", "", ErrCaptureHost
		}
		host := s[1:end]
		tail := s[end+1:]
		if tail == "" {
			return host, "", nil
		}
		if len(tail) < 2 || tail[0] != ':' {
			return "", "", ErrCaptureHost
		}
		return host, tail[1:], nil
	}
	if strings.Count(s, ":") > 1 {
		return "", "", ErrCaptureHost
	}
	host, port, ok := strings.Cut(s, ":")
	if !ok {
		return s, "", nil
	}
	return host, port, nil
}

func validDNS(host string) bool {
	if host == "" || strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			switch {
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
			default:
				return false
			}
		}
	}
	return true
}

func (item rule) matches(host string, port int) bool {
	if item.anyPort {
		if port != 80 && port != 443 {
			return false
		}
	} else if port != item.ports[0] {
		return false
	}
	if item.ip != nil {
		return item.ip.Equal(net.ParseIP(host))
	}
	host = strings.ToLower(host)
	if item.wildcard {
		return host != item.host && strings.HasSuffix(host, "."+item.host)
	}
	return host == item.host
}
