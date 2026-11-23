package observability

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	logcollector "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	tracecollector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"ros-go-common/httpapi"
)

func TestPropagation(t *testing.T) {
	const parent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	headers := http.Header{"Traceparent": []string{parent}, "Tracestate": []string{"vendor=value"}}
	ctx := ExtractHTTPContext(context.Background(), headers)
	span := trace.SpanContextFromContext(ctx)
	if !span.IsValid() || !span.IsRemote() || span.TraceState().String() != "vendor=value" {
		t.Fatal(span)
	}
	outgoing := http.Header{}
	InjectHTTPContext(ctx, outgoing)
	if outgoing.Get("Traceparent") != parent || outgoing.Get("Tracestate") != "vendor=value" {
		t.Fatal(outgoing)
	}
	original := metadata.Pairs("other", "preserved")
	md := InjectGRPCContext(ctx, original)
	if md.Get("traceparent")[0] != parent || len(original.Get("traceparent")) != 0 || md.Get("other")[0] != "preserved" {
		t.Fatal(md)
	}
	grpcContext := ExtractGRPCContext(context.Background(), md)
	if got := trace.SpanContextFromContext(grpcContext); got.TraceID() != span.TraceID() || !got.IsRemote() {
		t.Fatal(got)
	}
	invalid := http.Header{"Traceparent": []string{"00-00000000000000000000000000000000-0000000000000000-01"}}
	if trace.SpanContextFromContext(ExtractHTTPContext(context.Background(), invalid)).IsValid() {
		t.Fatal("accepted invalid trace context")
	}
	blank := http.Header{}
	InjectHTTPContext(context.Background(), blank)
	if len(blank) != 0 {
		t.Fatal(blank)
	}
}
func TestSafeHTTPCompletionLogs(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	handler := httpapi.RequestIDMiddleware(RequestCompletionMiddleware(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := w.(http.Flusher); !ok {
			t.Error("lost Flusher")
		}
		w.WriteHeader(201)
	})))
	request := httptest.NewRequest("POST", "/items?token=private-query", strings.NewReader("private-body"))
	request.Header.Set("Authorization", "private-header")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, request)
	output := logs.String()
	for _, value := range []string{"private-query", "private-body", "private-header", "token="} {
		if strings.Contains(output, value) {
			t.Fatal("unsafe log", output)
		}
	}
	for _, value := range []string{`"method":"POST"`, `"path":"/items"`, `"status":201`, w.Header().Get("X-Request-ID")} {
		if !strings.Contains(output, value) {
			t.Fatal("missing field", value, output)
		}
	}
}

type traceSink struct {
	tracecollector.UnimplementedTraceServiceServer
	requests chan *tracecollector.ExportTraceServiceRequest
}

func (s *traceSink) Export(_ context.Context, r *tracecollector.ExportTraceServiceRequest) (*tracecollector.ExportTraceServiceResponse, error) {
	s.requests <- r
	return &tracecollector.ExportTraceServiceResponse{}, nil
}

type logSink struct {
	logcollector.UnimplementedLogsServiceServer
	requests chan *logcollector.ExportLogsServiceRequest
}

func (s *logSink) Export(_ context.Context, r *logcollector.ExportLogsServiceRequest) (*logcollector.ExportLogsServiceResponse, error) {
	s.requests <- r
	return &logcollector.ExportLogsServiceResponse{}, nil
}
func TestOTLPExportAndShutdown(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	traces := &traceSink{requests: make(chan *tracecollector.ExportTraceServiceRequest, 10)}
	logs := &logSink{requests: make(chan *logcollector.ExportLogsServiceRequest, 10)}
	server := grpc.NewServer()
	tracecollector.RegisterTraceServiceServer(server, traces)
	logcollector.RegisterLogsServiceServer(server, logs)
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	defer func() { server.Stop(); <-serveDone; _ = listener.Close() }()
	previousTrace, previousLogger := otel.GetTracerProvider(), slog.Default()
	defer func() {
		otel.SetTracerProvider(previousTrace)
		slog.SetDefault(previousLogger)
		telemetryMu.Lock()
		telemetryInstalled = false
		telemetryMu.Unlock()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	guard, err := InitTelemetry(ctx, TelemetryConfig{ServiceName: "test-service", Endpoint: "http://" + listener.Addr().String()})
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Shutdown(ctx)
	spanCtx, span := otel.Tracer("test").Start(ctx, "test-span")
	span.SetAttributes(attribute.String("test.attribute", "present"))
	slog.Default().With("component", "test").InfoContext(spanCtx, "exported-event")
	slog.Default().WithGroup("group").DebugContext(spanCtx, "filtered-event")
	span.End()
	if _, err := InitTelemetry(ctx, TelemetryConfig{}); err == nil {
		t.Fatal("allowed repeated global initialization")
	}
	if err := guard.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case batch := <-traces.requests:
		found, service := false, false
		for _, res := range batch.ResourceSpans {
			for _, attr := range res.Resource.Attributes {
				if attr.Key == "service.name" && attr.Value.GetStringValue() == "test-service" {
					service = true
				}
			}
			for _, scope := range res.ScopeSpans {
				for _, span := range scope.Spans {
					if span.Name == "test-span" {
						found = true
					}
				}
			}
		}
		if !found || !service {
			t.Fatal("trace or service resource not exported")
		}
	case <-ctx.Done():
		t.Fatal("no OTLP traces exported")
	}
	select {
	case batch := <-logs.requests:
		found := false
		for _, res := range batch.ResourceLogs {
			for _, scope := range res.ScopeLogs {
				for _, record := range scope.LogRecords {
					if record.Body.GetStringValue() == "filtered-event" {
						t.Error("debug log not filtered")
					}
					if record.Body.GetStringValue() == "exported-event" {
						found = true
						if len(record.TraceId) != 16 || len(record.SpanId) != 8 {
							t.Error("log missing trace correlation")
						}
					}
				}
			}
		}
		if !found {
			t.Fatal("log not exported")
		}
	case <-ctx.Done():
		t.Fatal("no OTLP logs exported")
	}
}
func TestInvalidTelemetryConfig(t *testing.T) {
	for _, config := range []TelemetryConfig{
		{ServiceName: "", Endpoint: "http://localhost:4317"},
		{ServiceName: "test", Endpoint: "not-a-url"},
		{ServiceName: "test", Endpoint: "http://user:password@localhost:4317"},
		{ServiceName: "test", Endpoint: "http://localhost:4317?token=private"},
	} {
		if _, err := InitTelemetry(context.Background(), config); err == nil {
			t.Fatal("accepted unsafe config")
		}
	}
}
