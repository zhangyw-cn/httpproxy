package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"

	"httpproxy/internal/capture"

	"gopkg.in/yaml.v3"
)

type Values struct {
	Proxy        string   `json:"proxy" yaml:"proxy"`
	UI           string   `json:"ui" yaml:"ui"`
	MaxFlows     int      `json:"max_flows" yaml:"max_flows"`
	MaxBody      int64    `json:"max_body" yaml:"max_body"`
	Theme        string   `json:"theme" yaml:"theme"`
	Capture      bool     `json:"capture" yaml:"capture"`
	CaptureEmpty string   `json:"capture_empty" yaml:"capture_empty"`
	CaptureHTTPS string   `json:"capture_https" yaml:"capture_https"`
	CaptureHosts []string `json:"capture_hosts" yaml:"capture_hosts"`
	HostsProfile string   `json:"hosts_profile" yaml:"hosts_profile"`
}

var (
	ErrProxyAddress      = errors.New("invalid proxy address")
	ErrUIAddress         = errors.New("invalid ui address")
	ErrMaxFlows          = errors.New("max_flows must be an integer >= 1")
	ErrMaxBody           = errors.New("max_body must be an integer >= 0")
	ErrTheme             = errors.New("theme must be dark or light")
	ErrMissingField      = errors.New("missing config field")
	ErrUnknownField      = errors.New("unknown config field")
	ErrJSON              = errors.New("invalid config JSON")
	ErrCapture           = capture.ErrCapture
	ErrCaptureEmpty      = capture.ErrCaptureEmpty
	ErrCaptureHTTPS      = capture.ErrCaptureHTTPS
	ErrCaptureHosts      = capture.ErrCaptureHosts
	ErrCaptureHostsLimit = capture.ErrCaptureHostsLimit
	ErrCaptureHostLength = capture.ErrCaptureHostLength
	ErrCaptureHost       = capture.ErrCaptureHost
	ErrCaptureHostDup    = capture.ErrCaptureHostDup
	ErrHostsProfile      = errors.New("hosts_profile must be a string")
)

var knownFields = map[string]struct{}{
	"proxy":         {},
	"ui":            {},
	"max_flows":     {},
	"max_body":      {},
	"theme":         {},
	"capture":       {},
	"capture_empty": {},
	"capture_https": {},
	"capture_hosts": {},
	"hosts_profile": {},
}

func Defaults() Values {
	return Values{
		Proxy:        "127.0.0.1:8080",
		UI:           "127.0.0.1:8081",
		MaxFlows:     1000,
		MaxBody:      1048576,
		Theme:        "dark",
		Capture:      false,
		CaptureEmpty: "all",
		CaptureHTTPS: "decrypt",
		CaptureHosts: []string{},
		HostsProfile: "",
	}
}

func ParseYAML(data []byte) (Values, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return Values{}, err
	}
	doc := unwrapYAMLDocument(&root)
	if doc.Kind != yaml.MappingNode {
		return Values{}, ErrMissingField
	}

	fields := make(map[string]*yaml.Node)
	unknown := false
	for i := 0; i < len(doc.Content); i += 2 {
		key := doc.Content[i].Value
		val := doc.Content[i+1]
		if _, ok := knownFields[key]; ok {
			fields[key] = val
		} else {
			unknown = true
		}
	}

	var out Values
	out.Capture = false
	out.CaptureEmpty = "all"
	out.CaptureHTTPS = "decrypt"
	out.CaptureHosts = []string{}

	if n, ok := fields["proxy"]; ok && !isYAMLNull(n) {
		s, err := yamlString(n)
		if err != nil {
			return Values{}, ErrProxyAddress
		}
		if _, _, err := net.SplitHostPort(s); err != nil {
			return Values{}, ErrProxyAddress
		}
		out.Proxy = s
	}
	if n, ok := fields["ui"]; ok && !isYAMLNull(n) {
		s, err := yamlString(n)
		if err != nil {
			return Values{}, ErrUIAddress
		}
		if _, _, err := net.SplitHostPort(s); err != nil {
			return Values{}, ErrUIAddress
		}
		out.UI = s
	}
	if n, ok := fields["max_flows"]; ok && !isYAMLNull(n) {
		v, err := yamlInt(n, strconv.IntSize)
		if err != nil {
			return Values{}, ErrMaxFlows
		}
		if v < 1 {
			return Values{}, ErrMaxFlows
		}
		out.MaxFlows = int(v)
	}
	if n, ok := fields["max_body"]; ok && !isYAMLNull(n) {
		v, err := yamlInt(n, 64)
		if err != nil {
			return Values{}, ErrMaxBody
		}
		if v < 0 {
			return Values{}, ErrMaxBody
		}
		out.MaxBody = v
	}
	if n, ok := fields["theme"]; ok && !isYAMLNull(n) {
		s, err := yamlString(n)
		if err != nil || (s != "dark" && s != "light") {
			return Values{}, ErrTheme
		}
		out.Theme = s
	}
	if n, ok := fields["capture"]; ok && !isYAMLNull(n) {
		b, err := yamlBool(n)
		if err != nil {
			return Values{}, ErrCapture
		}
		out.Capture = b
	}
	if n, ok := fields["capture_empty"]; ok && !isYAMLNull(n) {
		s, err := yamlString(n)
		if err != nil {
			return Values{}, ErrCaptureEmpty
		}
		out.CaptureEmpty = s
	}
	if n, ok := fields["capture_https"]; ok && !isYAMLNull(n) {
		s, err := yamlString(n)
		if err != nil {
			return Values{}, ErrCaptureHTTPS
		}
		out.CaptureHTTPS = s
	}
	if n, ok := fields["capture_hosts"]; ok && !isYAMLNull(n) {
		hosts, err := yamlHostList(n)
		if err != nil {
			return Values{}, err
		}
		out.CaptureHosts = hosts
	}
	if n, ok := fields["hosts_profile"]; ok && !isYAMLNull(n) {
		s, err := yamlString(n)
		if err != nil {
			return Values{}, ErrHostsProfile
		}
		out.HostsProfile = s
	}
	if _, err := capture.Compile(out.Capture, out.CaptureEmpty, out.CaptureHTTPS, out.CaptureHosts); err != nil {
		return Values{}, err
	}

	for _, key := range []string{"proxy", "ui", "max_flows", "max_body", "theme"} {
		n, ok := fields[key]
		if !ok || isYAMLNull(n) {
			return Values{}, ErrMissingField
		}
	}
	if unknown {
		return Values{}, ErrUnknownField
	}
	return out, nil
}

func ParseJSON(data []byte) (Values, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	var raw map[string]json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		return Values{}, ErrJSON
	}
	var trailing json.RawMessage
	if err := dec.Decode(&trailing); err != io.EOF {
		return Values{}, ErrJSON
	}

	var out Values
	out.Capture = false
	out.CaptureEmpty = "all"
	out.CaptureHTTPS = "decrypt"
	out.CaptureHosts = []string{}

	if msg, ok := raw["proxy"]; ok && !jsonNull(msg) {
		s, err := jsonString(msg)
		if err != nil {
			return Values{}, ErrProxyAddress
		}
		if _, _, err := net.SplitHostPort(s); err != nil {
			return Values{}, ErrProxyAddress
		}
		out.Proxy = s
	}
	if msg, ok := raw["ui"]; ok && !jsonNull(msg) {
		s, err := jsonString(msg)
		if err != nil {
			return Values{}, ErrUIAddress
		}
		if _, _, err := net.SplitHostPort(s); err != nil {
			return Values{}, ErrUIAddress
		}
		out.UI = s
	}
	if msg, ok := raw["max_flows"]; ok && !jsonNull(msg) {
		v, err := jsonInt(msg, strconv.IntSize)
		if err != nil {
			return Values{}, ErrMaxFlows
		}
		if v < 1 {
			return Values{}, ErrMaxFlows
		}
		out.MaxFlows = int(v)
	}
	if msg, ok := raw["max_body"]; ok && !jsonNull(msg) {
		v, err := jsonInt(msg, 64)
		if err != nil {
			return Values{}, ErrMaxBody
		}
		if v < 0 {
			return Values{}, ErrMaxBody
		}
		out.MaxBody = v
	}
	if msg, ok := raw["theme"]; ok && !jsonNull(msg) {
		s, err := jsonString(msg)
		if err != nil || (s != "dark" && s != "light") {
			return Values{}, ErrTheme
		}
		out.Theme = s
	}
	if msg, ok := raw["capture"]; ok && !jsonNull(msg) {
		var b bool
		if err := json.Unmarshal(msg, &b); err != nil {
			return Values{}, ErrCapture
		}
		out.Capture = b
	}
	if msg, ok := raw["capture_empty"]; ok && !jsonNull(msg) {
		s, err := jsonString(msg)
		if err != nil {
			return Values{}, ErrCaptureEmpty
		}
		out.CaptureEmpty = s
	}
	if msg, ok := raw["capture_https"]; ok && !jsonNull(msg) {
		s, err := jsonString(msg)
		if err != nil {
			return Values{}, ErrCaptureHTTPS
		}
		out.CaptureHTTPS = s
	}
	if msg, ok := raw["capture_hosts"]; ok && !jsonNull(msg) {
		var hosts []string
		if err := json.Unmarshal(msg, &hosts); err != nil {
			return Values{}, ErrCaptureHosts
		}
		out.CaptureHosts = trimHosts(hosts)
	}
	if msg, ok := raw["hosts_profile"]; ok && !jsonNull(msg) {
		s, err := jsonString(msg)
		if err != nil {
			return Values{}, ErrHostsProfile
		}
		out.HostsProfile = s
	}
	if _, err := capture.Compile(out.Capture, out.CaptureEmpty, out.CaptureHTTPS, out.CaptureHosts); err != nil {
		return Values{}, err
	}

	for _, key := range []string{"proxy", "ui", "max_flows", "max_body", "theme", "capture", "capture_empty", "capture_https", "capture_hosts", "hosts_profile"} {
		msg, ok := raw[key]
		if !ok || jsonNull(msg) {
			return Values{}, ErrMissingField
		}
	}
	for key := range raw {
		if _, ok := knownFields[key]; !ok {
			return Values{}, ErrUnknownField
		}
	}
	return out, nil
}

func MarshalYAML(v Values) []byte {
	var b strings.Builder
	b.WriteString("proxy: ")
	b.WriteString(strconv.Quote(v.Proxy))
	b.WriteByte('\n')
	b.WriteString("ui: ")
	b.WriteString(strconv.Quote(v.UI))
	b.WriteByte('\n')
	b.WriteString("max_flows: ")
	b.WriteString(strconv.FormatInt(int64(v.MaxFlows), 10))
	b.WriteByte('\n')
	b.WriteString("max_body: ")
	b.WriteString(strconv.FormatInt(v.MaxBody, 10))
	b.WriteByte('\n')
	b.WriteString("theme: ")
	b.WriteString(strconv.Quote(v.Theme))
	b.WriteByte('\n')
	b.WriteString("capture: ")
	if v.Capture {
		b.WriteString("true")
	} else {
		b.WriteString("false")
	}
	b.WriteByte('\n')
	b.WriteString("capture_empty: ")
	b.WriteString(strconv.Quote(v.CaptureEmpty))
	b.WriteByte('\n')
	b.WriteString("capture_https: ")
	b.WriteString(strconv.Quote(v.CaptureHTTPS))
	b.WriteByte('\n')
	b.WriteString("capture_hosts:")
	if len(v.CaptureHosts) == 0 {
		b.WriteString(" []\n")
	} else {
		b.WriteByte('\n')
		for _, host := range v.CaptureHosts {
			b.WriteString("- ")
			b.WriteString(strconv.Quote(host))
			b.WriteByte('\n')
		}
	}
	b.WriteString("hosts_profile: ")
	b.WriteString(strconv.Quote(v.HostsProfile))
	b.WriteByte('\n')
	return []byte(b.String())
}

func unwrapYAMLDocument(n *yaml.Node) *yaml.Node {
	if n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		return n.Content[0]
	}
	return n
}

func isYAMLNull(n *yaml.Node) bool {
	return n.Tag == "!!null" || (n.Kind == yaml.ScalarNode && n.Tag == "" && n.Value == "null")
}

func yamlString(n *yaml.Node) (string, error) {
	if n.Kind != yaml.ScalarNode || n.Tag != "!!str" {
		return "", errors.New("not a string")
	}
	return n.Value, nil
}

func yamlBool(n *yaml.Node) (bool, error) {
	if n.Kind != yaml.ScalarNode || n.Tag != "!!bool" || (n.Value != "true" && n.Value != "false") {
		return false, errors.New("not a bool")
	}
	return n.Value == "true", nil
}

func yamlHostList(n *yaml.Node) ([]string, error) {
	if n.Kind != yaml.SequenceNode || n.Tag != "!!seq" {
		return nil, ErrCaptureHosts
	}
	hosts := make([]string, 0, len(n.Content))
	for _, item := range n.Content {
		s, err := yamlString(item)
		if err != nil {
			return nil, ErrCaptureHosts
		}
		hosts = append(hosts, strings.TrimSpace(s))
	}
	if len(hosts) == 0 {
		return []string{}, nil
	}
	return hosts, nil
}

func trimHosts(hosts []string) []string {
	out := make([]string, 0, len(hosts))
	for _, h := range hosts {
		out = append(out, strings.TrimSpace(h))
	}
	if len(out) == 0 {
		return []string{}
	}
	return out
}

func yamlInt(n *yaml.Node, bitSize int) (int64, error) {
	if n.Kind != yaml.ScalarNode || n.Tag != "!!int" {
		return 0, errors.New("not an int")
	}
	return strconv.ParseInt(n.Value, 10, bitSize)
}

func jsonNull(msg json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(msg), []byte("null"))
}

func jsonString(msg json.RawMessage) (string, error) {
	var s string
	if err := json.Unmarshal(msg, &s); err != nil {
		return "", err
	}
	return s, nil
}

func jsonInt(msg json.RawMessage, bitSize int) (int64, error) {
	text := string(bytes.TrimSpace(msg))
	if strings.ContainsAny(text, ".eE") {
		return 0, errors.New("not an integer")
	}
	return strconv.ParseInt(text, 10, bitSize)
}
