package proxy

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
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

	if req.Method == http.MethodConnect {
		_ = req.Body.Close()
		p.handleConnect(conn, req)
		return
	}

	p.handleRequest(conn, req, "http", "")
}

func (p *Proxy) handleConnect(conn net.Conn, connectReq *http.Request) {
	if p.ca == nil {
		writeResponse(conn, http.StatusBadGateway, "HTTPS interception requires a CA\n")
		return
	}
	if _, err := io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}

	connectHost := connectReq.URL.Hostname()
	if connectHost == "" {
		connectHost = hostWithoutPort(connectReq.Host)
	}
	tlsConn := tls.Server(conn, &tls.Config{
		GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			if hello.ServerName != "" || connectHost == "" {
				return p.ca.GetCertificate(hello)
			}
			fallbackHello := *hello
			fallbackHello.ServerName = connectHost
			return p.ca.GetCertificate(&fallbackHello)
		},
		NextProtos: []string{"h2", "http/1.1"},
	})
	if err := tlsConn.Handshake(); err != nil {
		return
	}
	if tlsConn.ConnectionState().NegotiatedProtocol == "h2" {
		// TODO(task 6): serve HTTP/2 streams after TLS negotiation.
		return
	}

	reader := bufio.NewReader(tlsConn)
	for {
		req, err := http.ReadRequest(reader)
		if err != nil {
			return
		}
		if !p.handleRequest(tlsConn, req, "https", connectReq.Host) {
			return
		}
	}
}

func (p *Proxy) handleRequest(conn net.Conn, req *http.Request, scheme, defaultHost string) bool {
	started := time.Now()
	requestBody, readErr := io.ReadAll(req.Body)
	_ = req.Body.Close()
	requestCapture := newBodyCapture(p.maxBody)
	_, _ = requestCapture.Write(requestBody)
	host := req.Host
	if host == "" {
		host = defaultHost
	}
	f := flow.Flow{
		ID:          uuid.NewString(),
		Scheme:      scheme,
		Method:      req.Method,
		Host:        host,
		Path:        req.URL.RequestURI(),
		HTTPVersion: fmt.Sprintf("%d.%d", req.ProtoMajor, req.ProtoMinor),
		ReqHeaders:  copyHeaders(req.Header),
		StartedAt:   started,
		ClientAddr:  conn.RemoteAddr().String(),
	}
	f.ReqBody, f.ReqSize, f.ReqTruncated = requestCapture.result()
	p.created(f)

	if readErr != nil {
		f.StatusCode = http.StatusBadRequest
		f.Error = readErr.Error()
		finishFlow(&f, started)
		p.updated(f)
		writeResponse(conn, http.StatusBadRequest, "Bad Request\n")
		return false
	}
	req.Body = io.NopCloser(bytes.NewReader(requestBody))
	req.RequestURI = ""
	if req.URL.Scheme == "" {
		req.URL.Scheme = scheme
	}
	if req.URL.Host == "" {
		req.URL.Host = host
	}

	resp, err := p.roundTrip(req)
	if err != nil {
		f.StatusCode = http.StatusBadGateway
		f.Error = err.Error()
		finishFlow(&f, started)
		p.updated(f)
		writeResponse(conn, http.StatusBadGateway, "Bad Gateway\n")
		return false
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
	resp.Close = req.Close

	writeErr := resp.Write(conn)
	if writeErr != nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		f.Error = writeErr.Error()
	}
	f.RespBody, f.RespSize, f.RespTruncated = responseCapture.result()
	finishFlow(&f, started)
	p.updated(f)
	return writeErr == nil && !req.Close
}

func (p *Proxy) roundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme != "https" || p.UpstreamTLS == nil {
		return p.transport.RoundTrip(req)
	}
	transport := p.transport.Clone()
	transport.TLSClientConfig = p.UpstreamTLS.Clone()
	defer transport.CloseIdleConnections()
	return transport.RoundTrip(req)
}

func hostWithoutPort(hostport string) string {
	if host, _, err := net.SplitHostPort(hostport); err == nil {
		return host
	}
	if parsed, err := url.Parse("//" + hostport); err == nil && parsed.Hostname() != "" {
		return parsed.Hostname()
	}
	return hostport
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
