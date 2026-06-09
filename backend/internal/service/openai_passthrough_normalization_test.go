package service

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestNormalizeOpenAIPassthroughOAuthBody_RemovesUnsupportedUser(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","input":"hello","user":"user_123","metadata":{"user_id":"user_123"},"prompt_cache_retention":"24h","safety_identifier":"sid","stream_options":{"include_usage":true}}`)

	normalized, changed, err := normalizeOpenAIPassthroughOAuthBody(body, false)
	require.NoError(t, err)
	require.True(t, changed)
	for _, field := range openAIChatGPTInternalUnsupportedFields {
		require.False(t, gjson.GetBytes(normalized, field).Exists(), "%s should be stripped", field)
	}
	require.True(t, gjson.GetBytes(normalized, "stream").Bool())
	require.False(t, gjson.GetBytes(normalized, "store").Bool())
}

func TestNormalizeOpenAIPassthroughOAuthBody_CompactRemovesUnsupportedUser(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","input":"hello","user":"user_123","metadata":{"user_id":"user_123"},"stream":true,"store":true}`)

	normalized, changed, err := normalizeOpenAIPassthroughOAuthBody(body, true)
	require.NoError(t, err)
	require.True(t, changed)
	require.False(t, gjson.GetBytes(normalized, "user").Exists())
	require.False(t, gjson.GetBytes(normalized, "metadata").Exists())
	require.False(t, gjson.GetBytes(normalized, "stream").Exists())
	require.False(t, gjson.GetBytes(normalized, "store").Exists())
}

func TestNormalizeOpenAIPassthroughOAuthBody_MissingInstructionsUsesMinimalDefault(t *testing.T) {
	for _, model := range []string{"gpt-5.5", "gpt-5.2", "gpt-5.1-codex-max", "codex-auto-review"} {
		t.Run(model, func(t *testing.T) {
			body := []byte(fmt.Sprintf(`{"model":%q,"input":"hello"}`, model))

			normalized, changed, err := normalizeOpenAIPassthroughOAuthBody(body, false)
			require.NoError(t, err)
			require.True(t, changed)
			require.Equal(t, "You are a helpful assistant.", strings.TrimSpace(gjson.GetBytes(normalized, "instructions").String()))
		})
	}
}

func TestNormalizeOpenAIPassthroughOAuthBody_PreservesInstructions(t *testing.T) {
	body := []byte(`{"model":"gpt-5.5","input":"hello","instructions":"client instructions"}`)

	normalized, _, err := normalizeOpenAIPassthroughOAuthBody(body, false)
	require.NoError(t, err)
	require.Equal(t, "client instructions", gjson.GetBytes(normalized, "instructions").String())
}
