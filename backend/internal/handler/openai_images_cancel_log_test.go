package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestImagesRequestCancelLoggerLogsCancelMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sink, cleanup := captureHandlerStructuredLog(t)
	defer cleanup()

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodPost, "/v1/images/edits", strings.NewReader("body")).WithContext(ctx)
	req.Host = "parallelfuture-ai.up.railway.app"
	req.RemoteAddr = "192.0.2.10:443"
	req.Header.Set("User-Agent", "litellm/1.86.1")
	req.Header.Set("X-Forwarded-Host", "parallelfuture.ai")
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Railway-Request-Id", "railway-rid")
	req.Header.Set("X-Railway-Edge-Request-Id", "edge-rid")
	req.Header.Set("Authorization", "Bearer should-not-be-logged")
	baseLog := logger.With(
		zap.String("component", "handler.openai_gateway.images"),
		zap.String("request_id", "local-rid"),
		zap.String("client_request_id", "client-rid"),
	)
	req = req.WithContext(logger.IntoContext(req.Context(), baseLog))
	c.Request = req

	cancel()
	logOpenAIImagesRequestContextCanceled(c, baseLog, c.Request.Context().Err(), openAIImagesRequestCancelLogFields{
		Phase:     "waiting_upstream",
		Start:     time.Now().Add(-35 * time.Second),
		Model:     "gpt-image-2",
		AccountID: 16,
		Platform:  "openai",
	})

	var eventFields map[string]any
	sink.mu.Lock()
	for _, event := range sink.events {
		if event != nil && event.Message == "openai.images.request_context_canceled" {
			eventFields = event.Fields
			break
		}
	}
	sink.mu.Unlock()
	require.NotNil(t, eventFields, "expected cancel log event")
	require.Equal(t, "waiting_upstream", eventFields["phase"])
	require.Equal(t, "context canceled", eventFields["context_error"])
	require.Equal(t, "parallelfuture-ai.up.railway.app", eventFields["host"])
	require.Equal(t, "parallelfuture.ai", eventFields["forwarded_host"])
	require.Equal(t, "https", eventFields["forwarded_proto"])
	require.Equal(t, "railway-rid", eventFields["railway_request_id"])
	require.Equal(t, "edge-rid", eventFields["railway_edge_request_id"])
	require.Equal(t, "litellm/1.86.1", eventFields["user_agent"])
	require.Equal(t, "gpt-image-2", eventFields["model"])
	require.Equal(t, "openai", eventFields["platform"])
	require.Equal(t, "16", fmt.Sprint(eventFields["account_id"]))
	require.NotContains(t, eventFields, "authorization")
}
