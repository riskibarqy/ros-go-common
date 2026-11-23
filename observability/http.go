package observability

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/felixge/httpsnoop"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/riskibarqy/ros-go-common/requestcontext"
)

// RequestCompletionMiddleware logs method, path without query, status, and
// elapsed handler time. It never logs headers, bodies, or query parameters.
// Unlike Tower's response future, net/http runs until the handler returns,
// so streaming time is included. Keep the request ID middleware outermost.
func RequestCompletionMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := ExtractHTTPContext(r.Context(), r.Header)
			ctx, span := otel.Tracer("ros-observability").Start(ctx, "http.request", trace.WithSpanKind(trace.SpanKindServer), trace.WithAttributes(
				attribute.String("http.request.method", r.Method), attribute.String("url.path", r.URL.Path),
			))
			defer span.End()
			started := time.Now()
			metrics := httpsnoop.CaptureMetrics(next, w, r.WithContext(ctx))
			span.SetAttributes(attribute.Int("http.response.status_code", metrics.Code))
			if metrics.Code >= 500 {
				span.SetStatus(codes.Error, "HTTP server error")
			}
			fields := []any{"method", r.Method, "path", r.URL.Path, "status", metrics.Code, "elapsed", time.Since(started)}
			if request, ok := requestcontext.FromContext(ctx); ok {
				fields = append(fields, "request_id", request.RequestID().String())
			}
			logger.InfoContext(ctx, "HTTP request completed", fields...)
		})
	}
}
