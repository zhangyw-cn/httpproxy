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

func (p *Proxy) handleHTTP2(conn *tls.Conn, connectAuthority, upstreamSNI string) {
	server := &http2.Server{}
	server.ServeConn(conn, &http2.ServeConnOpts{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			p.handleHTTP2Request(w, req, connectAuthority, upstreamSNI)
		}),
	})
}

func (p *Proxy) handleHTTP2Request(w http.ResponseWriter, req *http.Request, connectAuthority, upstreamSNI string) {
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
	rec, limit := p.startFlow(req, &f)
	req.URL.Scheme = "https"
	req.URL.Host = connectAuthority
	if p.isLoopbackTarget(req.URL) {
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		p.failUpstream(rec, errSelfProxy)
		return
	}
	prepareOutgoing(req, false)
	resp, err := p.roundTrip(req, upstreamSNI)
	if err != nil {
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		p.failUpstream(rec, err)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	f.StatusCode = resp.StatusCode
	f.RespHeaders = copyHeaders(resp.Header)
	removeHopHeaders(resp.Header)
	for name, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(name, value)
		}
	}
	w.WriteHeader(resp.StatusCode)
	respCapture := newBodyCapture(limit)
	writeErr := copyAndFlush(w, io.TeeReader(resp.Body, respCapture))
	if writeErr != nil {
		_, _ = io.Copy(respCapture, resp.Body)
		f.Error = writeErr.Error()
	}
	_ = resp.Body.Close()
	resp.Body = http.NoBody
	p.finishRecord(rec, resp.Header, respCapture)
}

func copyAndFlush(w http.ResponseWriter, r io.Reader) error {
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 32*1024)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return werr
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}
