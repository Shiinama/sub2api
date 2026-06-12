package handler

import (
	"strings"
	"time"

	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type openAIImagesRequestCancelLogFields struct {
	Phase     string
	Start     time.Time
	Model     string
	Stream    bool
	AccountID int64
	Platform  string
}

func logOpenAIImagesRequestContextCanceled(c *gin.Context, reqLog *zap.Logger, contextErr error, data openAIImagesRequestCancelLogFields) {
	if c == nil || c.Request == nil || reqLog == nil || contextErr == nil {
		return
	}

	fields := imagesRequestCancelStaticFields(c)
	fields = append(fields,
		zap.String("phase", strings.TrimSpace(data.Phase)),
		zap.String("context_error", contextErr.Error()),
		zap.Bool("stream", data.Stream),
	)
	if !data.Start.IsZero() {
		fields = append(fields, zap.Int64("elapsed_ms", time.Since(data.Start).Milliseconds()))
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
