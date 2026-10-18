package flow

import "time"

type Header struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type Flow struct {
	ID          string   `json:"id"`
	Scheme      string   `json:"scheme"`
	Method      string   `json:"method"`
	Host        string   `json:"host"`
	Path        string   `json:"path"`
	HTTPVersion string   `json:"http_version"`
	StatusCode  int      `json:"status_code"`
	ReqHeaders  []Header `json:"req_headers"`
	RespHeaders []Header `json:"resp_headers"`
	ReqBody     []byte   `json:"req_body,omitempty"`
	RespBody    []byte   `json:"resp_body,omitempty"`
	// RespBodyDecoded is a view-only decompressed copy of RespBody when the
	// response had a Content-Encoding; clients always receive the raw bytes.
	RespBodyDecoded []byte    `json:"resp_body_decoded,omitempty"`
	ReqSize         int64     `json:"req_size"`
	RespSize        int64     `json:"resp_size"`
	ReqTruncated    bool      `json:"req_truncated"`
	RespTruncated   bool      `json:"resp_truncated"`
	StartedAt       time.Time `json:"started_at"`
	FinishedAt      time.Time `json:"finished_at,omitempty"`
	DurationMs      int64     `json:"duration_ms"`
	Error           string    `json:"error,omitempty"`
	ClientAddr      string    `json:"client_addr"`
}

// FlowSummary is list/WS payload without bodies.
type FlowSummary struct {
	ID            string    `json:"id"`
	Scheme        string    `json:"scheme"`
	Method        string    `json:"method"`
	Host          string    `json:"host"`
	Path          string    `json:"path"`
	HTTPVersion   string    `json:"http_version"`
	StatusCode    int       `json:"status_code"`
	ReqSize       int64     `json:"req_size"`
	RespSize      int64     `json:"resp_size"`
	ReqTruncated  bool      `json:"req_truncated"`
	RespTruncated bool      `json:"resp_truncated"`
	StartedAt     time.Time `json:"started_at"`
	FinishedAt    time.Time `json:"finished_at,omitempty"`
	DurationMs    int64     `json:"duration_ms"`
	Error         string    `json:"error,omitempty"`
	ClientAddr    string    `json:"client_addr"`
}

func (f Flow) Summary() FlowSummary {
	return FlowSummary{
		ID: f.ID, Scheme: f.Scheme, Method: f.Method, Host: f.Host, Path: f.Path,
		HTTPVersion: f.HTTPVersion, StatusCode: f.StatusCode,
		ReqSize: f.ReqSize, RespSize: f.RespSize,
		ReqTruncated: f.ReqTruncated, RespTruncated: f.RespTruncated,
		StartedAt: f.StartedAt, FinishedAt: f.FinishedAt, DurationMs: f.DurationMs,
		Error: f.Error, ClientAddr: f.ClientAddr,
	}
}
