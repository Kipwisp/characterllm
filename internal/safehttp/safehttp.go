// Package safehttp downloads from URLs under a strict security policy: https
// only, no private/loopback/link-local/CGNAT targets, no redirects, and the
// request dials the exact IP address that was validated (DNS rebinding
// protection).
package safehttp

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"
)

const (
	// downloadTimeout bounds the entire download (DNS, connect, transfer).
	downloadTimeout = 15 * time.Second
	// dnsTimeout bounds hostname resolution during URL validation.
	dnsTimeout = 5 * time.Second
	// defaultUserAgent identifies the bot to servers.
	defaultUserAgent = "Mozilla/5.0 (compatible; characterllm/1.0)"
)

// Fetcher downloads URLs under a strict security policy.
type Fetcher struct {
	// Validate checks a URL and returns the URL to dial (pinned to a
	// validated address) and the original host for the Host header and TLS
	// SNI. Defaults to the built-in https-only SSRF policy.
	Validate func(ctx context.Context, raw string) (dialURL, host string, err error)
	// UserAgent overrides the User-Agent header sent with every request.
	// Empty uses defaultUserAgent.
	UserAgent string
}

// NewFetcher returns a Fetcher using the built-in policy.
func NewFetcher() *Fetcher {
	return &Fetcher{Validate: Validate}
}

// Get fetches the URL without following redirects. The caller must close the
// returned response body.
func (f *Fetcher) Get(ctx context.Context, raw string) (*http.Response, error) {
	validate := f.Validate
	if validate == nil {
		validate = Validate
	}
	dialURL, host, err := validate(ctx, raw)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "GET", dialURL, nil)
	if err != nil {
		return nil, err
	}
	req.Host = host
	ua := f.UserAgent
	if ua == "" {
		ua = defaultUserAgent
	}
	req.Header.Set("User-Agent", ua)

	client := &http.Client{
		Timeout: downloadTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			Proxy:           http.ProxyFromEnvironment,
			TLSClientConfig: &tls.Config{ServerName: host},
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		resp.Body.Close()
		return nil, fmt.Errorf("redirects are not allowed")
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("download failed: status %d", resp.StatusCode)
	}
	return resp, nil
}

// Validate enforces the download policy: https only; the host must resolve;
// every resolved address must be public (private, loopback, link-local,
// unspecified, and carrier-grade NAT addresses are rejected). It returns the
// URL pinned to the first resolved address, so the request dials exactly the
// address that was validated (a second resolution cannot swap in a blocked
// one), plus the original host name for the Host header and TLS SNI.
func Validate(ctx context.Context, raw string) (string, string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "https" {
		return "", "", fmt.Errorf("invalid URL scheme: %s (https only)", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return "", "", fmt.Errorf("invalid URL: missing host")
	}

	dnsCtx, cancel := context.WithTimeout(ctx, dnsTimeout)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIP(dnsCtx, "ip", host)
	if err != nil {
		return "", "", fmt.Errorf("failed to resolve host %s: %w", host, err)
	}
	for _, ip := range ips {
		if isDisallowedIP(ip) {
			return "", "", fmt.Errorf("host %s resolves to a disallowed address", host)
		}
	}

	port := u.Port()
	if port == "" {
		port = "443"
	}
	u.Host = net.JoinHostPort(ips[0].String(), port)
	return u.String(), host, nil
}

func isDisallowedIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsPrivate() {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil && ip4[0] == 100 && ip4[1]&0xC0 == 64 {
		return true // 100.64.0.0/10 carrier-grade NAT
	}
	return false
}
