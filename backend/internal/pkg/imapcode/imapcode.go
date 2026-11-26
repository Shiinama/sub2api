// Package imapcode fetches recent mail over IMAP (implicit TLS) and extracts
// one-time verification codes. It is a deliberately small, read-only IMAP
// client built on the standard library: LOGIN, SELECT, UID SEARCH, UID FETCH
// (BODY.PEEK, so messages are not marked as read) and LOGOUT.
package imapcode

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	defaultPort         = 993
	defaultFolder       = "INBOX"
	defaultSinceMinutes = 15
	maxSinceMinutes     = 24 * 60
	maxMessagesScanned  = 10
	maxListLimit        = 30
	maxFetchBytes       = 256 * 1024
	maxLiteralBytes     = maxFetchBytes + 4096
	maxLineBytes        = 64 * 1024
	dialTimeout         = 10 * time.Second
	sessionTimeout      = 55 * time.Second
)

// Request describes one verification-code lookup.
type Request struct {
	Host          string
	Port          int
	Username      string
	Password      string
	Folder        string
	SenderFilter  string // optional substring for the IMAP FROM search
	ToFilter      string // optional recipient (useful for catch-all mailboxes)
	SinceMinutes  int
	SkipTLSVerify bool
	// ProxyURL optionally tunnels the connection (http/https CONNECT or socks5).
	// When empty, HTTPS_PROXY / ALL_PROXY from the environment apply (NO_PROXY respected).
	ProxyURL string
}

// Result is the newest message that contained a verification code.
type Result struct {
	Code            string    `json:"code"`
	Subject         string    `json:"subject"`
	From            string    `json:"from"`
	To              string    `json:"to"`
	ReceivedAt      time.Time `json:"received_at"`
	MessagesChecked int       `json:"messages_checked"`
}

// ErrNoCode is returned when the mailbox was read successfully but no recent
// message contained something that looks like a verification code.
var ErrNoCode = errors.New("no verification code found in recent messages")

// Message is one recent mailbox entry with any verification code found in it.
type Message struct {
	UID        uint64    `json:"uid"`
	Subject    string    `json:"subject"`
	From       string    `json:"from"`
	To         string    `json:"to"`
	ReceivedAt time.Time `json:"received_at"`
	Code       string    `json:"code,omitempty"`
	Preview    string    `json:"preview"`
}

// List connects and returns up to limit of the newest messages received in
// the last SinceMinutes, newest first. Messages are opened read-only.
func List(ctx context.Context, req Request, limit int) ([]Message, error) {
	req, err := normalize(req)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > maxListLimit {
		limit = maxListLimit
	}
	var out []Message
	err = withClient(ctx, req, func(c *client) error {
		var err error
		out, err = c.list(req, limit)
		return err
	})
	return out, err
}

// Fetch connects, scans the newest matching messages and returns the code
// from the most recent one that has it.
func Fetch(ctx context.Context, req Request) (*Result, error) {
	req, err := normalize(req)
	if err != nil {
		return nil, err
	}
	var res *Result
	err = withClient(ctx, req, func(c *client) error {
		var err error
		res, err = c.run(req)
		return err
	})
	return res, err
}

func withClient(ctx context.Context, req Request, fn func(*client) error) error {
	deadline := time.Now().Add(sessionTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	// Budget for proxy retries plus a direct fallback; leave time to read mail.
	dialCtx, cancelDial := context.WithTimeout(ctx, 35*time.Second)
	defer cancelDial()
	conn, err := dialTLS(dialCtx, req, &tls.Config{
		ServerName:         req.Host,
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: req.SkipTLSVerify, //nolint:gosec // explicit admin opt-in for self-signed mail servers
	})
	if err != nil {
		return fmt.Errorf("connect %s:%d: %w", req.Host, req.Port, err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(deadline)
	return fn(newClient(conn))
}

func normalize(req Request) (Request, error) {
	req.Host = strings.TrimSpace(req.Host)
	req.Username = strings.TrimSpace(req.Username)
	req.Folder = strings.TrimSpace(req.Folder)
	req.SenderFilter = strings.TrimSpace(req.SenderFilter)
	req.ToFilter = strings.TrimSpace(req.ToFilter)
	if req.Host == "" || strings.ContainsAny(req.Host, " /\r\n") {
		return req, errors.New("invalid IMAP host")
	}
	if req.Username == "" || req.Password == "" {
		return req, errors.New("username and password are required")
	}
	if req.Port == 0 {
		req.Port = defaultPort
	}
	if req.Port < 1 || req.Port > 65535 {
		return req, errors.New("invalid IMAP port")
	}
	if req.Folder == "" {
		req.Folder = defaultFolder
	}
	if req.SinceMinutes <= 0 {
		req.SinceMinutes = defaultSinceMinutes
	}
	if req.SinceMinutes > maxSinceMinutes {
		req.SinceMinutes = maxSinceMinutes
	}
	for _, v := range []string{req.Username, req.Password, req.Folder, req.SenderFilter, req.ToFilter} {
		if strings.ContainsAny(v, "\r\n\x00") {
			return req, errors.New("fields must not contain line breaks")
		}
	}
	return req, nil
}

type client struct {
	r   *bufio.Reader
	w   io.Writer
	seq int
}

func newClient(conn net.Conn) *client {
	return &client{r: bufio.NewReaderSize(conn, 16*1024), w: conn}
}

// response is one untagged or tagged server response, with any literals
// ({n}\r\n<bytes>) collected separately from the surrounding text.
type response struct {
	text     string
	literals [][]byte
}

func (c *client) run(req Request) (*Result, error) {
	msgs, err := c.list(req, maxMessagesScanned)
	if err != nil {
		return nil, err
	}
	for i, m := range msgs {
		if m.Code != "" {
			return &Result{
				Code:            m.Code,
				Subject:         m.Subject,
				From:            m.From,
				To:              m.To,
				ReceivedAt:      m.ReceivedAt,
				MessagesChecked: i + 1,
			}, nil
		}
	}
	return &Result{MessagesChecked: len(msgs)}, ErrNoCode
}

func (c *client) list(req Request, limit int) ([]Message, error) {
	greeting, err := c.readResponse()
	if err != nil {
		return nil, fmt.Errorf("read greeting: %w", err)
	}
	if !strings.HasPrefix(greeting.text, "* OK") && !strings.HasPrefix(greeting.text, "* PREAUTH") {
		return nil, fmt.Errorf("unexpected greeting: %s", truncate(greeting.text, 120))
	}

	if _, err := c.command("LOGIN", astring(req.Username), astring(req.Password)); err != nil {
		return nil, fmt.Errorf("login failed: %w", err)
	}
	defer func() { _, _ = c.command("LOGOUT") }()

	if _, err := c.command("EXAMINE", astring(req.Folder)); err != nil {
		return nil, fmt.Errorf("open folder %q: %w", req.Folder, err)
	}

	cutoff := time.Now().Add(-time.Duration(req.SinceMinutes) * time.Minute)
	// SINCE is date-only (server timezone); go one day back and filter by
	// INTERNALDATE afterwards.
	criteria := []string{"SINCE", cutoff.Add(-24 * time.Hour).UTC().Format("2-Jan-2006")}
	if req.SenderFilter != "" {
		criteria = append(criteria, "FROM", astring(req.SenderFilter))
	}
	if req.ToFilter != "" {
		criteria = append(criteria, "TO", astring(req.ToFilter))
	}
	untagged, err := c.command(append([]string{"UID SEARCH"}, criteria...)...)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	uids := parseSearch(untagged)
	sort.Slice(uids, func(i, j int) bool { return uids[i] > uids[j] })
	if len(uids) > limit {
		uids = uids[:limit]
	}

	out := make([]Message, 0, len(uids))
	for _, uid := range uids {
		msg, err := c.fetch(uid)
		if err != nil {
			return nil, fmt.Errorf("fetch message: %w", err)
		}
		if msg == nil {
			continue
		}
		if !msg.receivedAt.IsZero() && msg.receivedAt.Before(cutoff) {
			// UIDs are descending, so everything after this is older still.
			break
		}
		out = append(out, Message{
			UID:        uid,
			Subject:    msg.subject,
			From:       msg.from,
			To:         msg.to,
			ReceivedAt: msg.receivedAt,
			Code:       ExtractCode(msg.subject, msg.body),
			Preview:    preview(msg.body, 160),
		})
	}
	return out, nil
}

func preview(body string, n int) string {
	s := strings.Join(strings.Fields(body), " ")
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// command sends a tagged command and returns the untagged responses that
// preceded the tagged OK.
func (c *client) command(parts ...string) ([]response, error) {
	c.seq++
	tag := fmt.Sprintf("a%03d", c.seq)
	line := tag + " " + strings.Join(parts, " ") + "\r\n"
	if _, err := io.WriteString(c.w, line); err != nil {
		return nil, err
	}
	var untagged []response
	for {
		resp, err := c.readResponse()
		if err != nil {
			return nil, err
		}
		if strings.HasPrefix(resp.text, tag+" ") {
			status := strings.TrimPrefix(resp.text, tag+" ")
			if strings.HasPrefix(strings.ToUpper(status), "OK") {
				return untagged, nil
			}
			return nil, errors.New(truncate(status, 200))
		}
		if strings.HasPrefix(resp.text, "+") {
			// We never send literals, so a continuation request is unexpected.
			return nil, errors.New("unexpected continuation request")
		}
		untagged = append(untagged, resp)
	}
}

func (c *client) readLine() (string, error) {
	var buf []byte
	for {
		chunk, isPrefix, err := c.r.ReadLine()
		if err != nil {
			return "", err
		}
		buf = append(buf, chunk...)
		if len(buf) > maxLineBytes {
			return "", errors.New("server line too long")
		}
		if !isPrefix {
			return string(buf), nil
		}
	}
}

var literalSuffix = regexp.MustCompile(`\{(\d+)\+?\}$`)

func (c *client) readResponse() (response, error) {
	var resp response
	var text strings.Builder
	for {
		line, err := c.readLine()
		if err != nil {
			return resp, err
		}
		m := literalSuffix.FindStringSubmatch(line)
		if m == nil {
			text.WriteString(line)
			resp.text = text.String()
			return resp, nil
		}
		n, err := strconv.Atoi(m[1])
		if err != nil || n < 0 || n > maxLiteralBytes {
			return resp, fmt.Errorf("literal too large (%s bytes)", m[1])
		}
		text.WriteString(line[:len(line)-len(m[0])])
		text.WriteString("{}")
		lit := make([]byte, n)
		if _, err := io.ReadFull(c.r, lit); err != nil {
			return resp, err
		}
		resp.literals = append(resp.literals, lit)
	}
}

func parseSearch(rs []response) []uint64 {
	var out []uint64
	for _, r := range rs {
		fields := strings.Fields(r.text)
		if len(fields) < 2 || fields[0] != "*" || !strings.EqualFold(fields[1], "SEARCH") {
			continue
		}
		for _, f := range fields[2:] {
			if v, err := strconv.ParseUint(f, 10, 64); err == nil {
				out = append(out, v)
			}
		}
	}
	return out
}

type fetchedMessage struct {
	subject    string
	from       string
	to         string
	receivedAt time.Time
	body       string
}

var internalDateRe = regexp.MustCompile(`INTERNALDATE "([^"]+)"`)

func (c *client) fetch(uid uint64) (*fetchedMessage, error) {
	rs, err := c.command("UID FETCH", strconv.FormatUint(uid, 10), fmt.Sprintf("(INTERNALDATE BODY.PEEK[]<0.%d>)", maxFetchBytes))
	if err != nil {
		return nil, err
	}
	for _, r := range rs {
		if !strings.Contains(strings.ToUpper(r.text), "FETCH") || len(r.literals) == 0 {
			continue
		}
		msg := parseMessage(r.literals[len(r.literals)-1])
		if m := internalDateRe.FindStringSubmatch(r.text); m != nil {
			if t, err := time.Parse("2-Jan-2006 15:04:05 -0700", strings.TrimSpace(m[1])); err == nil {
				msg.receivedAt = t
			}
		}
		return msg, nil
	}
	return nil, nil
}

func parseMessage(raw []byte) *fetchedMessage {
	out := &fetchedMessage{}
	m, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		out.body = string(raw)
		return out
	}
	dec := &mime.WordDecoder{}
	decodeHeader := func(v string) string {
		if s, err := dec.DecodeHeader(v); err == nil {
			return s
		}
		return v
	}
	out.subject = decodeHeader(m.Header.Get("Subject"))
	out.from = decodeHeader(m.Header.Get("From"))
	out.to = decodeHeader(m.Header.Get("To"))
	if out.receivedAt.IsZero() {
		if t, err := m.Header.Date(); err == nil {
			out.receivedAt = t
		}
	}
	plain, htmlText := collectText(m.Header.Get("Content-Type"), m.Header.Get("Content-Transfer-Encoding"), m.Body, 0)
	if strings.TrimSpace(plain) != "" {
		out.body = plain
	} else {
		out.body = htmlToText(htmlText)
	}
	return out
}

// collectText walks a MIME tree and returns the concatenated text/plain and
// text/html parts.
func collectText(contentType, encoding string, body io.Reader, depth int) (plain, htmlText string) {
	if depth > 8 {
		return "", ""
	}
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType == "" {
		mediaType = "text/plain"
	}
	if strings.HasPrefix(mediaType, "multipart/") {
		mr := multipart.NewReader(body, params["boundary"])
		for {
			part, err := mr.NextRawPart()
			if err != nil {
				break
			}
			p, h := collectText(part.Header.Get("Content-Type"), part.Header.Get("Content-Transfer-Encoding"), part, depth+1)
			plain += p
			htmlText += h
		}
		return plain, htmlText
	}
	if mediaType != "text/plain" && mediaType != "text/html" {
		return "", ""
	}
	data, _ := io.ReadAll(io.LimitReader(decodeTransfer(encoding, body), maxFetchBytes))
	text := string(data)
	if mediaType == "text/html" {
		return "", text
	}
	return text, ""
}

func decodeTransfer(encoding string, r io.Reader) io.Reader {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "base64":
		return base64.NewDecoder(base64.StdEncoding, &stripWhitespace{r: r})
	case "quoted-printable":
		return quotedprintable.NewReader(r)
	default:
		return r
	}
}

// stripWhitespace drops CR/LF/space so base64 bodies with line breaks decode.
type stripWhitespace struct{ r io.Reader }

func (s *stripWhitespace) Read(p []byte) (int, error) {
	for {
		n, err := s.r.Read(p)
		j := 0
		for _, b := range p[:n] {
			if b != '\r' && b != '\n' && b != ' ' && b != '\t' {
				p[j] = b
				j++
			}
		}
		if j > 0 || err != nil {
			return j, err
		}
	}
}

var (
	styleScriptRe = regexp.MustCompile(`(?is)<(style|script|head)[^>]*>.*?</(style|script|head)>`)
	tagRe         = regexp.MustCompile(`(?s)<[^>]+>`)
	spaceRe       = regexp.MustCompile(`[ \t\x{00a0}]+`)
)

func htmlToText(s string) string {
	s = styleScriptRe.ReplaceAllString(s, " ")
	s = tagRe.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	return spaceRe.ReplaceAllString(s, " ")
}

var (
	// A keyword followed (within a short window) by a 4-8 char token that
	// contains at least one digit, e.g. "验证码：123456", "code is G-482913".
	keywordCodeRe = regexp.MustCompile(`(?i)(?:验证码|校验码|驗證碼|动态码|确认码|代码|verification code|security code|one[- ]time (?:pass)?code|login code|sign[- ]in code|confirmation code|passcode|otp|code)[^A-Za-z0-9]{0,40}?(?:is|为|是|:|：)?[^A-Za-z0-9]{0,20}?((?:[A-Z]-)?[A-Za-z0-9]{4,8})\b`)
	sixDigitRe    = regexp.MustCompile(`(?:^|[^0-9A-Za-z#])(\d{6})(?:[^0-9A-Za-z]|$)`)
	hasDigitRe    = regexp.MustCompile(`\d`)
)

// ExtractCode finds the most likely verification code in a subject/body.
func ExtractCode(subject, body string) string {
	for _, text := range []string{subject, body} {
		for _, m := range keywordCodeRe.FindAllStringSubmatch(text, -1) {
			if hasDigitRe.MatchString(m[1]) && !isLikelyYear(m[1]) {
				return m[1]
			}
		}
	}
	for _, text := range []string{subject, body} {
		if m := sixDigitRe.FindStringSubmatch(text); m != nil {
			return m[1]
		}
	}
	return ""
}

func isLikelyYear(s string) bool {
	if len(s) != 4 {
		return false
	}
	n, err := strconv.Atoi(s)
	return err == nil && n >= 1990 && n <= 2100
}

// astring renders a value as an IMAP quoted string.
func astring(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
