package ogp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"
)

// ErrBlockedAddress is returned when DialContext refuses a connection
// because the resolved IP falls in a forbidden range (loopback, private,
// link-local, metadata, etc.). It is surfaced to callers so retry logic
// can treat it as non-retriable.
var ErrBlockedAddress = errors.New("ogp: address blocked by SSRF guard")

// ErrBlockedScheme is returned when the URL scheme isn't http or https.
var ErrBlockedScheme = errors.New("ogp: only http/https schemes allowed")

// ErrTooManyRedirects is returned when a fetch chain exceeds
// MaxRedirects.
var ErrTooManyRedirects = errors.New("ogp: too many redirects")

// MaxRedirects caps the redirect chain length.
const MaxRedirects = 5

// DefaultTimeout is the full-request timeout, including DNS + dial +
// TLS + body read.
const DefaultTimeout = 5 * time.Second

// blockedCIDRs is the set of IP ranges that the SSRF guard refuses. The
// list covers:
//   - loopback (IPv4 + IPv6)
//   - link-local (IPv4 + IPv6; also covers the AWS metadata service at
//     169.254.169.254)
//   - RFC1918 private ranges (IPv4)
//   - IPv6 unique-local (fc00::/7)
//   - unspecified / broadcast
//
// A hostile actor who controls DNS can still resolve a public name to a
// private IP on the first resolution, so we re-check the dialed IP
// here rather than trusting the name alone.
var blockedCIDRs = func() []*net.IPNet {
	cidrs := []string{
		"127.0.0.0/8",
		"::1/128",
		"169.254.0.0/16",
		"fe80::/10",
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"fc00::/7",
		"0.0.0.0/8",
		"255.255.255.255/32",
		"224.0.0.0/4", // IPv4 multicast
		"ff00::/8",    // IPv6 multicast
	}
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			panic(fmt.Sprintf("ogp: bad CIDR %q: %v", c, err))
		}
		out = append(out, n)
	}
	return out
}()

// IsBlockedIP reports whether ip falls into any of blockedCIDRs or is
// otherwise disallowed (unspecified, multicast without an explicit CIDR
// match, etc.). Exported for tests.
func IsBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	for _, n := range blockedCIDRs {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// NewSafeClient builds an *http.Client whose dialer refuses connections
// to any IP flagged by IsBlockedIP, and whose redirect policy re-applies
// the scheme check and limits the chain to MaxRedirects.
func NewSafeClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	base := &net.Dialer{
		Timeout:   timeout,
		KeepAlive: 15 * time.Second,
	}
	transport := &http.Transport{
		DialContext: safeDialContext(base),
		// Low ceilings: OGP fetches are one-shot, no need for pooling
		// across workers.
		MaxIdleConns:          4,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   timeout,
		ExpectContinueTimeout: 1 * time.Second,
		// Deny http proxy env vars — outbound proxy would bypass the
		// DialContext guard.
		Proxy: nil,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= MaxRedirects {
				return ErrTooManyRedirects
			}
			return checkURL(req.URL)
		},
	}
}

// safeDialContext wraps a *net.Dialer and rejects the connection if the
// resolved IP falls in a blocked range. It resolves the host explicitly
// and then dials by IP so DNS can't return one answer for the guard and
// another for the dial (DNS rebinding).
func safeDialContext(base *net.Dialer) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		ips, err := base.Resolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		for _, ip := range ips {
			if IsBlockedIP(ip.IP) {
				return nil, fmt.Errorf("%w: %s", ErrBlockedAddress, ip.IP)
			}
		}
		// Dial the first unblocked answer directly so a racy resolver
		// can't hand back a different IP than we validated.
		if len(ips) == 0 {
			return nil, fmt.Errorf("%w: no addresses", ErrBlockedAddress)
		}
		return base.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
	}
}

// checkURL ensures a URL's scheme is allowed and its hostname isn't a
// literal disallowed IP. (DNS names are validated at dial time.)
func checkURL(u *url.URL) error {
	if u == nil {
		return ErrBlockedScheme
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ErrBlockedScheme
	}
	host := u.Hostname()
	if ip := net.ParseIP(host); ip != nil && IsBlockedIP(ip) {
		return fmt.Errorf("%w: %s", ErrBlockedAddress, ip)
	}
	return nil
}
