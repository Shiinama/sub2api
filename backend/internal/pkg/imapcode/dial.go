package imapcode

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/net/http/httpproxy"
	"golang.org/x/net/proxy"
)

// dialTLS opens the implicit-TLS IMAP connection, tunnelling through a proxy
// when one applies: req.ProxyURL if set, otherwise HTTPS_PROXY / ALL_PROXY
// from the environment (respecting NO_PROXY). Supports http(s) CONNECT and socks5.
func dialTLS(ctx context.Context, req Request, cfg *tls.Config) (net.Conn, error) {
	addr := net.JoinHostPort(req.Host, fmt.Sprint(req.Port))
	proxyURL, err := resolveProxy(addr, req.ProxyURL)
	if err != nil {
		return nil, err
	}
	// Proxies and long-haul links to providers like Gmail are often flaky
	// (handshake EOF/timeouts), so retry once before giving up.
	var firstErr error
	for attempt := 0; attempt < 2; attempt++ {
		conn, err := dialTLSVia(ctx, addr, proxyURL, cfg)
		if err == nil {
			return conn, nil
		}
		if firstErr == nil {
			firstErr = err
		}
		if ctx.Err() != nil {
			return nil, firstErr
		}
	}
	if proxyURL != nil && strings.TrimSpace(req.ProxyURL) == "" {
		// A proxy picked up from the environment may not carry IMAP traffic;
		// fall back to a direct connection before giving up.
		if conn, err := dialTLSVia(ctx, addr, nil, cfg); err == nil {
			return conn, nil
		}
	}
	return nil, firstErr
}

func dialTLSVia(ctx context.Context, addr string, proxyURL *url.URL, cfg *tls.Config) (net.Conn, error) {
	raw, err := dialTCP(ctx, addr, proxyURL)
	if err != nil {
		return nil, err
	}
	conn := tls.Client(raw, cfg)
	if err := conn.HandshakeContext(ctx); err != nil {
		_ = raw.Close()
		return nil, fmt.Errorf("tls handshake with %s: %w", addr, err)
	}
	return conn, nil
}

func dialTCP(ctx context.Context, addr string, proxyURL *url.URL) (net.Conn, error) {
	base := &net.Dialer{Timeout: dialTimeout}
	if proxyURL == nil {
		return base.DialContext(ctx, "tcp", addr)
	}
	switch strings.ToLower(proxyURL.Scheme) {
	case "socks5", "socks5h":
		d, err := proxy.FromURL(proxyURL, base)
		if err != nil {
			return nil, fmt.Errorf("proxy %s: %w", proxyURL.Redacted(), err)
		}
		if cd, ok := d.(proxy.ContextDialer); ok {
			return cd.DialContext(ctx, "tcp", addr)
		}
		return d.Dial("tcp", addr)
	case "http", "https":
		return dialHTTPConnect(ctx, base, proxyURL, addr)
	default:
		return nil, fmt.Errorf("unsupported proxy scheme %q", proxyURL.Scheme)
	}
}

func resolveProxy(addr, explicit string) (*url.URL, error) {
	if explicit = strings.TrimSpace(explicit); explicit != "" {
		u, err := url.Parse(explicit)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy URL: %w", err)
		}
		return u, nil
	}
	// IMAP over TLS behaves like HTTPS for proxy selection purposes.
	u, err := httpproxy.FromEnvironment().ProxyFunc()(&url.URL{Scheme: "https", Host: addr})
	if err != nil || u != nil {
		return u, err
	}
	for _, key := range []string{"ALL_PROXY", "all_proxy"} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			cfg := &httpproxy.Config{HTTPSProxy: v, NoProxy: firstEnv("NO_PROXY", "no_proxy")}
			return cfg.ProxyFunc()(&url.URL{Scheme: "https", Host: addr})
		}
	}
	return nil, nil
}

func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}

func dialHTTPConnect(ctx context.Context, base *net.Dialer, proxyURL *url.URL, addr string) (net.Conn, error) {
	proxyAddr := proxyURL.Host
	if proxyURL.Port() == "" {
		if strings.EqualFold(proxyURL.Scheme, "https") {
			proxyAddr = net.JoinHostPort(proxyURL.Hostname(), "443")
		} else {
			proxyAddr = net.JoinHostPort(proxyURL.Hostname(), "80")
		}
	}
	conn, err := base.DialContext(ctx, "tcp", proxyAddr)
	if err != nil {
		return nil, fmt.Errorf("connect proxy %s: %w", proxyURL.Redacted(), err)
	}
	if strings.EqualFold(proxyURL.Scheme, "https") {
		tlsConn := tls.Client(conn, &tls.Config{ServerName: proxyURL.Hostname(), MinVersion: tls.VersionTLS12})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("proxy tls: %w", err)
		}
		conn = tlsConn
	}
	if d, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(d)
	}
	req := "CONNECT " + addr + " HTTP/1.1\r\nHost: " + addr + "\r\n"
	if u := proxyURL.User; u != nil {
		pw, _ := u.Password()
		req += "Proxy-Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(u.Username()+":"+pw)) + "\r\n"
	}
	req += "\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("proxy CONNECT: %w", err)
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("proxy CONNECT: %w", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_ = conn.Close()
		return nil, fmt.Errorf("proxy CONNECT to %s: %s", addr, resp.Status)
	}
	_ = conn.SetDeadline(time.Time{})
	if br.Buffered() > 0 {
		// Keep any tunnelled bytes that arrived together with the CONNECT reply.
		return &bufferedConn{Conn: conn, r: br}, nil
	}
	return conn, nil
}

type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) { return c.r.Read(p) }
