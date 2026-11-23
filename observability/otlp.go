package observability

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"sync"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// TelemetryConfig selects an OTLP gRPC collector. HTTP is explicit plaintext;
// HTTPS uses TLS. A nil log level means INFO. No environment is read for level.
type TelemetryConfig struct {
	ServiceName string
	Endpoint    string
	LogLevel    slog.Leveler
}
type TelemetryGuard struct {
	traces *sdktrace.TracerProvider
	logs   *sdklog.LoggerProvider
}

// Shutdown attempts both providers even if one fails. Supply a bounded context.
func (g *TelemetryGuard) Shutdown(ctx context.Context) error {
	return errors.Join(g.traces.Shutdown(ctx), g.logs.Shutdown(ctx))
}

var telemetryMu sync.Mutex
var telemetryInstalled bool

// InitTelemetry explicitly installs global trace and slog providers once.
// Retain the guard and call Shutdown during graceful application shutdown.
func InitTelemetry(ctx context.Context, config TelemetryConfig) (*TelemetryGuard, error) {
	telemetryMu.Lock()
	defer telemetryMu.Unlock()
	if telemetryInstalled {
		return nil, errors.New("telemetry is already installed")
	}
	if config.ServiceName == "" {
		return nil, errors.New("telemetry service name must not be empty")
	}
	endpoint, err := url.Parse(config.Endpoint)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || (endpoint.Path != "" && endpoint.Path != "/") {
		return nil, errors.New("telemetry endpoint must be an http or https collector URL without credentials, path, query or fragment")
	}
	traceOptions := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(endpoint.Host)}
	logOptions := []otlploggrpc.Option{otlploggrpc.WithEndpoint(endpoint.Host)}
	if endpoint.Scheme == "http" {
		traceOptions = append(traceOptions, otlptracegrpc.WithInsecure())
		logOptions = append(logOptions, otlploggrpc.WithInsecure())
	}
	traceExporter, err := otlptracegrpc.New(ctx, traceOptions...)
	if err != nil {
		return nil, fmt.Errorf("build OTLP trace exporter: %w", err)
	}
	logExporter, err := otlploggrpc.New(ctx, logOptions...)
	if err != nil {
		_ = traceExporter.Shutdown(ctx)
		return nil, fmt.Errorf("build OTLP log exporter: %w", err)
	}
	res := resource.NewSchemaless(attribute.String("service.name", config.ServiceName))
	traces := sdktrace.NewTracerProvider(sdktrace.WithResource(res), sdktrace.WithBatcher(traceExporter))
	logs := sdklog.NewLoggerProvider(sdklog.WithResource(res), sdklog.WithProcessor(sdklog.NewBatchProcessor(logExporter)))
	handler := otelslog.NewHandler("ros-observability", otelslog.WithLoggerProvider(logs))
	level := config.LogLevel
	if level == nil {
		level = slog.LevelInfo
	}
	slog.SetDefault(slog.New(&levelHandler{Handler: handler, level: level}))
	otel.SetTracerProvider(traces)
	telemetryInstalled = true
	return &TelemetryGuard{traces, logs}, nil
}

// otelslog intentionally exports every level; apply the service's log threshold.
type levelHandler struct {
	slog.Handler
	level slog.Leveler
}

func (h *levelHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.level.Level() && h.Handler.Enabled(ctx, level)
}
func (h *levelHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &levelHandler{h.Handler.WithAttrs(attrs), h.level}
}
func (h *levelHandler) WithGroup(name string) slog.Handler {
	return &levelHandler{h.Handler.WithGroup(name), h.level}
}
