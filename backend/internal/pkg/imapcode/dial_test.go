package imapcode

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestResolveProxyFromEnv(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:7897")
	t.Setenv("NO_PROXY", "internal.example")
	u, err := resolveProxy("imap.gmail.com:993", "")
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1:7897", u.Host)

	u, err = resolveProxy("mail.internal.example:993", "")
	require.NoError(t, err)
	require.Nil(t, u)

	u, err = resolveProxy("imap.gmail.com:993", "socks5://10.0.0.1:1080")
	require.NoError(t, err)
	require.Equal(t, "socks5", u.Scheme)
}

func TestDialHTTPConnect(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = c.Close() }()
		req, err := http.ReadRequest(bufio.NewReader(c))
		if err != nil || req.Method != http.MethodConnect || req.Host != "imap.example.com:993" {
			_, _ = c.Write([]byte("HTTP/1.1 400 Bad Request\r\n\r\n"))
			return
		}
		_, _ = c.Write([]byte("HTTP/1.1 200 Connection established\r\n\r\n"))
		_, _ = c.Write([]byte("tunnel-ok"))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := dialHTTPConnect(ctx, &net.Dialer{}, &url.URL{Scheme: "http", Host: ln.Addr().String()}, "imap.example.com:993")
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	buf := make([]byte, 9)
	_, err = conn.Read(buf)
	require.NoError(t, err)
	require.Equal(t, "tunnel-ok", string(buf))
}
