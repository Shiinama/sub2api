package admin

import (
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/imapcode"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"

	"github.com/gin-gonic/gin"
)

// Credential keys under which an account's mailbox (IMAP) config is stored.
// imap_password is listed in service.SensitiveCredentialKeys, so it is never
// echoed back to the frontend and survives full-object credential updates.
const (
	credIMAPHost     = "imap_host"
	credIMAPPort     = "imap_port"
	credIMAPUsername = "imap_username"
	credIMAPPassword = "imap_password"
	credIMAPFolder   = "imap_folder"
)

// IMAPInboxRequest is the body of POST /api/v1/admin/accounts/imap/inbox.
// When Password is empty and AccountID is set, the password saved on that
// account is used (re-authorizing an account whose mailbox is already saved).
type IMAPInboxRequest struct {
	AccountID     int64  `json:"account_id"`
	Host          string `json:"host"`
	Port          int    `json:"port"`
	Username      string `json:"username"`
	Password      string `json:"password"`
	Folder        string `json:"folder"`
	SenderFilter  string `json:"sender_filter"`
	ToFilter      string `json:"to_filter"`
	SinceMinutes  int    `json:"since_minutes"`
	Limit         int    `json:"limit"`
	SkipTLSVerify bool   `json:"skip_tls_verify"`
}

// IMAPInboxResponse lists recent messages, newest first.
type IMAPInboxResponse struct {
	Messages   []imapcode.Message `json:"messages"`
	LatestCode string             `json:"latest_code"`
}

// ListIMAPInbox reads recent mail with ad-hoc IMAP settings.
// POST /api/v1/admin/accounts/imap/inbox
func (h *AccountHandler) ListIMAPInbox(c *gin.Context) {
	var req IMAPInboxRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	imapReq := imapcode.Request{
		Host:          req.Host,
		Port:          req.Port,
		Username:      req.Username,
		Password:      req.Password,
		Folder:        req.Folder,
		SenderFilter:  req.SenderFilter,
		ToFilter:      req.ToFilter,
		SinceMinutes:  req.SinceMinutes,
		SkipTLSVerify: req.SkipTLSVerify,
	}
	if imapReq.Password == "" && req.AccountID > 0 {
		account, err := h.adminService.GetAccount(c.Request.Context(), req.AccountID)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		saved, ok := mailboxFromCredentials(account.Credentials)
		if !ok || !strings.EqualFold(saved.Username, strings.TrimSpace(req.Username)) {
			response.BadRequest(c, "Password is required")
			return
		}
		imapReq.Password = saved.Password
	}
	h.respondInbox(c, imapReq, req.Limit)
}

// AccountMailboxRequest is the optional body of POST /api/v1/admin/accounts/:id/mailbox.
type AccountMailboxRequest struct {
	SenderFilter string `json:"sender_filter"`
	SinceMinutes int    `json:"since_minutes"`
	Limit        int    `json:"limit"`
}

// ListAccountMailbox reads recent mail using the mailbox saved on the account.
// POST /api/v1/admin/accounts/:id/mailbox
func (h *AccountHandler) ListAccountMailbox(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	var req AccountMailboxRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			response.BadRequest(c, "Invalid request: "+err.Error())
			return
		}
	}
	account, err := h.adminService.GetAccount(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	imapReq, ok := mailboxFromCredentials(account.Credentials)
	if !ok {
		response.BadRequest(c, "Mailbox is not configured for this account")
		return
	}
	imapReq.SenderFilter = req.SenderFilter
	imapReq.SinceMinutes = req.SinceMinutes
	h.respondInbox(c, imapReq, req.Limit)
}

func (h *AccountHandler) respondInbox(c *gin.Context, req imapcode.Request, limit int) {
	if req.SinceMinutes <= 0 {
		req.SinceMinutes = 60
	}
	if limit <= 0 {
		limit = 15
	}
	msgs, err := imapcode.List(c.Request.Context(), req, limit)
	if err != nil {
		msg := "IMAP: " + err.Error()
		if strings.Contains(err.Error(), "connect ") {
			msg += " (network or proxy to the mail server is unstable, please retry)"
		}
		response.BadRequest(c, msg)
		return
	}
	if msgs == nil {
		msgs = []imapcode.Message{}
	}
	out := IMAPInboxResponse{Messages: msgs}
	for _, m := range msgs {
		if m.Code != "" {
			out.LatestCode = m.Code
			break
		}
	}
	response.Success(c, out)
}

func mailboxFromCredentials(creds map[string]any) (imapcode.Request, bool) {
	str := func(k string) string {
		v, _ := creds[k].(string)
		return strings.TrimSpace(v)
	}
	req := imapcode.Request{
		Host:     str(credIMAPHost),
		Username: str(credIMAPUsername),
		Password: str(credIMAPPassword),
		Folder:   str(credIMAPFolder),
	}
	switch v := creds[credIMAPPort].(type) {
	case float64:
		req.Port = int(v)
	case int:
		req.Port = v
	case int64:
		req.Port = int(v)
	case string:
		req.Port, _ = strconv.Atoi(strings.TrimSpace(v))
	}
	return req, req.Host != "" && req.Username != "" && req.Password != ""
}
