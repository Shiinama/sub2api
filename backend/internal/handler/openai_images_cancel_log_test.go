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
		Phase:                   "waiting_upstream",
		Start:                   time.Now().Add(-35 * time.Second),
		Model:                   "gpt-image-2",
		AccountID:               16,
		Platform:                "openai",
		AppResponseStarted:      false,
		AppResponseBytesWritten: 0,
		UpstreamStarted:         true,
		UpstreamHeadersReceived: false,
		UpstreamLatencyMs:       35000,
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
	require.Equal(t, false, eventFields["app_response_started"])
	require.Equal(t, "0", fmt.Sprint(eventFields["app_response_bytes_written"]))
	require.Equal(t, true, eventFields["upstream_started"])
	require.Equal(t, false, eventFields["upstream_headers_received"])
	require.Equal(t, "35000", fmt.Sprint(eventFields["upstream_latency_ms"]))
	require.Equal(t, "downstream", eventFields["termination_actor"])
	require.Equal(t, "downstream_closed_before_app_response", eventFields["termination_cause"])
	require.Equal(t, "medium", eventFields["termination_confidence"])
	require.Contains(t, fmt.Sprint(eventFields["termination_evidence"]), "no_app_response_started")
	require.Contains(t, fmt.Sprint(eventFields["termination_evidence"]), "no_upstream_headers")
	require.NotContains(t, eventFields, "authorization")
}

func TestClassifyOpenAIImagesCancellation(t *testing.T) {
	gin.SetMode(gin.TestMode)

	withRailwayRequestID := func() *gin.Context {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		req := httptest.NewRequest(http.MethodPost, "/v1/images/edits", nil)
		req.Header.Set("X-Railway-Request-Id", "railway-rid")
		c.Request = req
		return c
	}

	tests := []struct {
		name           string
		data           openAIImagesRequestCancelLogFields
		elapsedMs      int64
		wantActor      string
		wantCause      string
		wantConfidence string
	}{
		{
			name: "waiting upstream near 300s is railway first byte timeout",
			data: openAIImagesRequestCancelLogFields{
				Phase:                   "waiting_upstream",
				AppResponseStarted:      false,
				UpstreamStarted:         true,
				UpstreamHeadersReceived: false,
			},
			elapsedMs:      299971,
			wantActor:      "railway_edge",
			wantCause:      "edge_first_byte_timeout_300s",
			wantConfidence: "high",
		},
		{
			name: "reading body near 300s is railway body read timeout",
			data: openAIImagesRequestCancelLogFields{
				Phase:              "reading_body",
				AppResponseStarted: false,
			},
			elapsedMs:      300010,
			wantActor:      "railway_edge",
			wantCause:      "edge_body_read_timeout_300s",
			wantConfidence: "high",
		},
		{
			name: "early cancellation is downstream but not attributed to railway edge",
			data: openAIImagesRequestCancelLogFields{
				Phase:              "waiting_upstream",
				AppResponseStarted: false,
				UpstreamStarted:    true,
			},
			elapsedMs:      120000,
			wantActor:      "downstream",
			wantCause:      "downstream_closed_before_app_response",
			wantConfidence: "medium",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyOpenAIImagesCancellation(withRailwayRequestID(), context.Canceled, tt.data, tt.elapsedMs)
			require.Equal(t, tt.wantActor, got.Actor)
			require.Equal(t, tt.wantCause, got.Cause)
			require.Equal(t, tt.wantConfidence, got.Confidence)
		})
	}
}
