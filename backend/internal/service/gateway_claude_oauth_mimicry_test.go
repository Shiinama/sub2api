package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func resetGatewayForwardingCacheForTest(t *testing.T) {
	t.Helper()
	gatewayForwardingCache.Store(&cachedGatewayForwardingSettings{})
	t.Cleanup(func() {
		gatewayForwardingCache.Store(&cachedGatewayForwardingSettings{})
	})
}

func TestClaudeOAuthForwardingMode_DefaultEnablesMimicry(t *testing.T) {
	resetGatewayForwardingCacheForTest(t)
	svc := &GatewayService{
		settingService: NewSettingService(&gatewayTTLSettingRepo{data: map[string]string{}}, &config.Config{}),
	}

	mode := svc.claudeOAuthForwardingMode(context.Background(), &Account{Type: AccountTypeOAuth}, false)

	require.True(t, mode.mimicClaudeCode)
	require.False(t, mode.transparentOAuth)
}

func TestClaudeOAuthForwardingMode_DisabledUsesTransparentOAuth(t *testing.T) {
	resetGatewayForwardingCacheForTest(t)
	svc := &GatewayService{
		settingService: NewSettingService(&gatewayTTLSettingRepo{data: map[string]string{
			SettingKeyEnableClaudeCodeOAuthMimicry: "false",
		}}, &config.Config{}),
	}

	mode := svc.claudeOAuthForwardingMode(context.Background(), &Account{Type: AccountTypeOAuth}, false)
	realClaudeMode := svc.claudeOAuthForwardingMode(context.Background(), &Account{Type: AccountTypeOAuth}, true)

	require.False(t, mode.mimicClaudeCode)
	require.True(t, mode.transparentOAuth)
	require.False(t, realClaudeMode.mimicClaudeCode)
	require.False(t, realClaudeMode.transparentOAuth)
}

func TestSettingService_ClaudeCodeOAuthMimicryDefaultsAndRefreshesCache(t *testing.T) {
	ctx := context.Background()
	resetGatewayForwardingCacheForTest(t)
	repo := &gatewayTTLSettingRepo{data: map[string]string{}}
	svc := NewSettingService(repo, &config.Config{})

	settings, err := svc.GetAllSettings(ctx)
	require.NoError(t, err)
	require.True(t, settings.EnableClaudeCodeOAuthMimicry)
	require.True(t, svc.IsClaudeCodeOAuthMimicryEnabled(ctx))

	err = svc.UpdateSettings(ctx, &SystemSettings{
		EnableFingerprintUnification: true,
		EnableClaudeCodeOAuthMimicry: false,
	})
	require.NoError(t, err)
	require.Equal(t, "false", repo.data[SettingKeyEnableClaudeCodeOAuthMimicry])
	require.False(t, svc.IsClaudeCodeOAuthMimicryEnabled(ctx))
}

func TestSettingService_ClaudeCodeOAuthMimicryDBErrorFallbackEnabled(t *testing.T) {
	ctx := context.Background()
	resetGatewayForwardingCacheForTest(t)
	repo := &gatewayTTLSettingRepo{getMultipleErr: errors.New("db down")}
	svc := NewSettingService(repo, &config.Config{})

	require.True(t, svc.IsClaudeCodeOAuthMimicryEnabled(ctx))
}

func TestGatewayService_ForwardTransparentOAuthPreservesSystemAndMessages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resetGatewayForwardingCacheForTest(t)

	body := []byte(`{"model":"claude-3-7-sonnet-20250219","system":"client system","messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`)
	parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("User-Agent", "opencode/1.0")
	c.Request.Header.Set("X-App", "opencode")

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}, "x-request-id": []string{"rid-transparent"}},
		Body:       io.NopCloser(strings.NewReader(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-3-7-sonnet-20250219","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":3,"output_tokens":1}}`)),
	}}
	cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}
	svc := &GatewayService{
		cfg:                  cfg,
		responseHeaderFilter: compileResponseHeaderFilter(cfg),
		httpUpstream:         upstream,
		rateLimitService:     &RateLimitService{},
		deferredService:      &DeferredService{},
		settingService: NewSettingService(&gatewayTTLSettingRepo{data: map[string]string{
			SettingKeyEnableClaudeCodeOAuthMimicry: "false",
		}}, &config.Config{}),
	}
	account := &Account{
		ID:          10,
		Name:        "anthropic-oauth-transparent",
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "oauth-token"},
		Status:      StatusActive,
		Schedulable: true,
	}

	result, err := svc.Forward(context.Background(), c, account, parsed)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, upstream.lastReq)
	require.Equal(t, "client system", gjson.GetBytes(upstream.lastBody, "system").String())
	require.Equal(t, "user", gjson.GetBytes(upstream.lastBody, "messages.0.role").String())
	require.Equal(t, "hi", gjson.GetBytes(upstream.lastBody, "messages.0.content.0.text").String())
	require.False(t, gjson.GetBytes(upstream.lastBody, "metadata.user_id").Exists())
	require.NotContains(t, string(upstream.lastBody), "x-anthropic-billing-header")
	require.NotContains(t, string(upstream.lastBody), claudeCodeSystemPrompt)
	require.Equal(t, "opencode/1.0", getHeaderRaw(upstream.lastReq.Header, "User-Agent"))
	require.Equal(t, "opencode", getHeaderRaw(upstream.lastReq.Header, "X-App"))
	require.NotContains(t, getHeaderRaw(upstream.lastReq.Header, "anthropic-beta"), claude.BetaClaudeCode)
}

func TestForwardAsChatCompletions_TransparentOAuthSkipsClaudeCodeMimicry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resetGatewayForwardingCacheForTest(t)

	body := []byte(`{"model":"claude-3-7-sonnet-20250219","messages":[{"role":"system","content":"client system"},{"role":"user","content":"hello"}],"stream":false}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("User-Agent", "openai-compatible-client/1.0")

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusBadRequest,
		Header:     http.Header{"Content-Type": []string{"application/json"}, "x-request-id": []string{"rid-chat-transparent"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":{"type":"invalid_request_error","message":"stop"}}`)),
	}}
	cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}
	svc := &GatewayService{
		cfg:                  cfg,
		responseHeaderFilter: compileResponseHeaderFilter(cfg),
		httpUpstream:         upstream,
		settingService: NewSettingService(&gatewayTTLSettingRepo{data: map[string]string{
			SettingKeyEnableClaudeCodeOAuthMimicry: "false",
		}}, &config.Config{}),
	}
	account := &Account{
		ID:          11,
		Name:        "anthropic-oauth-chat-transparent",
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "oauth-token"},
	}

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body, nil)
	require.Error(t, err)
	require.Nil(t, result)
	require.NotNil(t, upstream.lastReq)
	require.Contains(t, string(upstream.lastBody), "client system")
	require.NotContains(t, string(upstream.lastBody), "x-anthropic-billing-header")
	require.NotContains(t, string(upstream.lastBody), claudeCodeSystemPrompt)
	require.NotContains(t, getHeaderRaw(upstream.lastReq.Header, "anthropic-beta"), claude.BetaClaudeCode)
}

func TestForwardAsResponses_TransparentOAuthSkipsClaudeCodeMimicry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resetGatewayForwardingCacheForTest(t)

	body := []byte(`{"model":"claude-3-7-sonnet-20250219","input":[{"role":"system","content":"client instructions"},{"role":"user","content":[{"type":"input_text","text":"hello"}]}],"stream":false}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("User-Agent", "openai-compatible-client/1.0")

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusBadRequest,
		Header:     http.Header{"Content-Type": []string{"application/json"}, "x-request-id": []string{"rid-responses-transparent"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":{"type":"invalid_request_error","message":"stop"}}`)),
	}}
	cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}
	svc := &GatewayService{
		cfg:                  cfg,
		responseHeaderFilter: compileResponseHeaderFilter(cfg),
		httpUpstream:         upstream,
		settingService: NewSettingService(&gatewayTTLSettingRepo{data: map[string]string{
			SettingKeyEnableClaudeCodeOAuthMimicry: "false",
		}}, &config.Config{}),
	}
	account := &Account{
		ID:          12,
		Name:        "anthropic-oauth-responses-transparent",
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "oauth-token"},
	}

	result, err := svc.ForwardAsResponses(context.Background(), c, account, body, nil)
	require.Error(t, err)
	require.Nil(t, result)
	require.NotNil(t, upstream.lastReq)
	require.Contains(t, string(upstream.lastBody), "client instructions")
	require.NotContains(t, string(upstream.lastBody), "x-anthropic-billing-header")
	require.NotContains(t, string(upstream.lastBody), claudeCodeSystemPrompt)
	require.NotContains(t, getHeaderRaw(upstream.lastReq.Header, "anthropic-beta"), claude.BetaClaudeCode)
}

func TestBuildUpstreamRequest_TransparentOAuthPreservesClientHeadersAndBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Request.Header.Set("User-Agent", "opencode/1.0")
	c.Request.Header.Set("X-App", "opencode")
	c.Request.Header.Set("Anthropic-Beta", "custom-beta")
	c.Request.Header.Set("X-Stainless-Lang", "go")

	body := []byte(`{"model":"claude-3-7-sonnet-20250219","metadata":{"user_id":"client-user"},"system":"client system","messages":[{"role":"user","content":"hi"}]}`)
	svc := &GatewayService{
		cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}},
	}
	account := &Account{ID: 10, Platform: PlatformAnthropic, Type: AccountTypeOAuth}

	req, _, err := svc.buildUpstreamRequest(context.Background(), c, account, body, "oauth-token", "oauth", "claude-3-7-sonnet-20250219", false, claudeUpstreamForwardingMode{transparentOAuth: true})
	require.NoError(t, err)

	require.Equal(t, "Bearer oauth-token", getHeaderRaw(req.Header, "authorization"))
	require.Equal(t, "application/json", getHeaderRaw(req.Header, "content-type"))
	require.Equal(t, "2023-06-01", getHeaderRaw(req.Header, "anthropic-version"))
	require.Equal(t, "opencode/1.0", getHeaderRaw(req.Header, "User-Agent"))
	require.Equal(t, "opencode", getHeaderRaw(req.Header, "X-App"))
	require.Equal(t, "custom-beta", getHeaderRaw(req.Header, "anthropic-beta"))
	require.NotContains(t, getHeaderRaw(req.Header, "anthropic-beta"), claude.BetaOAuth)
	require.NotContains(t, getHeaderRaw(req.Header, "anthropic-beta"), claude.BetaClaudeCode)
	require.Equal(t, "go", getHeaderRaw(req.Header, "X-Stainless-Lang"))
	require.Empty(t, getHeaderRaw(req.Header, "X-Stainless-Package-Version"))
	require.JSONEq(t, string(body), string(readRequestBodyForTest(t, req)))
}

func TestBuildUpstreamRequest_MimicOAuthForcesClaudeCodeHeadersAndBetas(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Request.Header.Set("User-Agent", "opencode/1.0")
	c.Request.Header.Set("X-App", "opencode")
	c.Request.Header.Set("Anthropic-Beta", "custom-beta")

	svc := &GatewayService{
		cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}},
	}
	account := &Account{ID: 10, Platform: PlatformAnthropic, Type: AccountTypeOAuth}

	req, _, err := svc.buildUpstreamRequest(context.Background(), c, account, []byte(`{"model":"claude-3-7-sonnet-20250219"}`), "oauth-token", "oauth", "claude-3-7-sonnet-20250219", false, claudeUpstreamForwardingMode{mimicClaudeCode: true})
	require.NoError(t, err)

	require.Contains(t, getHeaderRaw(req.Header, "User-Agent"), "claude-cli/")
	require.Equal(t, "cli", getHeaderRaw(req.Header, "X-App"))
	require.Contains(t, getHeaderRaw(req.Header, "anthropic-beta"), claude.BetaOAuth)
	require.Contains(t, getHeaderRaw(req.Header, "anthropic-beta"), claude.BetaClaudeCode)
}

func TestBuildCountTokensRequest_TransparentOAuthPreservesClientBeta(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", nil)
	c.Request.Header.Set("User-Agent", "opencode/1.0")
	c.Request.Header.Set("X-App", "opencode")
	c.Request.Header.Set("Anthropic-Beta", "custom-beta")

	svc := &GatewayService{
		cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}},
	}
	account := &Account{ID: 10, Platform: PlatformAnthropic, Type: AccountTypeOAuth}

	req, _, err := svc.buildCountTokensRequest(context.Background(), c, account, []byte(`{"model":"claude-3-7-sonnet-20250219"}`), "oauth-token", "oauth", "claude-3-7-sonnet-20250219", claudeUpstreamForwardingMode{transparentOAuth: true})
	require.NoError(t, err)

	require.Equal(t, "opencode/1.0", getHeaderRaw(req.Header, "User-Agent"))
	require.Equal(t, "opencode", getHeaderRaw(req.Header, "X-App"))
	require.Equal(t, "custom-beta", getHeaderRaw(req.Header, "anthropic-beta"))
	require.NotContains(t, getHeaderRaw(req.Header, "anthropic-beta"), claude.BetaTokenCounting)
	require.NotContains(t, getHeaderRaw(req.Header, "anthropic-beta"), claude.BetaOAuth)
	require.NotContains(t, getHeaderRaw(req.Header, "anthropic-beta"), claude.BetaClaudeCode)
}
