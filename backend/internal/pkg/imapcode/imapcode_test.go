package imapcode

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestExtractCode(t *testing.T) {
	cases := []struct {
		name, subject, body, want string
	}{
		{"chinese keyword", "", "您的验证码：482913，10分钟内有效。", "482913"},
		{"english is", "", "Your verification code is 739201. It expires in 10 minutes.", "739201"},
		{"google prefix", "G-552018 is your Google verification code", "", "552018"},
		{"alnum", "", "Your login code: AB12CD", "AB12CD"},
		{"subject wins", "Code 112233", "Your code is 445566", "112233"},
		{"bare six digits", "", "Use 908172 to sign in.", "908172"},
		{"skip year", "", "Code expires 2026. Use 551234 now.", "551234"},
		{"none", "Welcome", "Thanks for signing up!", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, ExtractCode(tc.subject, tc.body))
		})
	}
}

func TestParseMessageMultipartQuotedPrintableAndHTML(t *testing.T) {
	raw := strings.Join([]string{
		"From: Google <no-reply@accounts.google.com>",
		"To: user@example.com",
		"Subject: =?UTF-8?B?6aqM6K+B56CB?=",
		"Content-Type: multipart/alternative; boundary=XYZ",
		"",
		"--XYZ",
		"Content-Type: text/html; charset=UTF-8",
		"Content-Transfer-Encoding: quoted-printable",
		"",
		"<html><head><style>.a{color:#123456}</style></head><body>Your code is <b>=",
		"314159</b></body></html>",
		"--XYZ--",
		"",
	}, "\r\n")
	msg := parseMessage([]byte(raw))
	require.Equal(t, "验证码", msg.subject)
	require.Equal(t, "314159", ExtractCode(msg.subject, msg.body))
}

// fakeServer speaks just enough IMAP over a plain TCP pipe to exercise run().
func fakeServer(t *testing.T, conn net.Conn, mails map[uint64]string, dates map[uint64]time.Time) {
	t.Helper()
	defer func() { _ = conn.Close() }()
	r := bufio.NewReader(conn)
	w := func(s string) { _, _ = conn.Write([]byte(s)) }
	w("* OK fake IMAP ready\r\n")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		parts := strings.SplitN(line, " ", 3)
		tag, cmd := parts[0], strings.ToUpper(parts[1])
		rest := ""
		if len(parts) > 2 {
			rest = parts[2]
		}
		switch {
		case cmd == "LOGIN":
			if rest != `"user@example.com" "p\"w"` {
				w(tag + " NO [AUTHENTICATIONFAILED] bad creds\r\n")
				continue
			}
			w(tag + " OK logged in\r\n")
		case cmd == "EXAMINE":
			w("* 3 EXISTS\r\n" + tag + " OK [READ-ONLY] done\r\n")
		case cmd == "UID" && strings.HasPrefix(strings.ToUpper(rest), "SEARCH"):
			ids := []string{}
			for uid := range mails {
				ids = append(ids, fmt.Sprint(uid))
			}
			w("* SEARCH " + strings.Join(ids, " ") + "\r\n" + tag + " OK search done\r\n")
		case cmd == "UID" && strings.HasPrefix(strings.ToUpper(rest), "FETCH"):
			var uid uint64
			_, _ = fmt.Sscanf(strings.Fields(rest)[1], "%d", &uid)
			body := mails[uid]
			d := dates[uid].Format("02-Jan-2006 15:04:05 -0700")
			w(fmt.Sprintf("* %d FETCH (UID %d INTERNALDATE \"%s\" BODY[]<0> {%d}\r\n%s)\r\n", uid, uid, d, len(body), body))
			w(tag + " OK fetch done\r\n")
		case cmd == "LOGOUT":
			w("* BYE\r\n" + tag + " OK bye\r\n")
			return
		default:
			w(tag + " BAD unknown\r\n")
		}
	}
}

func runAgainstFake(t *testing.T, req Request, mails map[uint64]string, dates map[uint64]time.Time) (*Result, error) {
	t.Helper()
	srv, cli := net.Pipe()
	go fakeServer(t, srv, mails, dates)
	_ = cli.SetDeadline(time.Now().Add(5 * time.Second))
	req, err := normalize(req)
	require.NoError(t, err)
	return newClient(cli).run(req)
}

func TestRunReturnsNewestCodeAndSkipsOld(t *testing.T) {
	now := time.Now()
	mails := map[uint64]string{
		1: "Subject: old\r\n\r\nYour code is 111111\r\n",
		2: "Subject: newsletter\r\n\r\nHello there\r\n",
		3: "Subject: Your sign-in code\r\nContent-Type: text/plain\r\n\r\nYour verification code is 222333\r\n",
	}
	dates := map[uint64]time.Time{1: now.Add(-2 * time.Hour), 2: now.Add(-time.Minute), 3: now.Add(-30 * time.Second)}

	res, err := runAgainstFake(t, Request{Host: "imap.example.com", Username: "user@example.com", Password: `p"w`, SinceMinutes: 10}, mails, dates)
	require.NoError(t, err)
	require.Equal(t, "222333", res.Code)
	require.Equal(t, "Your sign-in code", res.Subject)
	require.Equal(t, 1, res.MessagesChecked)

	// Only the old message has a code -> it is outside the window.
	delete(mails, 3)
	res, err = runAgainstFake(t, Request{Host: "imap.example.com", Username: "user@example.com", Password: `p"w`, SinceMinutes: 10}, mails, dates)
	require.ErrorIs(t, err, ErrNoCode)
	require.Equal(t, 1, res.MessagesChecked)
}

func TestRunLoginFailure(t *testing.T) {
	_, err := runAgainstFake(t, Request{Host: "imap.example.com", Username: "user@example.com", Password: "wrong"}, map[uint64]string{}, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "login failed")
}

func TestNormalizeRejectsInjection(t *testing.T) {
	_, err := normalize(Request{Host: "h", Username: "u", Password: "p\r\na2 DELETE INBOX"})
	require.Error(t, err)
}

func TestListReturnsRecentMessagesNewestFirst(t *testing.T) {
	now := time.Now()
	mails := map[uint64]string{
		1: "Subject: old\r\n\r\nYour code is 111111\r\n",
		2: "Subject: hello\r\nFrom: a@b.c\r\n\r\nJust   saying\r\nhi\r\n",
		3: "Subject: Your code\r\n\r\nYour verification code is 222333\r\n",
	}
	dates := map[uint64]time.Time{1: now.Add(-2 * time.Hour), 2: now.Add(-time.Minute), 3: now.Add(-30 * time.Second)}
	srv, cli := net.Pipe()
	go fakeServer(t, srv, mails, dates)
	_ = cli.SetDeadline(time.Now().Add(5 * time.Second))
	req, err := normalize(Request{Host: "imap.example.com", Username: "user@example.com", Password: `p"w`, SinceMinutes: 10})
	require.NoError(t, err)
	msgs, err := newClient(cli).list(req, 10)
	require.NoError(t, err)
	require.Len(t, msgs, 2)
	require.Equal(t, uint64(3), msgs[0].UID)
	require.Equal(t, "222333", msgs[0].Code)
	require.Equal(t, "hello", msgs[1].Subject)
	require.Equal(t, "", msgs[1].Code)
	require.Equal(t, "Just saying hi", msgs[1].Preview)
}
