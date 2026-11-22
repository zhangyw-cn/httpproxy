package hosts

import (
	"fmt"
	"net"
	"strings"
)

const (
	// MaxFileBytes is the largest hosts file body Parse accepts.
	MaxFileBytes = 1 << 20
	// MaxMappings is the largest number of mapping lines one file may contain.
	MaxMappings = 1000
	// MaxHostLen is the longest domain name Parse accepts.
	MaxHostLen = 253
)

// Warning reports a mapping that was ignored because the domain already had a
// mapping earlier in the same file.
type Warning struct {
	Domain      string `json:"domain"`
	IgnoredLine int    `json:"ignored_line"`
	FirstLine   int    `json:"first_line"`
}

// ParseError is a hosts syntax error. Line is 1-based; it is 0 when the error
// does not belong to a line.
type ParseError struct {
	Line int
	Msg  string
}

func (e *ParseError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("%d: %s", e.Line, e.Msg)
	}
	return e.Msg
}

// Table is an exact, case-insensitive domain-to-IP lookup table.
type Table struct {
	entries map[string]net.IP
}

// Parse turns a hosts file body into a lookup table. Duplicate domains are not
// errors: the first mapping wins and each later one produces a Warning.
func Parse(data []byte) (Table, []Warning, error) {
	if len(data) > MaxFileBytes {
		return Table{}, nil, &ParseError{Msg: "file exceeds 1 MiB"}
	}

	table := Table{entries: make(map[string]net.IP)}
	firstLines := make(map[string]int)
	var warnings []Warning
	mappings := 0

	for i, raw := range strings.Split(string(data), "\n") {
		lineNo := i + 1
		line := strings.TrimSuffix(raw, "\r")
		if comment := strings.IndexByte(line, '#'); comment >= 0 {
			line = line[:comment]
		}

		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) < 2 {
			return Table{}, nil, &ParseError{Line: lineNo, Msg: "missing field"}
		}

		ip := net.ParseIP(fields[0])
		if ip == nil {
			return Table{}, nil, &ParseError{Line: lineNo, Msg: "invalid IP"}
		}

		mappings++
		if mappings > MaxMappings {
			return Table{}, nil, &ParseError{Line: lineNo, Msg: "too many mappings"}
		}

		for _, field := range fields[1:] {
			domain, ok := normalizeHostname(field)
			if !ok {
				return Table{}, nil, &ParseError{Line: lineNo, Msg: "invalid hostname"}
			}

			if firstLine, seen := firstLines[domain]; seen {
				warnings = append(warnings, Warning{
					Domain:      domain,
					IgnoredLine: lineNo,
					FirstLine:   firstLine,
				})
				continue
			}
			firstLines[domain] = lineNo
			table.entries[domain] = ip
		}
	}

	return table, warnings, nil
}

// Lookup returns the mapped IP for host. Lookup applies the same normalization
// as Parse.
func (t Table) Lookup(host string) (net.IP, bool) {
	name := normalizeForLookup(host)
	ip, ok := t.entries[name]
	return ip, ok
}

// Len returns the number of unique domains in the table.
func (t Table) Len() int {
	return len(t.entries)
}

// normalizeHostname lower-cases name, removes one trailing dot, and validates
// the result as an exact DNS domain name.
func normalizeHostname(name string) (string, bool) {
	name = normalizeForLookup(name)
	if name == "" || len(name) > MaxHostLen {
		return "", false
	}
	if strings.ContainsAny(name, "*:/") {
		return "", false
	}
	if net.ParseIP(name) != nil {
		return "", false
	}
	if !validDNSName(name) {
		return "", false
	}
	return name, true
}

func normalizeForLookup(name string) string {
	return strings.TrimSuffix(strings.ToLower(name), ".")
}

func validDNSName(name string) bool {
	if strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".") {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if label == "" || len(label) > 63 {
			return false
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			switch {
			case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-':
			default:
				return false
			}
		}
	}
	return true
}
