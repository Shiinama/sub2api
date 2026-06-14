package handler

import (
	"context"
	"errors"
	"strings"
	"time"

	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type openAIImagesRequestCancelLogFields struct {
	Phase                   string
	Start                   time.Time
	Model                   string
	Stream                  bool
	AccountID               int64
	Platform                string
	AppResponseStarted      bool
	AppResponseBytesWritten int
	UpstreamStarted         bool
	UpstreamHeadersReceived bool
	UpstreamLatencyMs       int64
}

func logOpenAIImagesRequestContextCanceled(c *gin.Context, reqLog *zap.Logger, contextErr error, data openAIImagesRequestCancelLogFields) {
	if c == nil || c.Request == nil || reqLog == nil || contextErr == nil {
		return
	}

	fields := imagesRequestCancelStaticFields(c)
	elapsedMs := int64(0)
	if !data.Start.IsZero() {
		elapsedMs = time.Since(data.Start).Milliseconds()
	}
	termination := classifyOpenAIImagesCancellation(c, contextErr, data, elapsedMs)
	fields = append(fields,
		zap.String("phase", strings.TrimSpace(data.Phase)),
		zap.String("context_error", contextErr.Error()),
		zap.Bool("stream", data.Stream),
		zap.Bool("app_response_started", data.AppResponseStarted),
		zap.Int("app_response_bytes_written", data.AppResponseBytesWritten),
		zap.Bool("upstream_started", data.UpstreamStarted),
		zap.Bool("upstream_headers_received", data.UpstreamHeadersReceived),
		zap.String("termination_actor", termination.Actor),
		zap.String("termination_cause", termination.Cause),
		zap.String("termination_confidence", termination.Confidence),
		zap.Strings("termination_evidence", termination.Evidence),
	)
	if elapsedMs > 0 {
		fields = append(fields, zap.Int64("elapsed_ms", elapsedMs))
	}
	if data.UpstreamLatencyMs > 0 {
		fields = append(fields, zap.Int64("upstream_latency_ms", data.UpstreamLatencyMs))
	}
	if model := strings.TrimSpace(data.Model); model != "" {
		fields = append(fields, zap.String("model", model))
	}
	if data.AccountID > 0 {
		fields = append(fields, zap.Int64("account_id", data.AccountID))
	}
	if platform := strings.TrimSpace(data.Platform); platform != "" {
		fields = append(fields, zap.String("platform", platform))
	}

	reqLog.Warn("openai.images.request_context_canceled", fields...)
}

type openAIImagesCancellationClassification struct {
	Actor      string
	Cause      string
	Confidence string
	Evidence   []string
}

func classifyOpenAIImagesCancellation(
	c *gin.Context,
	contextErr error,
	data openAIImagesRequestCancelLogFields,
	elapsedMs int64,
) openAIImagesCancellationClassification {
	evidence := []string{}
	if phase := strings.TrimSpace(data.Phase); phase != "" {
		evidence = append(evidence, "phase="+phase)
	}
	if contextErr != nil {
		evidence = append(evidence, "context_error="+contextErr.Error())
	}
	if data.AppResponseStarted {
		evidence = append(evidence, "app_response_started")
	} else {
		evidence = append(evidence, "no_app_response_started")
	}
	if data.UpstreamHeadersReceived {
		evidence = append(evidence, "upstream_headers_received")
	} else {
		evidence = append(evidence, "no_upstream_headers")
	}
	if data.UpstreamStarted {
		evidence = append(evidence, "upstream_started")
	}
	if elapsedMs >= 295000 && elapsedMs <= 305000 {
		evidence = append(evidence, "elapsed_near_300s")
	}
	hasRailwayRequestID := c != nil && strings.TrimSpace(c.GetHeader("X-Railway-Request-Id")) != ""
	if hasRailwayRequestID {
		evidence = append(evidence, "railway_request_id_present")
	}

	phase := strings.TrimSpace(data.Phase)
	contextCanceled := errors.Is(contextErr, context.Canceled)
	if !contextCanceled && contextErr != nil {
		contextCanceled = strings.EqualFold(strings.TrimSpace(contextErr.Error()), context.Canceled.Error())
	}
	nearRailway300s := elapsedMs >= 295000 && elapsedMs <= 305000
	if contextCanceled &&
		phase == "reading_body" &&
		nearRailway300s &&
		hasRailwayRequestID &&
		!data.AppResponseStarted {
		return openAIImagesCancellationClassification{
			Actor:      "railway_edge",
			Cause:      "edge_body_read_timeout_300s",
			Confidence: "high",
			Evidence:   evidence,
		}
	}
	if contextCanceled &&
		phase == "waiting_upstream" &&
		nearRailway300s &&
		hasRailwayRequestID &&
		!data.AppResponseStarted &&
		!data.UpstreamHeadersReceived {
		return openAIImagesCancellationClassification{
			Actor:      "railway_edge",
			Cause:      "edge_first_byte_timeout_300s",
			Confidence: "high",
			Evidence:   evidence,
		}
	}
	if contextCanceled && !data.AppResponseStarted {
		return openAIImagesCancellationClassification{
			Actor:      "downstream",
			Cause:      "downstream_closed_before_app_response",
			Confidence: "medium",
			Evidence:   evidence,
		}
	}
	if contextCanceled {
		return openAIImagesCancellationClassification{
			Actor:      "downstream",
			Cause:      "downstream_closed_after_app_response_started",
			Confidence: "medium",
			Evidence:   evidence,
		}
	}
	return openAIImagesCancellationClassification{
		Actor:      "unknown",
		Cause:      "request_context_done",
		Confidence: "low",
		Evidence:   evidence,
	}
}

func imagesRequestCancelStaticFields(c *gin.Context) []zap.Field {
	fields := []zap.Field{
		zap.String("method", c.Request.Method),
		zap.String("path", c.Request.URL.Path),
		zap.String("protocol", c.Request.Proto),
		zap.String("client_ip", ip.GetClientIP(c)),
		zap.Int64("content_length", c.Request.ContentLength),
	}
	if value := pkghttputil.SanitizeHeaderValueForLog(c.Request.Host); value != "" {
		fields = append(fields, zap.String("host", value))
	}
	if value := pkghttputil.SanitizeHeaderValueForLog(c.GetHeader("User-Agent")); value != "" {
		fields = append(fields, zap.String("user_agent", value))
	}
	headerFields := []struct {
		field  string
		header string
	}{
		{"forwarded_host", "X-Forwarded-Host"},
		{"forwarded_proto", "X-Forwarded-Proto"},
		{"railway_request_id", "X-Railway-Request-Id"},
		{"railway_edge_request_id", "X-Railway-Edge-Request-Id"},
	}
	for _, item := range headerFields {
		if value := pkghttputil.SanitizeHeaderValueForLog(c.GetHeader(item.header)); value != "" {
			fields = append(fields, zap.String(item.field, value))
		}
	}
	return fields
}
