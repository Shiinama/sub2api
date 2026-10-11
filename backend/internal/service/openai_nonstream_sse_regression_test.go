//go:build unit

package service

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const nonStreamingLunaResponse = `{"id":"resp_luna_nonstream","object":"response","model":"gpt-5.6-luna","status":"completed","instructions":"You are a coding assistant.\nAnswer the user clearly.","output":[{"id":"msg_luna","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"hello","annotations":[]}]}],"usage":{"input_tokens":13,"output_tokens":5,"total_tokens":18,"input_tokens_details":{"cached_tokens":3}}}`

func TestOpenAINonStreamingResponses_TerminalOnlySSE(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, passthrough := range []bool{false, true} {
		route := "standard"
		if passthrough {
			route = "passthrough"
		}
		for _, accountType := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
			for _, contentType := range []struct {
				name  string
				value string
			}{
				{name: "event_stream", value: "text/event-stream"},
				{name: "json", value: "application/json"},
				{name: "plain_text", value: "text/plain"},
				{name: "missing"},
			} {
				t.Run(route+"/"+accountType+"/"+contentType.name, func(t *testing.T) {
					// Some upstreams send only the completed event, without deltas,
					// an event: field, or a trailing [DONE] marker.
					body := "data: {\"type\":\"response.completed\",\"response\":" + nonStreamingLunaResponse + "}\n\n"
					rec, usage, responseID := runNonStreamingLunaResponse(t, passthrough, accountType, contentType.value, body)
					mediaType, _, err := mime.ParseMediaType(rec.Header().Get("Content-Type"))
					require.NoError(t, err)
					assert.Equal(t, "application/json", mediaType, "converted responses must be advertised as JSON")
					assert.NotContains(t, rec.Body.String(), "data:")
					assert.NotContains(t, rec.Body.String(), "event:")
					require.True(t, json.Valid(rec.Body.Bytes()), "response body: %s", rec.Body.String())
					assert.False(t, gjson.GetBytes(rec.Body.Bytes(), "response").Exists(), "the terminal event wrapper must be removed")
					assert.Equal(t, "response", gjson.GetBytes(rec.Body.Bytes(), "object").String())
					assert.Equal(t, "gpt-5.6-luna", gjson.GetBytes(rec.Body.Bytes(), "model").String())
					assert.Equal(t, "hello", gjson.GetBytes(rec.Body.Bytes(), "output.0.content.0.text").String())
					assert.Equal(t, "resp_luna_nonstream", gjson.GetBytes(rec.Body.Bytes(), "id").String())
					assert.Equal(t, int64(18), gjson.GetBytes(rec.Body.Bytes(), "usage.total_tokens").Int())
					assert.Equal(t, "resp_luna_nonstream", responseID)
					assert.Equal(t, 13, usage.InputTokens)
					assert.Equal(t, 5, usage.OutputTokens)
					assert.Equal(t, 3, usage.CacheReadInputTokens)
				})
			}
		}
	}
}

func TestOpenAINonStreamingResponses_JSONWithSSEText(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const text = "processing data: 1,2,3 then event: click finished"
	body := strings.Replace(nonStreamingLunaResponse, `"hello"`, `"`+text+`"`, 1)
	for _, passthrough := range []bool{false, true} {
		route := "standard"
		if passthrough {
			route = "passthrough"
		}
		for _, accountType := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
			t.Run(route+"/"+accountType, func(t *testing.T) {
				rec, usage, responseID := runNonStreamingLunaResponse(t, passthrough, accountType, "application/json", body)
				assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
				assert.JSONEq(t, body, rec.Body.String())
				assert.Equal(t, text, gjson.GetBytes(rec.Body.Bytes(), "output.0.content.0.text").String())
				assert.Equal(t, "resp_luna_nonstream", responseID)
				assert.Equal(t, 13, usage.InputTokens)
				assert.Equal(t, 5, usage.OutputTokens)
				assert.Equal(t, 3, usage.CacheReadInputTokens)
			})
		}
	}
}

func runNonStreamingLunaResponse(t *testing.T, passthrough bool, accountType, contentType, body string) (*httptest.ResponseRecorder, *OpenAIUsage, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	if contentType != "" {
		resp.Header.Set("Content-Type", contentType)
	}
	account := &Account{ID: 1, Type: accountType, Platform: PlatformOpenAI}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, toolCorrector: NewCodexToolCorrector()}
	if passthrough {
		// The passthrough header writer copies Content-Type with a nil filter too.
		result, err := svc.handleNonStreamingResponsePassthrough(context.Background(), resp, c, account, "gpt-5.6-luna", "gpt-5.6-luna")
		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotNil(t, result.OpenAIUsage)
		assert.Equal(t, http.StatusOK, rec.Code)
		return rec, result.OpenAIUsage, result.responseID
	}
	// Match the normal service's header filter, which forwards Content-Type.
	svc.responseHeaderFilter = responseheaders.CompileHeaderFilter(config.ResponseHeaderConfig{})
	result, err := svc.handleNonStreamingResponse(context.Background(), resp, c, account, "gpt-5.6-luna", "gpt-5.6-luna")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.OpenAIUsage)
	assert.Equal(t, http.StatusOK, rec.Code)
	return rec, result.OpenAIUsage, result.responseID
}
