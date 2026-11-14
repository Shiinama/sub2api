package service

import (
	"encoding/json"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestChatCompletionBridgePreservesExplicitLimitsWithConfiguredFloor(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.MinOutputTokens = 128
	maxTokens, maxCompletion := 20, 5
	for _, stream := range []bool{false, true} {
		req := &apicompat.ChatCompletionsRequest{Model: "gpt-4o", Stream: stream,
			MaxTokens: &maxTokens, MaxCompletionTokens: &maxCompletion,
			Messages: []apicompat.ChatMessage{{Role: "user", Content: json.RawMessage(`"hi"`)}},
		}
		converted, err := chatCompletionsToResponses(cfg, req)
		require.NoError(t, err)
		require.NotNil(t, converted.MaxOutputTokens)
		require.Equal(t, 5, *converted.MaxOutputTokens)
	}
}
