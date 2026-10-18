package proxy

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"httpproxy/internal/flow"
)

func (p *Proxy) handleHTTP1(conn net.Conn) {
	req, err := http.ReadRequest(bufio.NewReader(conn))
	if err != nil {
		return
	}
	defer req.Body.Close()

	if req.Method == http.MethodConnect {
		writeResponse(conn, http.StatusNotImplemented, "CONNECT is not implemented\n")
		return
	}

	started := time.Now()
	f := flow.Flow{
		ID:          uuid.NewString(),
		Scheme:      "http",
		Method:      req.Method,
		Host:        req.Host,
		Path:        req.URL.RequestURI(),
		HTTPVersion: req.Proto,
		ReqHeaders:  copyHeaders(req.Header),
		StartedAt:   started,
		ClientAddr:  conn.RemoteAddr().String(),
	}
	p.created(f)

	requestCapture := newBodyCapture(p.maxBody)
	req.Body = struct {
		io.Reader
		io.Closer
	}{
		Reader: io.TeeReader(req.Body, requestCapture),
		Closer: req.Body,
	}
	req.RequestURI = ""
	if req.URL.Scheme == "" {
		req.URL.Scheme = "http"
	}
	if req.URL.Host == "" {
		req.URL.Host = req.Host
	}

	resp, err := p.transport.RoundTrip(req)
	f.ReqBody, f.ReqSize, f.ReqTruncated = requestCapture.result()
	if err != nil {
		f.StatusCode = http.StatusBadGateway
		f.Error = err.Error()
		finishFlow(&f, started)
		p.updated(f)
		writeResponse(conn, http.StatusBadGateway, "Bad Gateway\n")
		return
	}
	defer resp.Body.Close()

	f.StatusCode = resp.StatusCode
	f.RespHeaders = copyHeaders(resp.Header)
	responseCapture := newBodyCapture(p.maxBody)
	resp.Body = struct {
		io.Reader
		io.Closer
	}{
		Reader: io.TeeReader(resp.Body, responseCapture),
		Closer: resp.Body,
	}
	resp.Close = true

	writeErr := resp.Write(conn)
	if writeErr != nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		f.Error = writeErr.Error()
	}
	f.RespBody, f.RespSize, f.RespTruncated = responseCapture.result()
	finishFlow(&f, started)
	p.updated(f)
}

func (p *Proxy) created(f flow.Flow) {
	if p.sink != nil {
		p.sink.OnCreated(f)
	}
}

func (p *Proxy) updated(f flow.Flow) {
	if p.sink != nil {
		p.sink.OnUpdated(f)
	}
}

func finishFlow(f *flow.Flow, started time.Time) {
	f.FinishedAt = time.Now()
	f.DurationMs = f.FinishedAt.Sub(started).Milliseconds()
}

func copyHeaders(headers http.Header) []flow.Header {
	var copied []flow.Header
	for name, values := range headers {
		for _, value := range values {
			copied = append(copied, flow.Header{Name: name, Value: value})
		}
	}
	return copied
}

func writeResponse(w io.Writer, status int, body string) {
	response := &http.Response{
		Status:        fmt.Sprintf("%d %s", status, http.StatusText(status)),
		StatusCode:    status,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        make(http.Header),
		Body:          io.NopCloser(strings.NewReader(body)),
		ContentLength: int64(len(body)),
		Close:         true,
	}
	response.Header.Set("Content-Type", "text/plain; charset=utf-8")
	_ = response.Write(w)
}
