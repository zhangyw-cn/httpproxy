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

	"httpproxy/internal/capture"
	"httpproxy/internal/cert"
	"httpproxy/internal/clients"
	"httpproxy/internal/hosts"
)

const selfProxyMessage = "refusing to proxy to the proxy's own listen address"

var errSelfProxy = errors.New(selfProxyMessage)

const selfProxyResponse = selfProxyMessage + "\n"

const (
	maxIdleConnsTotal   = 128
	maxIdleConnsPerHost = 6
	sessionCacheEntries = 128
	maxSNITransports    = 32
)

type sniTransport struct {
	key string
	tr  *http.Transport
}

type Proxy struct {
	addr    string
	sink    FlowSink
	maxBody int64
	ca      *cert.Manager
	policy  capture.Policy

	// UpstreamTLS is nil in production so system roots are used. Its
	// NextProtos are ignored: upstream ALPN always offers h2 and http/1.1.
	UpstreamTLS *tls.Config

	mu                       sync.Mutex
	listener                 net.Listener
	conns                    map[net.Conn]struct{}
	closed                   bool
	tracker                  *clients.Tracker
	transport                *http.Transport
	upstreamDisableKeepAlive bool
	hostsName                string
	hostsTable               hosts.Table
	sessions                 tls.ClientSessionCache
	sniTransports            []sniTransport
	recordQueue              int
	recordGate               chan struct{}
	jobs                     chan recordJob
	recStarted               bool
	recStop                  sync.Once
	recWG                    sync.WaitGroup
	handlerWG                sync.WaitGroup

	// bodyCaptureHook, when set by a test, runs once per body this proxy captures.
	bodyCaptureHook func()
}

func New(addr string, sink FlowSink, maxBody int64, ca *cert.Manager) *Proxy {
	if maxBody < 0 {
		maxBody = 0
	}
	return &Proxy{
		addr:        addr,
		sink:        sink,
		maxBody:     maxBody,
		ca:          ca,
		policy:      capture.Default(),
		conns:       make(map[net.Conn]struct{}),
		recordQueue: defaultRecordQueue,
	}
}

func (p *Proxy) openBodyCapture(max int64) *bodyCapture {
	if p.bodyCaptureHook != nil {
		p.bodyCaptureHook()
	}
	return newBodyCapture(max)
}

func (p *Proxy) SetPolicy(policy capture.Policy) {
	p.mu.Lock()
	p.policy = policy
	p.mu.Unlock()
}

// SetHosts installs the hosts profile used for new upstream dials. Idle
// upstream connections are closed so the next request dials the new target;
// requests and tunnels already in flight are not interrupted.
func (p *Proxy) SetHosts(name string, table hosts.Table) {
	p.mu.Lock()
	p.hostsName = name
	p.hostsTable = table
	transport := p.transport
	sni := append([]sniTransport(nil), p.sniTransports...)
	p.mu.Unlock()

	if transport != nil {
		transport.CloseIdleConnections()
	}
	for _, entry := range sni {
		entry.tr.CloseIdleConnections()
	}
}

func (p *Proxy) currentPolicy() capture.Policy {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.policy
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

// SetClientTracker installs the client tracker that records accepted
// connections, forwarded requests, and CONNECT tunnels. A nil tracker disables
// tracking; it is never required for proxying.
func (p *Proxy) SetClientTracker(t *clients.Tracker) {
	p.mu.Lock()
	p.tracker = t
	p.mu.Unlock()
}

func (p *Proxy) clientTracker() *clients.Tracker {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.tracker
}

// clientRequest counts one forwarded request from conn's client to host.
func (p *Proxy) clientRequest(conn net.Conn, host string) {
	p.clientRequestAddr(remoteIP(conn.RemoteAddr()), host)
}

// clientRequestAddr counts one forwarded request from a raw RemoteAddr.
func (p *Proxy) clientRequestAddr(remoteAddr, host string) {
	tracker := p.clientTracker()
	if tracker == nil {
		return
	}
	tracker.Request(addrIP(remoteAddr), hostWithoutPort(host))
}

// clientTunnel counts one CONNECT tunnel from conn's client to host.
func (p *Proxy) clientTunnel(conn net.Conn, host string) {
	tracker := p.clientTracker()
	if tracker == nil {
		return
	}
	tracker.Tunnel(remoteIP(conn.RemoteAddr()), hostWithoutPort(host))
}

// remoteIP returns the IP part of a remote address. It never fails: a bare
// address is returned unchanged, and a nil addr yields an empty IP.
func remoteIP(addr net.Addr) string {
	if addr == nil {
		return ""
	}
	return addrIP(addr.String())
}

// addrIP returns the IP part of "host:port", or addr unchanged when it has no
// port. A tracker ignores the empty result.
func addrIP(addr string) string {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}

func (p *Proxy) baseTLS() *tls.Config {
	if p.UpstreamTLS != nil {
		return p.UpstreamTLS.Clone()
	}
	return &tls.Config{}
}

func (p *Proxy) sessionCache() tls.ClientSessionCache {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.sessions == nil {
		p.sessions = tls.NewLRUClientSessionCache(sessionCacheEntries)
	}
	return p.sessions
}

// hostsLookup resolves hostname through the active hosts table. IP literals are
// never overridden. It reports the mapped IP and the profile name.
func (p *Proxy) hostsLookup(hostname string) (net.IP, string, bool) {
	if hostname == "" || net.ParseIP(hostname) != nil {
		return nil, "", false
	}
	p.mu.Lock()
	table, name := p.hostsTable, p.hostsName
	p.mu.Unlock()
	ip, ok := table.Lookup(hostname)
	if !ok {
		return nil, "", false
	}
	return ip, name, true
}

// hostsForURL reports the mapped dial target for u and the profile that
// supplied it. The port always comes from u: an explicit port, else the scheme
// default. IP-literal URLs and unknown schemes are never overridden.
func (p *Proxy) hostsForURL(u *url.URL) (addr, profile string, ok bool) {
	if u == nil {
		return "", "", false
	}
	port := u.Port()
	if port == "" {
		switch strings.ToLower(u.Scheme) {
		case "http":
			port = "80"
		case "https":
			port = "443"
		default:
			return "", "", false
		}
	}
	ip, name, ok := p.hostsLookup(u.Hostname())
	if !ok {
		return "", "", false
	}
	return net.JoinHostPort(ip.String(), port), name, true
}

// dialContext dials the TCP target for addr. Only the dial address is rewritten
// by hosts: callers keep the original URL, Host header, and TLS ServerName.
// A mapped target that is this proxy's own listener is rejected.
func (p *Proxy) dialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	target := addr
	if host, port, err := net.SplitHostPort(addr); err == nil {
		if ip, _, ok := p.hostsLookup(host); ok {
			target = net.JoinHostPort(ip.String(), port)
		}
	}
	if p.isSelfAddr(target) {
		return nil, errSelfProxy
	}
	d := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	return d.DialContext(ctx, network, target)
}

func (p *Proxy) buildTransport(base *tls.Config, maxIdle, perHost int) *http.Transport {
	if base == nil {
		base = &tls.Config{}
	}
	p.mu.Lock()
	disableKeepAlive := p.upstreamDisableKeepAlive
	p.mu.Unlock()
	cfg := base.Clone()
	cfg.NextProtos = []string{"h2", "http/1.1"}
	cfg.ClientSessionCache = p.sessionCache()
	return &http.Transport{
		DialContext:           p.dialContext,
		TLSClientConfig:       cfg,
		ForceAttemptHTTP2:     true,
		DisableCompression:    true,
		DisableKeepAlives:     disableKeepAlive,
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

	p.ensureRecorder()

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
		p.handlerWG.Add(1)
		tracker := p.tracker
		p.mu.Unlock()

		clientIP := remoteIP(conn.RemoteAddr())
		if tracker != nil {
			tracker.AddConn(clientIP)
		}

		go func() {
			defer p.handlerWG.Done()
			if tracker != nil {
				defer tracker.DropConn(clientIP)
			}
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
	sni := append([]sniTransport(nil), p.sniTransports...)
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
	for _, entry := range sni {
		entry.tr.CloseIdleConnections()
	}
	p.handlerWG.Wait()
	p.stopRecorder()
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

func (p *Proxy) touchSNI(key string) *http.Transport {
	for i := range p.sniTransports {
		if p.sniTransports[i].key != key {
			continue
		}
		entry := p.sniTransports[i]
		copy(p.sniTransports[i:], p.sniTransports[i+1:])
		p.sniTransports[len(p.sniTransports)-1] = entry
		return entry.tr
	}
	return nil
}

func (p *Proxy) sniTransport(dialHost, sniHost string) *http.Transport {
	key := dialHost + "\n" + sniHost
	p.mu.Lock()
	if tr := p.touchSNI(key); tr != nil {
		p.mu.Unlock()
		return tr
	}
	p.mu.Unlock()

	base := p.baseTLS()
	base.ServerName = sniHost
	tr := p.buildTransport(base, maxIdleConnsPerHost, maxIdleConnsPerHost)
	tr.DialTLSContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		raw, err := p.dialContext(ctx, network, dialHost)
		if err != nil {
			return nil, err
		}
		tc := tls.Client(raw, tr.TLSClientConfig)
		hsCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if err := tc.HandshakeContext(hsCtx); err != nil {
			_ = raw.Close()
			return nil, err
		}
		return tc, nil
	}

	p.mu.Lock()
	if existing := p.touchSNI(key); existing != nil {
		p.mu.Unlock()
		tr.CloseIdleConnections()
		return existing
	}
	var evict *http.Transport
	if len(p.sniTransports) == maxSNITransports {
		evict = p.sniTransports[0].tr
		p.sniTransports = p.sniTransports[1:]
	}
	p.sniTransports = append(p.sniTransports, sniTransport{key: key, tr: tr})
	p.mu.Unlock()
	if evict != nil {
		evict.CloseIdleConnections()
	}
	return tr
}

// roundTrip forwards req. When upstreamSNI is set (HTTPS MITM with a DNS SNI
// while CONNECT targeted an IP), TLS uses that name for certificate verification
// while still dialing req.URL.Host.
func (p *Proxy) roundTrip(req *http.Request, upstreamSNI string) (*http.Response, error) {
	if upstreamSNI == "" || !strings.EqualFold(req.URL.Scheme, "https") {
		return p.upstream().RoundTrip(req)
	}
	dialHost := req.URL.Host
	sniHost := hostWithoutPort(upstreamSNI)
	if sniHost == "" || strings.EqualFold(sniHost, hostWithoutPort(dialHost)) {
		return p.upstream().RoundTrip(req)
	}
	return p.sniTransport(dialHost, sniHost).RoundTrip(req)
}

// isSelfAddr reports whether hostport names this proxy's own listener.
func (p *Proxy) isSelfAddr(hostport string) bool {
	listening, configured := p.selfAddrs()
	if listening == "" || hostport == "" {
		return false
	}
	return sameEndpoint(hostport, listening, configured)
}

// selfAddrs returns the bound listener address and the configured address, or
// empty strings before the proxy starts listening.
func (p *Proxy) selfAddrs() (listening, configured string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.listener == nil {
		return "", ""
	}
	return p.listener.Addr().String(), p.addr
}

func (p *Proxy) isLoopbackTarget(u *url.URL) bool {
	if u == nil || u.Host == "" {
		return false
	}
	if p.isSelfAddr(u.Host) {
		return true
	}
	// Compare host:port when addr is ":8080" style vs concrete.
	_, configured := p.selfAddrs()
	return configured != "" && u.Host == configured
}

// sameEndpoint reports whether target names the listener at listening.
//
// A wildcard listener (its host is empty, 0.0.0.0, or ::) accepts connections on
// every local address, so loopback, unspecified, and interface addresses count
// as self when the port matches. A listener bound to a specific address matches
// only that address, which keeps a different loopback alias on the same port
// reachable. An unspecified target is self either way: connecting to 0.0.0.0 or
// :: lands on the local host.
func sameEndpoint(target, listening, configured string) bool {
	th, tp, err := net.SplitHostPort(target)
	if err != nil {
		return target == listening || (configured != "" && target == configured)
	}
	sh, sp, err := net.SplitHostPort(listening)
	if err != nil {
		sh, sp, err = net.SplitHostPort(configured)
		if err != nil {
			return false
		}
	}
	if tp != sp {
		return false
	}

	wildcard := isWildcardHost(sh)
	if ch, cp, ok := splitEndpoint(configured); ok && isWildcardHost(ch) && (cp == sp || cp == "0") {
		wildcard = true
	}

	tip := net.ParseIP(th)
	if tip != nil && tip.IsUnspecified() {
		return true
	}
	if tip == nil {
		return th == sh
	}
	if wildcard {
		return isLocalAddrIP(tip)
	}
	sip := net.ParseIP(sh)
	return sip != nil && tip.Equal(sip)
}

func splitEndpoint(addr string) (host, port string, ok bool) {
	host, port, err := net.SplitHostPort(addr)
	return host, port, err == nil
}

func isWildcardHost(host string) bool {
	if host == "" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsUnspecified()
}

// isLocalAddrIP reports whether ip is assigned to a local interface; a wildcard
// listener is reachable on all of them.
func isLocalAddrIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsUnspecified() {
		return true
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return false
	}
	for _, addr := range addrs {
		if ifnet, ok := addr.(*net.IPNet); ok && ifnet.IP.Equal(ip) {
			return true
		}
	}
	return false
}
