package scrape

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"syscall"
	"time"
)

// Listing URLs come from whatever the user (or a page, or an agent) pastes, and
// braveduck fetches them from the user's own machine. Without a guard, a pasted
// http://127.0.0.1:… or http://192.168.1.1/… turns the board into a proxy onto
// the user's localhost and LAN. Job boards are public sites, so only public
// unicast addresses are ever fetched.

// ErrNonPublicURL is returned for a URL that isn't public http(s).
var ErrNonPublicURL = errors.New("only public http(s) job links can be fetched")

// cgnat is 100.64.0.0/10 (carrier-grade NAT), which netip doesn't flag as private.
var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// isPublicAddr reports whether ip is a globally-routable unicast address.
func isPublicAddr(ip netip.Addr) bool {
	ip = ip.Unmap()
	return ip.IsValid() &&
		ip.IsGlobalUnicast() &&
		!ip.IsPrivate() &&
		!ip.IsLoopback() &&
		!ip.IsLinkLocalUnicast() &&
		!cgnat.Contains(ip)
}

// lookupIP is the resolver CheckPublicURL uses; overridable in tests.
var lookupIP = net.DefaultResolver.LookupNetIP

// CheckPublicURL rejects anything that isn't an http(s) URL whose host resolves
// only to public addresses. It is the up-front check (so the caller can answer
// 400); publicClient re-checks at dial time, which also covers redirects and
// DNS that changes between the check and the fetch.
func CheckPublicURL(ctx context.Context, raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return ErrNonPublicURL
	}
	host := u.Hostname()
	if ip, err := netip.ParseAddr(host); err == nil {
		if !isPublicAddr(ip) {
			return ErrNonPublicURL
		}
		return nil
	}
	ips, err := lookupIP(ctx, "ip", host)
	if err != nil {
		return fmt.Errorf("can't resolve %s: %w", host, err)
	}
	for _, ip := range ips {
		if !isPublicAddr(ip) {
			return ErrNonPublicURL
		}
	}
	return nil
}

// publicClient is an HTTP client that refuses to connect to non-public
// addresses. The check runs on the resolved IP at connect time, so redirects to
// internal hosts and DNS rebinding are blocked too.
var publicClient = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		DialContext: (&net.Dialer{
			Timeout: 10 * time.Second,
			Control: func(_, address string, _ syscall.RawConn) error {
				ap, err := netip.ParseAddrPort(address)
				if err != nil || !isPublicAddr(ap.Addr()) {
					return ErrNonPublicURL
				}
				return nil
			},
		}).DialContext,
		TLSHandshakeTimeout: 10 * time.Second,
		MaxIdleConns:        10,
		IdleConnTimeout:     60 * time.Second,
	},
}
