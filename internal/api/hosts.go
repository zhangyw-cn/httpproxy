package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"httpproxy/internal/config"
	"httpproxy/internal/hosts"
)

const (
	hostsProfileSuffix = ".hosts"
	maxHostsBodyBytes  = 8 << 20
)

// hostProfileJSON is one row of the hosts list. Warnings is always a slice so
// the JSON shape stays [] when there is nothing to report.
type hostProfileJSON struct {
	Name     string          `json:"name"`
	Rules    int             `json:"rules"`
	Warnings []hosts.Warning `json:"warnings"`
	Error    string          `json:"error"`
}

type hostsListJSON struct {
	Active      string            `json:"active"`
	ActiveError string            `json:"active_error"`
	Profiles    []hostProfileJSON `json:"profiles"`
}

type hostDetailJSON struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

type hostSaveJSON struct {
	Name     string          `json:"name"`
	Rules    int             `json:"rules"`
	Warnings []hosts.Warning `json:"warnings"`
}

type hostErrorJSON struct {
	Error string `json:"error"`
	Line  int    `json:"line,omitempty"`
}

// SetHostsProfiles installs the profile store. It is called once at startup,
// before the listeners accept requests.
func (s *Server) SetHostsProfiles(p *hosts.Profiles) {
	s.hostsProfiles = p
}

// InitHosts loads and applies the profile named by config.yaml. A non-empty
// name that is missing or invalid is an error: the process must not silently
// fall back to real DNS.
func (s *Server) InitHosts() error {
	if s.session == nil {
		return nil
	}
	file, _ := s.session.Snapshot()
	raw := strings.TrimSpace(file.HostsProfile)
	if raw == "" {
		s.applyHostsTable("", hosts.Table{})
		return nil
	}
	if !hosts.ValidName(raw) {
		return fmt.Errorf("hosts profile %q: %w", raw, hosts.ErrInvalidName)
	}
	if s.hostsProfiles == nil {
		return fmt.Errorf("hosts profile %q: %w", raw, hosts.ErrNotFound)
	}
	table, err := s.loadHostsTable(raw)
	if err != nil {
		return fmt.Errorf("hosts profile %q: %w", raw, err)
	}
	s.applyHostsTable(raw, table)
	return nil
}

// loadHostsTable reads and parses the named profile. The empty name is the
// "no hosts" case and yields an empty table.
func (s *Server) loadHostsTable(name string) (hosts.Table, error) {
	if name == "" {
		return hosts.Table{}, nil
	}
	clean, ok := cleanHostsName(name)
	if !ok {
		return hosts.Table{}, hosts.ErrInvalidName
	}
	if s.hostsProfiles == nil {
		return hosts.Table{}, hosts.ErrNotFound
	}
	data, err := s.hostsProfiles.Read(clean)
	if err != nil {
		return hosts.Table{}, err
	}
	table, _, err := hosts.Parse(data)
	if err != nil {
		return hosts.Table{}, err
	}
	return table, nil
}

// applyHostsTable records the applied profile and installs it on the proxy.
// The proxy call happens after releasing hostsMu so the proxy lock is never
// nested inside the API lock.
func (s *Server) applyHostsTable(name string, table hosts.Table) {
	var modTime time.Time
	var size int64
	if name != "" && s.hostsProfiles != nil {
		if info, err := os.Stat(s.profilePath(name)); err == nil {
			modTime = info.ModTime()
			size = info.Size()
		}
	}
	s.applyHostsTableStat(name, table, modTime, size)
}

// applyHostsTableStat is applyHostsTable with the stat the caller already read,
// so a file changing between the read and the apply cannot make the cached
// stat hide the change from the next refresh.
func (s *Server) applyHostsTableStat(name string, table hosts.Table, modTime time.Time, size int64) {
	s.hostsMu.Lock()
	s.hostsActive = name
	s.hostsRules = table.Len()
	s.hostsErr = ""
	s.hostsModTime = modTime
	s.hostsSize = size
	s.hostsMu.Unlock()

	if s.proxy != nil {
		s.proxy.SetHosts(name, table)
	}
}

// setHostsActive records a renamed active profile and forgets the cached file
// stat so the next refresh reloads the file under its new name.
func (s *Server) setHostsActive(name string) {
	s.hostsMu.Lock()
	s.hostsActive = name
	s.hostsModTime = time.Time{}
	s.hostsSize = 0
	s.hostsMu.Unlock()
}

func (s *Server) setHostsError(msg string) {
	s.hostsMu.Lock()
	s.hostsErr = msg
	s.hostsMu.Unlock()
}

func (s *Server) hostsState() (active string, rules int, errMsg string) {
	s.hostsMu.Lock()
	defer s.hostsMu.Unlock()
	return s.hostsActive, s.hostsRules, s.hostsErr
}

// refreshActiveHosts re-reads the active profile when its file changed since
// the last successful apply. A bad or missing file keeps the last good table
// and only records active_error.
func (s *Server) refreshActiveHosts() {
	if s.hostsProfiles == nil {
		return
	}
	active, _, _ := s.hostsState()
	if active == "" {
		return
	}
	s.hostsMu.Lock()
	modTime, size := s.hostsModTime, s.hostsSize
	s.hostsMu.Unlock()

	path := s.profilePath(active)
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			s.setHostsError("文件不存在")
		} else {
			s.setHostsError("无法读取配置")
		}
		return
	}
	if info.ModTime().Equal(modTime) && info.Size() == size {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		s.setHostsError("无法读取配置")
		return
	}
	table, _, err := hosts.Parse(data)
	if err != nil {
		_, msg := parseErrorMessage(err)
		s.setHostsError(msg)
		return
	}
	s.applyHostsTableStat(active, table, info.ModTime(), info.Size())
}

// hostsList builds the GET /api/hosts body. The active profile is sorted first
// even when its file is missing from the directory listing.
func (s *Server) hostsList() (hostsListJSON, error) {
	active, _, activeErr := s.hostsState()
	out := hostsListJSON{
		Active:      active,
		ActiveError: activeErr,
		Profiles:    []hostProfileJSON{},
	}
	if s.hostsProfiles == nil {
		return out, nil
	}
	names, err := s.hostsProfiles.List()
	if err != nil {
		return hostsListJSON{}, err
	}

	var activeEntry hostProfileJSON
	rest := make([]hostProfileJSON, 0, len(names))
	activeFound := false
	for _, name := range names {
		entry := s.profileEntry(name)
		if active != "" && strings.EqualFold(name, active) {
			activeEntry = entry
			activeFound = true
			continue
		}
		rest = append(rest, entry)
	}

	if activeFound {
		out.Profiles = append(out.Profiles, activeEntry)
	} else if active != "" {
		out.Profiles = append(out.Profiles, hostProfileJSON{
			Name:     active,
			Warnings: []hosts.Warning{},
			Error:    activeErr,
		})
	}
	out.Profiles = append(out.Profiles, rest...)
	return out, nil
}

func (s *Server) profileEntry(name string) hostProfileJSON {
	entry := hostProfileJSON{Name: name, Warnings: []hosts.Warning{}}
	data, err := s.hostsProfiles.Read(name)
	if err != nil {
		entry.Error = hostsReadMessage(err)
		return entry
	}
	table, warnings, err := hosts.Parse(data)
	if err != nil {
		_, entry.Error = parseErrorMessage(err)
		return entry
	}
	entry.Rules = table.Len()
	if len(warnings) > 0 {
		entry.Warnings = warnings
	}
	return entry
}

// parseErrorMessage turns a hosts.ParseError into the user-facing message and
// the 1-based line number, if the error belongs to a line.
func parseErrorMessage(err error) (line int, msg string) {
	var parseErr *hosts.ParseError
	if !errors.As(err, &parseErr) {
		return 0, "配置无效"
	}
	line = parseErr.Line
	switch parseErr.Msg {
	case "invalid IP":
		msg = "IP 不合法"
	case "invalid hostname":
		msg = "域名不合法"
	case "missing field":
		msg = "缺少字段"
	case "file exceeds 1 MiB":
		msg = "文件超过 1 MiB"
	case "too many mappings":
		msg = "规则超过 1000 条"
	default:
		msg = parseErr.Msg
	}
	if line > 0 {
		msg = fmt.Sprintf("第 %d 行：%s", line, msg)
	}
	return line, msg
}

func hostsReadMessage(err error) string {
	switch {
	case errors.Is(err, hosts.ErrNotFound):
		return "配置不存在"
	case errors.Is(err, hosts.ErrInvalidName):
		return "配置名不合法"
	default:
		return "无法读取配置"
	}
}

// cleanHostsName trims and validates a profile name.
func cleanHostsName(raw string) (string, bool) {
	name := strings.TrimSpace(raw)
	if !hosts.ValidName(name) {
		return "", false
	}
	return name, true
}

func (s *Server) profilePath(name string) string {
	return filepath.Join(s.hostsProfiles.Dir(), name+hostsProfileSuffix)
}

// configuredHostsName reports the profile name stored in config.yaml.
func (s *Server) configuredHostsName() string {
	if s.session == nil {
		return ""
	}
	file, _ := s.session.Snapshot()
	if name, ok := cleanHostsName(file.HostsProfile); ok {
		return name
	}
	return strings.TrimSpace(file.HostsProfile)
}

func (s *Server) isActiveHosts(name string) bool {
	active, _, _ := s.hostsState()
	if active != "" && strings.EqualFold(name, active) {
		return true
	}
	configured := s.configuredHostsName()
	return configured != "" && strings.EqualFold(name, configured)
}

// decodeHostsBody reads a strict JSON object that must carry exactly the
// requested keys.
func decodeHostsBody(r *http.Request, keys ...string) (map[string]json.RawMessage, error) {
	data, err := io.ReadAll(io.LimitReader(r.Body, maxHostsBodyBytes))
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	var raw map[string]json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		return nil, config.ErrJSON
	}
	var trailing json.RawMessage
	if err := dec.Decode(&trailing); err != io.EOF {
		return nil, config.ErrJSON
	}
	allowed := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		allowed[key] = struct{}{}
		if _, ok := raw[key]; !ok {
			return nil, config.ErrMissingField
		}
	}
	for key := range raw {
		if _, ok := allowed[key]; !ok {
			return nil, config.ErrUnknownField
		}
	}
	return raw, nil
}

func decodeStringField(raw map[string]json.RawMessage, key string) (string, bool) {
	var value *string
	if err := json.Unmarshal(raw[key], &value); err != nil || value == nil {
		return "", false
	}
	return *value, true
}

func (s *Server) listHosts(w http.ResponseWriter, _ *http.Request) {
	s.refreshActiveHosts()
	list, err := s.hostsList()
	if err != nil {
		writeHostsError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) getHost(w http.ResponseWriter, r *http.Request) {
	if s.hostsProfiles == nil {
		writeJSON(w, http.StatusNotFound, hostErrorJSON{Error: "配置不存在"})
		return
	}
	name, ok := cleanHostsName(r.PathValue("name"))
	if !ok {
		writeHostsError(w, hosts.ErrInvalidName)
		return
	}
	data, err := s.hostsProfiles.Read(name)
	if err != nil {
		writeHostsError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, hostDetailJSON{Name: name, Content: string(data)})
}

func (s *Server) putHost(w http.ResponseWriter, r *http.Request) {
	if s.hostsProfiles == nil {
		writeJSON(w, http.StatusNotFound, hostErrorJSON{Error: "配置不存在"})
		return
	}
	name, ok := cleanHostsName(r.PathValue("name"))
	if !ok {
		writeHostsError(w, hosts.ErrInvalidName)
		return
	}
	raw, err := decodeHostsBody(r, "content")
	if err != nil {
		writeConfigError(w, err)
		return
	}
	content, ok := decodeStringField(raw, "content")
	if !ok {
		writeConfigError(w, config.ErrJSON)
		return
	}

	table, warnings, err := hosts.Parse([]byte(content))
	if err != nil {
		writeHostsError(w, err)
		return
	}
	if len(warnings) == 0 {
		warnings = []hosts.Warning{}
	}
	if err := s.hostsProfiles.Write(name, []byte(content)); err != nil {
		writeHostsError(w, err)
		return
	}
	if s.isActiveHosts(name) {
		s.applyHostsTable(name, table)
	}
	writeJSON(w, http.StatusOK, hostSaveJSON{Name: name, Rules: table.Len(), Warnings: warnings})
}

func (s *Server) createHost(w http.ResponseWriter, r *http.Request) {
	if s.hostsProfiles == nil {
		writeHostsError(w, errNoHostsProfiles)
		return
	}
	raw, err := decodeHostsBody(r, "name")
	if err != nil {
		writeConfigError(w, err)
		return
	}
	rawName, ok := decodeStringField(raw, "name")
	if !ok {
		writeHostsError(w, hosts.ErrInvalidName)
		return
	}
	name, ok := cleanHostsName(rawName)
	if !ok {
		writeHostsError(w, hosts.ErrInvalidName)
		return
	}
	if err := s.hostsProfiles.Create(name); err != nil {
		writeHostsError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) activateHosts(w http.ResponseWriter, r *http.Request) {
	raw, err := decodeHostsBody(r, "active")
	if err != nil {
		writeConfigError(w, err)
		return
	}
	rawActive, ok := decodeStringField(raw, "active")
	if !ok {
		writeJSON(w, http.StatusBadRequest, hostErrorJSON{Error: "Hosts 配置名必须是字符串"})
		return
	}
	name := ""
	if strings.TrimSpace(rawActive) != "" {
		name, ok = cleanHostsName(rawActive)
		if !ok {
			writeHostsError(w, hosts.ErrInvalidName)
			return
		}
	}

	table, err := s.loadHostsTable(name)
	if err != nil {
		writeActiveHostsError(w, err)
		return
	}
	if s.session == nil {
		writeConfigError(w, errors.New("nil session"))
		return
	}
	if _, err := s.session.SetHostsProfile(name, nil); err != nil {
		writeConfigError(w, err)
		return
	}
	s.applyHostsTable(name, table)

	list, err := s.hostsList()
	if err != nil {
		writeHostsError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) renameHost(w http.ResponseWriter, r *http.Request) {
	if s.hostsProfiles == nil {
		writeJSON(w, http.StatusNotFound, hostErrorJSON{Error: "配置不存在"})
		return
	}
	oldName, ok := cleanHostsName(r.PathValue("name"))
	if !ok {
		writeHostsError(w, hosts.ErrInvalidName)
		return
	}
	raw, err := decodeHostsBody(r, "name")
	if err != nil {
		writeConfigError(w, err)
		return
	}
	rawName, ok := decodeStringField(raw, "name")
	if !ok {
		writeHostsError(w, hosts.ErrInvalidName)
		return
	}
	newName, ok := cleanHostsName(rawName)
	if !ok {
		writeHostsError(w, hosts.ErrInvalidName)
		return
	}

	wasActive := s.isActiveHosts(oldName)
	if err := s.hostsProfiles.Rename(oldName, newName); err != nil {
		writeHostsError(w, err)
		return
	}
	if !wasActive {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if s.session == nil {
		_ = s.hostsProfiles.Rename(newName, oldName)
		writeConfigError(w, errors.New("nil session"))
		return
	}
	if _, err := s.session.SetHostsProfile(newName, nil); err != nil {
		_ = s.hostsProfiles.Rename(newName, oldName)
		writeConfigError(w, err)
		return
	}
	s.setHostsActive(newName)
	s.refreshActiveHosts()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteHost(w http.ResponseWriter, r *http.Request) {
	if s.hostsProfiles == nil {
		writeJSON(w, http.StatusNotFound, hostErrorJSON{Error: "配置不存在"})
		return
	}
	name, ok := cleanHostsName(r.PathValue("name"))
	if !ok {
		writeHostsError(w, hosts.ErrInvalidName)
		return
	}
	if s.isActiveHosts(name) {
		writeJSON(w, http.StatusConflict, hostErrorJSON{Error: "当前启用的 Hosts 配置不能删除"})
		return
	}
	if err := s.hostsProfiles.Delete(name); err != nil {
		writeHostsError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

var errNoHostsProfiles = errors.New("hosts profiles are not configured")

// writeActiveHostsError is the activation/config case: a target profile that
// does not exist is a client error, not a 404.
func writeActiveHostsError(w http.ResponseWriter, err error) {
	if errors.Is(err, hosts.ErrNotFound) {
		writeJSON(w, http.StatusBadRequest, hostErrorJSON{Error: "配置不存在"})
		return
	}
	writeHostsError(w, err)
}

func writeHostsError(w http.ResponseWriter, err error) {
	status, msg, line := http.StatusInternalServerError, "无法写入配置", 0
	var parseErr *hosts.ParseError
	switch {
	case errors.As(err, &parseErr):
		status = http.StatusBadRequest
		line, msg = parseErrorMessage(err)
	case errors.Is(err, hosts.ErrInvalidName):
		status, msg = http.StatusBadRequest, "配置名不合法"
	case errors.Is(err, hosts.ErrExists):
		status, msg = http.StatusConflict, "同名配置已存在"
	case errors.Is(err, hosts.ErrNotFound):
		status, msg = http.StatusNotFound, "配置不存在"
	}
	writeJSON(w, status, hostErrorJSON{Error: msg, Line: line})
}
