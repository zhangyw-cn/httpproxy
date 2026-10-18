package proxy

import (
	"crypto/tls"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
	"golang.org/x/net/http2"

	"httpproxy/internal/flow"
)

func (p *Proxy) handleHTTP2(conn *tls.Conn, connectAuthority string) {
	server := &http2.Server{}
	server.ServeConn(conn, &http2.ServeConnOpts{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			p.handleHTTP2Request(w, req, connectAuthority)
		}),
	})
}

func (p *Proxy) handleHTTP2Request(w http.ResponseWriter, req *http.Request, connectAuthority string) {
	started := time.Now()
	clientBody := req.Body
	if clientBody != nil {
		defer clientBody.Close()
	}

	f := flow.Flow{
		ID:          uuid.NewString(),
		Scheme:      "https",
		Method:      req.Method,
		Host:        connectAuthority,
		Path:        req.URL.RequestURI(),
		HTTPVersion: "2",
		StartedAt:   started,
		ClientAddr:  req.RemoteAddr,
	}
	reqCapture, readErr := p.startFlow(req, &f)
	if readErr != nil {
		f.StatusCode = http.StatusBadRequest
		f.Error = readErr.Error()
		finishFlow(&f, started)
		p.updated(f)
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	req.URL.Scheme = "https"
	req.URL.Host = connectAuthority
	prepareOutgoing(req)

	resp, err := p.upstream().RoundTrip(req)
	if err != nil {
		p.failUpstream(&f, reqCapture, req, err)
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	f.StatusCode = resp.StatusCode
	f.RespHeaders = copyHeaders(resp.Header)
	removeHopHeaders(resp.Header)
	for name, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(name, value)
		}
	}
	w.WriteHeader(resp.StatusCode)

	respCapture := newBodyCapture(p.maxBody)
	_, writeErr := io.Copy(w, io.TeeReader(resp.Body, respCapture))
	if writeErr != nil {
		_, _ = io.Copy(respCapture, resp.Body)
		f.Error = writeErr.Error()
	}
	p.finishResponse(&f, reqCapture, req, resp, respCapture)
}
