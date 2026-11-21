package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"httpproxy/internal/capture"
	"httpproxy/internal/config"
	"httpproxy/internal/hosts"
	"httpproxy/internal/proxy"
)

func (s *Server) SetProxy(px *proxy.Proxy) {
	s.proxy = px
}

func (s *Server) config(w http.ResponseWriter, _ *http.Request) {
	if s.session == nil {
		writeConfigError(w, errors.New("nil session"))
		return
	}
	file, runtime := s.session.Snapshot()
	writeJSON(w, http.StatusOK, struct {
		File    config.Values `json:"file"`
		Runtime config.Values `json:"runtime"`
	}{file, runtime})
}

func (s *Server) putConfig(w http.ResponseWriter, r *http.Request) {
	if s.session == nil {
		writeConfigError(w, errors.New("nil session"))
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeConfigError(w, err)
		return
	}
	next, err := config.ParseJSON(data)
	if err != nil {
		writeConfigError(w, err)
		return
	}

	current, _ := s.session.Snapshot()
	next.HostsProfile = strings.TrimSpace(next.HostsProfile)
	hostsChanged := next.HostsProfile != strings.TrimSpace(current.HostsProfile)
	var pendingTable hosts.Table
	if hostsChanged {
		if next.HostsProfile != "" && !hosts.ValidName(next.HostsProfile) {
			writeHostsError(w, hosts.ErrInvalidName)
			return
		}
		table, err := s.loadHostsTable(next.HostsProfile)
		if err != nil {
			writeActiveHostsError(w, err)
			return
		}
		pendingTable = table
	}

	applied, err := s.session.SaveAndApply(next, s.applySaved)
	if err != nil {
		writeConfigError(w, err)
		return
	}
	if hostsChanged {
		s.applyHostsTable(next.HostsProfile, pendingTable)
	}
	writeJSON(w, http.StatusOK, struct {
		File    config.Values `json:"file"`
		Runtime config.Values `json:"runtime"`
	}{applied.File, applied.Runtime})
}

func (s *Server) putCapture(w http.ResponseWriter, r *http.Request) {
	if s.session == nil {
		writeConfigError(w, errors.New("nil session"))
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeConfigError(w, err)
		return
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	var raw map[string]json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		writeConfigError(w, config.ErrJSON)
		return
	}
	var trailing json.RawMessage
	if err := dec.Decode(&trailing); err != io.EOF {
		writeConfigError(w, config.ErrJSON)
		return
	}
	msg, ok := raw["capture"]
	if !ok || bytes.Equal(bytes.TrimSpace(msg), []byte("null")) {
		writeConfigError(w, config.ErrMissingField)
		return
	}
	for key := range raw {
		if key != "capture" {
			writeConfigError(w, config.ErrUnknownField)
			return
		}
	}
	var captureOn bool
	if err := json.Unmarshal(msg, &captureOn); err != nil {
		writeConfigError(w, config.ErrCapture)
		return
	}
	applied, err := s.session.SetCapture(captureOn, s.applySaved)
	if err != nil {
		writeConfigError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		File    config.Values `json:"file"`
		Runtime config.Values `json:"runtime"`
	}{applied.File, applied.Runtime})
}

func (s *Server) applySaved(applied config.Applied) {
	if applied.SetMaxFlows {
		ids := s.store.SetMax(applied.MaxFlows)
		for _, id := range ids {
			s.hub.Broadcast(Event{Type: "flow.evicted", FlowID: id})
		}
	}
	if applied.SetMaxBody && s.proxy != nil {
		s.proxy.SetMaxBody(applied.MaxBody)
	}
	s.applyCapture(applied.Runtime)
}

func (s *Server) applyCapture(v config.Values) {
	if s.proxy == nil {
		return
	}
	policy, err := capture.Compile(v.Capture, v.CaptureEmpty, v.CaptureHTTPS, v.CaptureHosts)
	if err != nil {
		panic(err)
	}
	s.proxy.SetPolicy(policy)
}

func writeConfigError(w http.ResponseWriter, err error) {
	status, msg := http.StatusInternalServerError, "无法写入配置"
	switch {
	case errors.Is(err, config.ErrProxyAddress):
		status, msg = http.StatusBadRequest, "代理地址必须是 host:port"
	case errors.Is(err, config.ErrUIAddress):
		status, msg = http.StatusBadRequest, "UI 地址必须是 host:port"
	case errors.Is(err, config.ErrMaxFlows):
		status, msg = http.StatusBadRequest, "保留条数必须是大于等于 1 的整数"
	case errors.Is(err, config.ErrMaxBody):
		status, msg = http.StatusBadRequest, "正文上限必须是大于等于 0 的整数"
	case errors.Is(err, config.ErrTheme):
		status, msg = http.StatusBadRequest, "主题必须是 dark 或 light"
	case errors.Is(err, config.ErrCapture):
		status, msg = http.StatusBadRequest, "抓包开关必须是布尔值"
	case errors.Is(err, config.ErrCaptureEmpty):
		status, msg = http.StatusBadRequest, "空规则必须是 all 或 none"
	case errors.Is(err, config.ErrCaptureHTTPS):
		status, msg = http.StatusBadRequest, "未抓 HTTPS 必须是 decrypt 或 tunnel"
	case errors.Is(err, config.ErrCaptureHosts):
		status, msg = http.StatusBadRequest, "主机规则必须是字符串列表"
	case errors.Is(err, config.ErrCaptureHostsLimit):
		status, msg = http.StatusBadRequest, "主机规则最多 100 条"
	case errors.Is(err, config.ErrCaptureHostLength):
		status, msg = http.StatusBadRequest, "主机规则不能超过 253 个字符"
	case errors.Is(err, config.ErrCaptureHost):
		status, msg = http.StatusBadRequest, "主机规则不合法"
	case errors.Is(err, config.ErrCaptureHostDup):
		status, msg = http.StatusBadRequest, "主机规则重复"
	case errors.Is(err, config.ErrHostsProfile):
		status, msg = http.StatusBadRequest, "Hosts 配置名必须是字符串"
	case errors.Is(err, config.ErrMissingField):
		status, msg = http.StatusBadRequest, "配置缺少字段"
	case errors.Is(err, config.ErrUnknownField):
		status, msg = http.StatusBadRequest, "配置含有未知字段"
	case errors.Is(err, config.ErrJSON):
		status, msg = http.StatusBadRequest, "配置不是合法的 JSON"
	}
	writeJSON(w, status, struct {
		Error string `json:"error"`
	}{Error: msg})
}
