// Package observability provides W3C propagation, completion logs, and OTLP setup.
// No global provider is installed merely by importing this package.
package observability

import (
	"context"
	"net/http"

	"go.opentelemetry.io/otel/propagation"
	"google.golang.org/grpc/metadata"
)

func ExtractHTTPContext(ctx context.Context, headers http.Header) context.Context {
	return (propagation.TraceContext{}).Extract(ctx, propagation.HeaderCarrier(headers))
}
func InjectHTTPContext(ctx context.Context, headers http.Header) {
	(propagation.TraceContext{}).Inject(ctx, propagation.HeaderCarrier(headers))
}

type metadataCarrier metadata.MD

func (m metadataCarrier) Get(key string) string {
	values := metadata.MD(m).Get(key)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
func (m metadataCarrier) Set(key, value string) { metadata.MD(m).Set(key, value) }
func (m metadataCarrier) Keys() []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	return keys
}
func ExtractGRPCContext(ctx context.Context, md metadata.MD) context.Context {
	return (propagation.TraceContext{}).Extract(ctx, metadataCarrier(md))
}

// InjectGRPCContext returns a copy so shared metadata is never mutated.
func InjectGRPCContext(ctx context.Context, md metadata.MD) metadata.MD {
	copy := md.Copy()
	(propagation.TraceContext{}).Inject(ctx, metadataCarrier(copy))
	return copy
}
