package proxy

import (
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"httpproxy/internal/cert"
)

type Proxy struct {
	addr    string
	sink    FlowSink
	maxBody int64
	ca      *cert.Manager

	// UpstreamTLS is nil in production so system roots are used. Its
	// NextProtos are ignored: upstream ALPN always offers h2 and http/1.1.
	UpstreamTLS *tls.Config

	mu        sync.Mutex
	listener  net.Listener
	conns     map[net.Conn]struct{}
	closed    bool
	transport *http.Transport
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
	}
}

// upstream returns the shared client transport, built lazily so tests can set
// UpstreamTLS after New. It negotiates h2 or HTTP/1.1 per upstream server,
// independently of the protocol the client spoke to the proxy.
func (p *Proxy) upstream() *http.Transport {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.transport == nil {
		tlsConfig := &tls.Config{}
		if p.UpstreamTLS != nil {
			tlsConfig = p.UpstreamTLS.Clone()
		}
		tlsConfig.NextProtos = []string{"h2", "http/1.1"}
		p.transport = &http.Transport{
			DialContext:           (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			TLSClientConfig:       tlsConfig,
			ForceAttemptHTTP2:     true,
			DisableCompression:    true,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		}
	}
	return p.transport
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
	transport := p.transport
	p.mu.Unlock()

	var closeErr error
	if listener != nil {
		closeErr = listener.Close()
	}
	for _, conn := range conns {
		_ = conn.Close()
	}
	if transport != nil {
		transport.CloseIdleConnections()
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
