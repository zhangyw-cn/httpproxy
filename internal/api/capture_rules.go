package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"httpproxy/internal/config"
)

type captureRulesBody struct {
	CaptureEmpty string   `json:"capture_empty"`
	CaptureHTTPS string   `json:"capture_https"`
	CaptureHosts []string `json:"capture_hosts"`
}

// putCaptureRules merges the capture rules into config.yaml without touching
// capture or hosts_profile. The body must carry exactly the three rule keys.
func (s *Server) putCaptureRules(w http.ResponseWriter, r *http.Request) {
	if s.session == nil {
		writeConfigError(w, errors.New("nil session"))
		return
	}
	raw, err := decodeHostsBody(r, "capture_empty", "capture_https", "capture_hosts")
	if err != nil {
		writeConfigError(w, err)
		return
	}
	if isNullField(raw["capture_empty"]) || isNullField(raw["capture_https"]) || isNullField(raw["capture_hosts"]) {
		writeConfigError(w, config.ErrMissingField)
		return
	}

	var body captureRulesBody
	empty, ok := decodeStringField(raw, "capture_empty")
	if !ok {
		writeConfigError(w, config.ErrCaptureEmpty)
		return
	}
	body.CaptureEmpty = empty
	httpsMode, ok := decodeStringField(raw, "capture_https")
	if !ok {
		writeConfigError(w, config.ErrCaptureHTTPS)
		return
	}
	body.CaptureHTTPS = httpsMode
	if err := json.Unmarshal(raw["capture_hosts"], &body.CaptureHosts); err != nil {
		writeConfigError(w, config.ErrCaptureHosts)
		return
	}
	hosts := make([]string, 0, len(body.CaptureHosts))
	for _, rawHost := range body.CaptureHosts {
		if host := strings.TrimSpace(rawHost); host != "" {
			hosts = append(hosts, host)
		}
	}

	applied, err := s.session.SetCaptureRules(body.CaptureEmpty, body.CaptureHTTPS, hosts, s.applySaved)
	if err != nil {
		writeConfigError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		File    config.Values `json:"file"`
		Runtime config.Values `json:"runtime"`
	}{applied.File, applied.Runtime})
}

// isNullField reports whether a present JSON field is null. The config
// handlers treat null as an absent field.
func isNullField(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}
