package proxy

import (
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"sync"

	"golang.org/x/net/http2"

	"httpproxy/internal/cert"
)

type Proxy struct {
	addr    string
	sink    FlowSink
	maxBody int64
	ca      *cert.Manager

	// UpstreamTLS is nil in production so system roots are used.
	UpstreamTLS *tls.Config

	mu          sync.Mutex
	listener    net.Listener
	conns       map[net.Conn]struct{}
	closed      bool
	transport   *http.Transport
	h2Transport *http2.Transport
}

func New(addr string, sink FlowSink, maxBody int64, ca *cert.Manager) *Proxy {
	if maxBody < 0 {
		maxBody = 0
	}
	return &Proxy{
		addr:    addr,
		sink:    sink,
		maxBody: maxBody,
		ca:      ca,
		conns:   make(map[net.Conn]struct{}),
		transport: &http.Transport{
			DisableKeepAlives:  true,
			DisableCompression: true,
		},
	}
}

func (p *Proxy) ListenAndServe() error {
	listener, err := net.Listen("tcp", p.addr)
	if err != nil {
		return err
	}

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		_ = listener.Close()
		return nil
	}
	p.listener = listener
	p.mu.Unlock()

	for {
		conn, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}

		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			_ = conn.Close()
			return nil
		}
		p.conns[conn] = struct{}{}
		p.mu.Unlock()

		go func() {
			defer func() {
				_ = conn.Close()
				p.mu.Lock()
				delete(p.conns, conn)
				p.mu.Unlock()
			}()
			p.handleHTTP1(conn)
		}()
	}
}

func (p *Proxy) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	listener := p.listener
	conns := make([]net.Conn, 0, len(p.conns))
	for conn := range p.conns {
		conns = append(conns, conn)
	}
	h2Transport := p.h2Transport
	p.mu.Unlock()

	var closeErr error
	if listener != nil {
		closeErr = listener.Close()
	}
	for _, conn := range conns {
		_ = conn.Close()
	}
	p.transport.CloseIdleConnections()
	if h2Transport != nil {
		h2Transport.CloseIdleConnections()
	}
	return closeErr
}

func (p *Proxy) Addr() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.listener == nil {
		return ""
	}
	return p.listener.Addr().String()
}
