package proxy

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"httpproxy/internal/cert"
)

var errSelfProxy = errors.New("refusing to proxy to the proxy's own listen address")

const (
	maxIdleConnsTotal   = 128
	maxIdleConnsPerHost = 6
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

// SetMaxBody sets the body capture cap for flows that start after this call.
// A negative n is stored as 0. A flow already started keeps the limit from startFlow.
func (p *Proxy) SetMaxBody(n int64) {
	if n < 0 {
		n = 0
	}
	p.mu.Lock()
	p.maxBody = n
	p.mu.Unlock()
}

func (p *Proxy) maxBodyLimit() int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.maxBody
}

func (p *Proxy) baseTLS() *tls.Config {
	if p.UpstreamTLS != nil {
		return p.UpstreamTLS.Clone()
	}
	return &tls.Config{}
}

func (p *Proxy) buildTransport(base *tls.Config, maxIdle, perHost int) *http.Transport {
	if base == nil {
		base = &tls.Config{}
	}
	cfg := base.Clone()
	cfg.NextProtos = []string{"h2", "http/1.1"}
	return &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSClientConfig:       cfg,
		ForceAttemptHTTP2:     true,
		DisableCompression:    true,
		MaxIdleConns:          maxIdle,
		MaxIdleConnsPerHost:   perHost,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}

// upstream returns the shared client transport, built lazily so tests can set
// UpstreamTLS after New. It negotiates h2 or HTTP/1.1 per upstream server,
// independently of the protocol the client spoke to the proxy.
func (p *Proxy) upstream() *http.Transport {
	p.mu.Lock()
	tr := p.transport
	base := p.baseTLS()
	p.mu.Unlock()
	if tr != nil {
		return tr
	}
	built := p.buildTransport(base, maxIdleConnsTotal, maxIdleConnsPerHost)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.transport == nil {
		p.transport = built
		return built
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

// roundTrip forwards req. When upstreamSNI is set (HTTPS MITM with a DNS SNI
// while CONNECT targeted an IP), TLS uses that name for certificate verification
// while still dialing req.URL.Host.
func (p *Proxy) roundTrip(req *http.Request, upstreamSNI string) (*http.Response, error) {
	tr := p.upstream()
	if upstreamSNI == "" || !strings.EqualFold(req.URL.Scheme, "https") {
		return tr.RoundTrip(req)
	}
	dialHost := req.URL.Host
	sniHost := hostWithoutPort(upstreamSNI)
	if sniHost == "" || strings.EqualFold(sniHost, hostWithoutPort(dialHost)) {
		return tr.RoundTrip(req)
	}
	baseTLS := tr.TLSClientConfig
	if baseTLS == nil {
		baseTLS = &tls.Config{}
	}
	tlsCfg := baseTLS.Clone()
	tlsCfg.ServerName = sniHost
	tlsCfg.NextProtos = []string{"h2", "http/1.1"}
	custom := &http.Transport{
		Proxy:                 tr.Proxy,
		DialContext:           tr.DialContext,
		ForceAttemptHTTP2:     true,
		DisableCompression:    true,
		MaxIdleConns:          tr.MaxIdleConns,
		IdleConnTimeout:       tr.IdleConnTimeout,
		TLSHandshakeTimeout:   tr.TLSHandshakeTimeout,
		ExpectContinueTimeout: tr.ExpectContinueTimeout,
		TLSClientConfig:       tlsCfg,
		DialTLSContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			d := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
			raw, err := d.DialContext(ctx, network, dialHost)
			if err != nil {
				return nil, err
			}
			tc := tls.Client(raw, tlsCfg)
			if err := tc.HandshakeContext(ctx); err != nil {
				_ = raw.Close()
				return nil, err
			}
			return tc, nil
		},
	}
	return custom.RoundTrip(req)
}

func (p *Proxy) isLoopbackTarget(u *url.URL) bool {
	if u == nil {
		return false
	}
	target := u.Host
	if target == "" {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.listener == nil {
		return false
	}
	self := p.listener.Addr().String()
	if target == self || target == p.addr {
		return true
	}
	// Compare host:port when addr is ":8080" style vs concrete.
	th, tp, err1 := net.SplitHostPort(target)
	sh, sp, err2 := net.SplitHostPort(self)
	if err1 != nil || err2 != nil {
		return false
	}
	if tp != sp {
		return false
	}
	if th == "" || th == "0.0.0.0" || th == "::" {
		return false
	}
	return th == sh || (isLocalIP(th) && isLocalIP(sh))
}

func isLocalIP(host string) bool {
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
