package api

import (
	"errors"
	"io"
	"net/http"

	"httpproxy/internal/config"
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
	applied, err := s.session.Save(next)
	if err != nil {
		writeConfigError(w, err)
		return
	}
	if applied.SetMaxFlows {
		ids := s.store.SetMax(applied.MaxFlows)
		for _, id := range ids {
			s.hub.Broadcast(Event{Type: "flow.evicted", FlowID: id})
		}
	}
	if applied.SetMaxBody && s.proxy != nil {
		s.proxy.SetMaxBody(applied.MaxBody)
	}
	writeJSON(w, http.StatusOK, struct {
		File    config.Values `json:"file"`
		Runtime config.Values `json:"runtime"`
	}{applied.File, applied.Runtime})
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
