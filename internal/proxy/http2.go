package proxy

import (
	"bytes"
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
	requestBody, readErr := io.ReadAll(req.Body)
	_ = req.Body.Close()
	requestCapture := newBodyCapture(p.maxBody)
	_, _ = requestCapture.Write(requestBody)

	f := flow.Flow{
		ID:          uuid.NewString(),
		Scheme:      "https",
		Method:      req.Method,
		Host:        connectAuthority,
		Path:        req.URL.RequestURI(),
		HTTPVersion: "2",
		ReqHeaders:  copyHeaders(req.Header),
		StartedAt:   started,
		ClientAddr:  req.RemoteAddr,
	}
	f.ReqBody, f.ReqSize, f.ReqTruncated = requestCapture.result()
	p.created(f)

	if readErr != nil {
		f.StatusCode = http.StatusBadRequest
		f.Error = readErr.Error()
		finishFlow(&f, started)
		p.updated(f)
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	req.Body = io.NopCloser(bytes.NewReader(requestBody))
	req.RequestURI = ""
	req.URL.Scheme = "https"
	req.URL.Host = connectAuthority

	resp, err := p.roundTripHTTP2(req)
	if err != nil {
		f.StatusCode = http.StatusBadGateway
		f.Error = err.Error()
		finishFlow(&f, started)
		p.updated(f)
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	f.StatusCode = resp.StatusCode
	f.RespHeaders = copyHeaders(resp.Header)
	for name, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(name, value)
		}
	}
	w.WriteHeader(resp.StatusCode)

	responseCapture := newBodyCapture(p.maxBody)
	_, writeErr := io.Copy(w, io.TeeReader(resp.Body, responseCapture))
	if writeErr != nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		f.Error = writeErr.Error()
	}
	f.RespBody, f.RespSize, f.RespTruncated = responseCapture.result()
	finishFlow(&f, started)
	p.updated(f)
}

func (p *Proxy) roundTripHTTP2(req *http.Request) (*http.Response, error) {
	tlsConfig := &tls.Config{}
	if p.UpstreamTLS != nil {
		tlsConfig = p.UpstreamTLS.Clone()
	}
	transport := &http2.Transport{
		TLSClientConfig:    tlsConfig,
		DisableCompression: true,
	}
	defer transport.CloseIdleConnections()
	return transport.RoundTrip(req)
}
