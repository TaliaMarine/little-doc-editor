// Package clientip resolves the real client address of a request behind
// reverse proxies. X-Forwarded-For is attacker-controlled unless the
// request really came through a proxy you operate, so it is honored only
// when the TCP peer is inside the configured trusted-proxy set
// (TRUSTED_PROXIES); otherwise the peer address is the answer.
package clientip

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

type Resolver struct {
	trusted []netip.Prefix
}

// New parses CIDRs or bare IPs. An empty list yields a resolver that always
// returns the TCP peer address.
func New(trusted []string) (*Resolver, error) {
	r := &Resolver{}
	for _, t := range trusted {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if p, err := netip.ParsePrefix(t); err == nil {
			r.trusted = append(r.trusted, p.Masked())
			continue
		}
		a, err := netip.ParseAddr(t)
		if err != nil {
			return nil, fmt.Errorf("TRUSTED_PROXIES entry %q is not a CIDR or IP", t)
		}
		r.trusted = append(r.trusted, netip.PrefixFrom(a.Unmap(), a.Unmap().BitLen()))
	}
	return r, nil
}

func (r *Resolver) isTrusted(a netip.Addr) bool {
	a = a.Unmap()
	for _, p := range r.trusted {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// IP returns the client IP for req: the TCP peer, unless the peer is a
// trusted proxy, in which case X-Forwarded-For is walked right to left and
// the first address that is not itself a trusted proxy wins (so a client
// can't spoof by prepending entries). Falls back to the peer when the
// header is absent or malformed.
func (r *Resolver) IP(req *http.Request) string {
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		host = req.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil || r == nil || !r.isTrusted(peer) {
		return host
	}
	hops := strings.Split(strings.Join(req.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		hop := strings.TrimSpace(hops[i])
		a, err := netip.ParseAddr(hop)
		if err != nil {
			// Some load balancers append "ip:port".
			ap, perr := netip.ParseAddrPort(hop)
			if perr != nil {
				return host // malformed chain: don't guess
			}
			a = ap.Addr()
		}
		if !r.isTrusted(a) {
			return a.Unmap().String()
		}
	}
	return host
}
