package proxy

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"httpproxy/internal/capture"
	"httpproxy/internal/flow"
)

const (
	tlsHandshakeTimeout = 10 * time.Second
	idleReadTimeout     = 120 * time.Second
)

// hopHeaders are connection-specific and must not be forwarded (RFC 9110 7.6.1).
var hopHeaders = []string{
	"Connection",
	"Proxy-Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Te",
	"Trailer",
	"Transfer-Encoding",
	"Upgrade",
}

// bufferedConn keeps bytes already read by the request parser available to
// whoever takes over the connection after CONNECT.
type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) {
	return c.r.Read(p)
}

func (p *Proxy) handleHTTP1(conn net.Conn) {
	reader := bufio.NewReader(conn)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(idleReadTimeout))
		req, err := http.ReadRequest(reader)
		if err != nil {
			return
		}
		_ = conn.SetReadDeadline(time.Time{})
		if req.Method == http.MethodConnect {
			_ = req.Body.Close()
			p.handleConnect(&bufferedConn{Conn: conn, r: reader}, req)
			return
		}
		record := p.currentPolicy().Decide(false, req.Host) == capture.Record
		if !p.handleRequest(conn, req, "http", "", "", record) {
			return
		}
	}
}

func (p *Proxy) handleConnect(conn net.Conn, connectReq *http.Request) {
	connectAuthority := connectReq.URL.Host
	if connectAuthority == "" {
		connectAuthority = connectReq.Host
	}
	connectHost := hostWithoutPort(connectAuthority)
	if connectHost == "" {
		connectHost = hostWithoutPort(connectReq.Host)
	}
	// A CONNECT is one tunnel whether it is spliced or decrypted below.
	p.clientTunnel(conn, connectHost)

	action := p.currentPolicy().Decide(true, connectAuthority)
	if action == capture.Tunnel {
		p.spliceTunnel(conn, connectAuthority)
		return
	}
	// The CONNECT decision (decrypt vs tunnel) is fixed for this connection.
	// Recording is re-decided per request so pausing capture stops new flows.
	recordHandshakeFailure := action == capture.Record

	if p.ca == nil {
		writeResponse(conn, http.StatusBadGateway, "HTTPS interception requires a CA\n")
		return
	}
	if _, err := io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}

	var clientSNI string
	tlsConn := tls.Server(conn, &tls.Config{
		GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			if hello != nil && hello.ServerName != "" {
				clientSNI = hello.ServerName
			}
			if hello.ServerName != "" || connectHost == "" {
				return p.ca.GetCertificate(hello)
			}
			fallbackHello := *hello
			fallbackHello.ServerName = connectHost
			return p.ca.GetCertificate(&fallbackHello)
		},
		NextProtos: []string{"h2", "http/1.1"},
	})
	started := time.Now()
	_ = conn.SetDeadline(started.Add(tlsHandshakeTimeout))
	if err := tlsConn.Handshake(); err != nil {
		if recordHandshakeFailure && !errors.Is(err, io.EOF) {
			p.recordTLSFailure(conn, connectAuthority, started, err)
		}
		return
	}
	_ = conn.SetDeadline(time.Time{})
	if tlsConn.ConnectionState().NegotiatedProtocol == "h2" {
		p.handleHTTP2(tlsConn, connectAuthority, clientSNI)
		return
	}

	reader := bufio.NewReader(tlsConn)
	for {
		_ = tlsConn.SetReadDeadline(time.Now().Add(idleReadTimeout))
		req, err := http.ReadRequest(reader)
		if err != nil {
			return
		}
		_ = tlsConn.SetReadDeadline(time.Time{})
		record := p.currentPolicy().Decide(true, connectAuthority) == capture.Record
		if !p.handleRequest(tlsConn, req, "https", connectAuthority, clientSNI, record) {
			return
		}
	}
}

func (p *Proxy) spliceTunnel(conn net.Conn, authority string) {
	if _, _, err := net.SplitHostPort(authority); err != nil {
		writeResponse(conn, http.StatusBadGateway, "Bad Gateway\n")
		return
	}
	if p.isLoopbackTarget(&url.URL{Host: authority}) {
		writeResponse(conn, http.StatusBadGateway, selfProxyResponse)
		return
	}
	upstream, err := p.dialContext(context.Background(), "tcp", authority)
	if err != nil {
		if errors.Is(err, errSelfProxy) {
			writeResponse(conn, http.StatusBadGateway, selfProxyResponse)
			return
		}
		writeResponse(conn, http.StatusBadGateway, "Bad Gateway\n")
		return
	}
	if _, err := io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		_ = upstream.Close()
		return
	}
	_ = conn.SetDeadline(time.Time{})
	_ = upstream.SetDeadline(time.Time{})
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(upstream, conn)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(conn, upstream)
		done <- struct{}{}
	}()
	<-done
	_ = upstream.Close()
	_ = conn.Close()
}

// recordTLSFailure surfaces a failed MITM handshake (typically an untrusted
// proxy CA or certificate pinning) as a failed CONNECT Flow.
func (p *Proxy) recordTLSFailure(conn net.Conn, authority string, started time.Time, err error) {
	f := flow.Flow{
		ID:          uuid.NewString(),
		Scheme:      "https",
		Method:      http.MethodConnect,
		Host:        authority,
		HTTPVersion: "1.1",
		StartedAt:   started,
		ClientAddr:  conn.RemoteAddr().String(),
		Error:       "TLS handshake with client failed: " + err.Error(),
	}
	rec := p.beginRecord(&f, nil, 0, 0)
	p.finishRecord(rec, nil, nil)
}

func (p *Proxy) handleRequest(conn net.Conn, req *http.Request, scheme, defaultHost, upstreamSNI string, record bool) bool {
	clientBody := req.Body
	defer clientBody.Close()
	clientClose := req.Close
	websocket := isWebSocketRequest(req)

	host := defaultHost
	if host == "" {
		host = req.Host
	}
	p.clientRequest(conn, host)
	if defaultHost != "" {
		req.URL.Scheme = scheme
		req.URL.Host = defaultHost
	} else if req.URL.Scheme == "" {
		req.URL.Scheme = scheme
		if req.URL.Host == "" {
			req.URL.Host = host
		}
	}
	var f flow.Flow
	var rec *flowRecord
	var limit int64
	if record {
		f = flow.Flow{
			ID:          uuid.NewString(),
			Scheme:      scheme,
			Method:      req.Method,
			Host:        host,
			Path:        req.URL.RequestURI(),
			HTTPVersion: fmt.Sprintf("%d.%d", req.ProtoMajor, req.ProtoMinor),
			StartedAt:   time.Now(),
			ClientAddr:  conn.RemoteAddr().String(),
		}
		if addr, profile, ok := p.hostsForURL(req.URL); ok {
			f.UpstreamAddr = addr
			f.HostsProfile = profile
		}
		rec, limit = p.startFlow(req, &f)
	}
	if p.isLoopbackTarget(req.URL) {
		writeResponse(conn, http.StatusBadGateway, "Bad Gateway\n")
		if record {
			p.failUpstream(rec, errSelfProxy)
		}
		return false
	}
	prepareOutgoing(req, websocket)
	resp, err := p.roundTrip(req, upstreamSNI)
	if err != nil {
		if errors.Is(err, errSelfProxy) {
			writeResponse(conn, http.StatusBadGateway, selfProxyResponse)
		} else {
			writeResponse(conn, http.StatusBadGateway, "Bad Gateway\n")
		}
		if record {
			p.failUpstream(rec, err)
		}
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	if record {
		f.StatusCode = resp.StatusCode
		f.RespHeaders = copyHeaders(resp.Header)
	}
	if websocket && resp.StatusCode == http.StatusSwitchingProtocols {
		return p.tunnelWebSocket(conn, req, resp, rec)
	}
	var respCapture *bodyCapture
	if record {
		respCapture = p.openBodyCapture(limit)
		resp.Body = struct {
			io.Reader
			io.Closer
		}{Reader: io.TeeReader(resp.Body, respCapture), Closer: resp.Body}
	}
	closeAfter := normalizeForHTTP1Client(resp, req, clientClose)
	writeErr := resp.Write(conn)
	if writeErr != nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		if record {
			f.Error = writeErr.Error()
		}
	}
	_ = resp.Body.Close()
	resp.Body = http.NoBody
	if record {
		p.finishRecord(rec, resp.Header, respCapture)
	}
	return writeErr == nil && !closeAfter
}

func (p *Proxy) tunnelWebSocket(conn net.Conn, req *http.Request, resp *http.Response, rec *flowRecord) bool {
	removeHopHeadersPreserveUpgrade(resp.Header)
	resp.Proto = "HTTP/1.1"
	resp.ProtoMajor = 1
	resp.ProtoMinor = 1
	resp.Close = false
	upstreamConn, ok := resp.Body.(io.ReadWriteCloser)
	if !ok {
		_ = resp.Body.Close()
		resp.Body = http.NoBody
		if rec != nil {
			rec.f.Error = "websocket upgrade: upstream body is not duplex"
		}
		p.finishRecord(rec, nil, nil)
		writeResponse(conn, http.StatusBadGateway, "Bad Gateway\n")
		return false
	}
	resp.Body = http.NoBody
	if err := resp.Write(conn); err != nil {
		_ = upstreamConn.Close()
		if rec != nil {
			rec.f.Error = err.Error()
		}
		p.finishRecord(rec, nil, nil)
		return false
	}
	p.finishRecord(rec, resp.Header, nil)
	errCh := make(chan error, 2)
	go func() {
		_, err := io.Copy(upstreamConn, conn)
		errCh <- err
	}()
	go func() {
		_, err := io.Copy(conn, upstreamConn)
		errCh <- err
	}()
	<-errCh
	_ = upstreamConn.Close()
	return false
}

func isWebSocketRequest(req *http.Request) bool {
	return strings.EqualFold(req.Header.Get("Upgrade"), "websocket")
}

// normalizeForHTTP1Client rewrites an upstream response (HTTP/1.x or h2) so it
// can be serialized to an HTTP/1.1 client, and reports whether the client
// connection must be closed afterwards.
func normalizeForHTTP1Client(resp *http.Response, req *http.Request, clientClose bool) bool {
	removeHopHeaders(resp.Header)
	resp.Proto = "HTTP/1.1"
	resp.ProtoMajor = 1
	resp.ProtoMinor = 1
	resp.Trailer = nil

	method := req.Method
	http10 := req.ProtoMajor == 1 && req.ProtoMinor == 0
	hasBody := method != http.MethodHead && resp.StatusCode >= 200 &&
		resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotModified

	closeAfter := clientClose || (method == http.MethodHead && resp.ContentLength < 0)
	if hasBody && resp.ContentLength < 0 {
		if http10 {
			// HTTP/1.0 clients do not understand chunked encoding; close to delimit.
			resp.TransferEncoding = nil
			closeAfter = true
		} else {
			resp.TransferEncoding = []string{"chunked"}
		}
	} else {
		resp.TransferEncoding = nil
	}
	resp.Close = closeAfter
	return closeAfter
}

func (p *Proxy) startFlow(req *http.Request, f *flow.Flow) (*flowRecord, int64) {
	limit := p.maxBodyLimit()
	f.ReqHeaders = copyHeaders(req.Header)
	reqCap := p.streamRequestBody(req, limit)
	return p.beginRecord(f, reqCap, limit, req.ContentLength), limit
}

func (p *Proxy) failUpstream(rec *flowRecord, err error) {
	if rec == nil {
		return
	}
	rec.f.StatusCode = http.StatusBadGateway
	rec.f.Error = err.Error()
	p.finishRecord(rec, nil, nil)
}

func prepareOutgoing(req *http.Request, websocket bool) {
	req.RequestURI = ""
	req.Close = false
	if websocket {
		removeHopHeadersPreserveUpgrade(req.Header)
		return
	}
	removeHopHeaders(req.Header)
}

func removeHopHeaders(header http.Header) {
	for _, value := range header.Values("Connection") {
		for _, name := range strings.Split(value, ",") {
			if name = strings.TrimSpace(name); name != "" {
				header.Del(name)
			}
		}
	}
	for _, name := range hopHeaders {
		header.Del(name)
	}
}

func removeHopHeadersPreserveUpgrade(header http.Header) {
	// Strip hop-by-hop except Upgrade / Connection needed for WebSocket.
	for _, name := range []string{
		"Proxy-Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization",
		"Te", "Trailer", "Transfer-Encoding",
	} {
		header.Del(name)
	}
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
