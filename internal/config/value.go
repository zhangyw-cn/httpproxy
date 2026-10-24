package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type Values struct {
	Proxy    string `json:"proxy" yaml:"proxy"`
	UI       string `json:"ui" yaml:"ui"`
	MaxFlows int    `json:"max_flows" yaml:"max_flows"`
	MaxBody  int64  `json:"max_body" yaml:"max_body"`
	Theme    string `json:"theme" yaml:"theme"`
}

var (
	ErrProxyAddress = errors.New("invalid proxy address")
	ErrUIAddress    = errors.New("invalid ui address")
	ErrMaxFlows     = errors.New("max_flows must be an integer >= 1")
	ErrMaxBody      = errors.New("max_body must be an integer >= 0")
	ErrTheme        = errors.New("theme must be dark or light")
	ErrMissingField = errors.New("missing config field")
	ErrUnknownField = errors.New("unknown config field")
	ErrJSON         = errors.New("invalid config JSON")
)

var knownFields = map[string]struct{}{
	"proxy":     {},
	"ui":        {},
	"max_flows": {},
	"max_body":  {},
	"theme":     {},
}

func Defaults() Values {
	return Values{
		Proxy:    "127.0.0.1:8080",
		UI:       "127.0.0.1:8081",
		MaxFlows: 1000,
		MaxBody:  1048576,
		Theme:    "dark",
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

	for _, key := range []string{"proxy", "ui", "max_flows", "max_body", "theme"} {
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
