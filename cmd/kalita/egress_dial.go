package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"
)

// Connection-level egress policy (LIM-9). The allowlist authorizes a host
// NAME; this decides which addresses that name may actually reach. The name is
// resolved once per connection and the connection is made to a vetted address
// (resolve-and-pin), so a DNS answer that flips to an internal address
// (rebinding) cannot turn an allowlisted domain into a path to the node's own
// network.
//
//   - an IP literal in the URL already matched the allowlist exactly: it is the
//     operator's explicit choice and is dialed as is (not unspecified/multicast);
//   - a name may resolve to public addresses and to loopback (traffic to the
//     local machine never crosses a network);
//   - private, link-local (cloud metadata), CGNAT and IPv6 ULA addresses are
//     refused unless the operator opted in with KALITA_EGRESS_ALLOW_PRIVATE=1;
//   - a configured HTTP(S)_PROXY is dialed as is: the proxy is then the egress
//     control point.
type egressPolicy struct {
	allowPrivate bool
	proxies      map[string]bool // host:port of HTTP_PROXY/HTTPS_PROXY
	// lookup resolves a host name; nil uses the system resolver (tests stub it)
	lookup func(ctx context.Context, host string) ([]net.IPAddr, error)
}

// egressTargetError is a refusal by the policy: permanent, never retried.
type egressTargetError struct {
	host string
	ips  []net.IP
}

func (e *egressTargetError) Error() string {
	return fmt.Sprintf("egress to %s refused: it resolves only to non-public addresses %v (set KALITA_EGRESS_ALLOW_PRIVATE=1 to allow private networks)", e.host, e.ips)
}

var cgnat = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

// ipAllowed reports whether a resolved address may be dialed for a host name.
func (p egressPolicy) ipAllowed(ip net.IP) bool {
	switch {
	case ip == nil, ip.IsUnspecified(), ip.IsMulticast():
		return false
	case ip.IsLoopback():
		return true
	case ip.IsPrivate(), ip.IsLinkLocalUnicast(), ip.IsLinkLocalMulticast(), cgnat.Contains(ip):
		return p.allowPrivate
	}
	return true
}

func (p egressPolicy) dialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	d := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	if p.proxies[strings.ToLower(addr)] {
		return d.DialContext(ctx, network, addr)
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsUnspecified() || ip.IsMulticast() {
			return nil, &egressTargetError{host: host, ips: []net.IP{ip}}
		}
		return d.DialContext(ctx, network, addr)
	}
	lookup := p.lookup
	if lookup == nil {
		lookup = net.DefaultResolver.LookupIPAddr
	}
	addrs, err := lookup(ctx, host)
	if err != nil {
		return nil, err
	}
	var all []net.IP
	var lastErr error
	for _, a := range addrs {
		all = append(all, a.IP)
		if !p.ipAllowed(a.IP) {
			continue
		}
		conn, err := d.DialContext(ctx, network, net.JoinHostPort(a.IP.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, &egressTargetError{host: host, ips: all}
}

// isEgressRefusal reports whether a client error is a policy refusal.
func isEgressRefusal(err error) bool {
	var te *egressTargetError
	return errors.As(err, &te)
}

// envProxies collects the proxy endpoints the transport may dial directly.
func envProxies() map[string]bool {
	out := map[string]bool{}
	for _, k := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy"} {
		v := strings.TrimSpace(os.Getenv(k))
		if v == "" {
			continue
		}
		if !strings.Contains(v, "://") {
			v = "http://" + v
		}
		u, err := url.Parse(v)
		if err != nil || u.Hostname() == "" {
			continue
		}
		port := u.Port()
		if port == "" {
			port = "80"
			if u.Scheme == "https" {
				port = "443"
			}
		}
		out[strings.ToLower(net.JoinHostPort(u.Hostname(), port))] = true
	}
	return out
}

// isASCIIHost reports whether a host is plain ASCII. Non-ASCII (IDN) hosts are
// refused: a homograph reads like a familiar domain, and the allowlist holds
// the punycode (xn--) form instead.
func isASCIIHost(h string) bool {
	for i := 0; i < len(h); i++ {
		if h[i] >= 0x80 {
			return false
		}
	}
	return true
}
