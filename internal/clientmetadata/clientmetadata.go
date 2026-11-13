// Package clientmetadata fetches OAuth client ID metadata documents: the
// wire adapter and its server-side request forgery guards. Which documents
// identify a client is oauthservice policy.
package clientmetadata

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"syscall"
	"time"
)

const (
	// MaxBytes bounds a document; real ones are a few hundred bytes.
	MaxBytes = 5 * 1024
	// Timeout bounds the whole fetch, from dialing to the last byte.
	Timeout = 5 * time.Second
)

var (
	ErrURL      = errors.New("client metadata URL must be https")
	ErrAddress  = errors.New("client metadata host is not a public address")
	ErrTooLarge = fmt.Errorf("client metadata document exceeds %d bytes", MaxBytes)
	ErrStatus   = errors.New("client metadata document unavailable")
)

// Fetcher reads documents over https from public addresses only. It checks
// each address it connects to, after resolution, so a name that resolves to a
// private address is refused even when it changes between lookups. It never
// follows redirects or uses a proxy.
type Fetcher struct{ client *http.Client }

// New returns a fetcher that connects only to public addresses.
func New() *Fetcher { return newFetcher(Public, nil) }

func newFetcher(allowed func(netip.Addr) bool, roots *x509.CertPool) *Fetcher {
	dialer := &net.Dialer{Timeout: Timeout, Control: func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return ErrAddress
		}
		addr, err := netip.ParseAddr(host)
		if err != nil || !allowed(addr.Unmap()) {
			return ErrAddress
		}
		return nil
	}}
	transport := &http.Transport{
		DialContext:            dialer.DialContext,
		TLSClientConfig:        &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:    Timeout,
		ResponseHeaderTimeout:  Timeout,
		MaxResponseHeaderBytes: 16 << 10,
		DisableKeepAlives:      true,
	}
	return &Fetcher{client: &http.Client{Transport: transport, Timeout: Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

// reserved holds special-purpose ranges the standard library's address
// classes do not exclude.
var reserved = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
}

// Public reports a globally routable unicast address: not loopback,
// private, link-local, multicast, unspecified or otherwise reserved.
func Public(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() || addr.IsPrivate() {
		return false
	}
	for _, prefix := range reserved {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}

// Fetch returns the document at an https URL, at most MaxBytes long.
func (fetcher *Fetcher) Fetch(ctx context.Context, raw string) ([]byte, error) {
	target, err := url.Parse(raw)
	if err != nil || target.Scheme != "https" || target.Host == "" || target.User != nil {
		return nil, ErrURL
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, ErrURL
	}
	request.Header.Set("Accept", "application/json")
	response, err := fetcher.client.Do(request)
	if errors.Is(err, ErrAddress) {
		return nil, ErrAddress
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStatus, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", ErrStatus, response.StatusCode)
	}
	if response.ContentLength > MaxBytes {
		return nil, ErrTooLarge
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, MaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStatus, err)
	}
	if len(body) > MaxBytes {
		return nil, ErrTooLarge
	}
	return body, nil
}
