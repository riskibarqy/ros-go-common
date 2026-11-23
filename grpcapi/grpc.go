// Package grpcapi adapts request context and failure categories to grpc-go.
package grpcapi

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"ros-go-common/failure"
	"ros-go-common/requestcontext"
)

const RequestIDMetadataKey = "x-request-id"

// InsertRequestContext preserves other outgoing metadata and any earlier deadline.
// The caller must call cancel even when the request has no deadline.
// grpc-go encodes the context deadline as grpc-timeout automatically.
func InsertRequestContext(ctx context.Context, request requestcontext.RequestContext) (context.Context, context.CancelFunc, error) {
	if !request.RequestID().Valid() {
		return nil, nil, requestcontext.ErrInvalidRequestID
	}
	md, _ := metadata.FromOutgoingContext(ctx)
	md = md.Copy()
	md.Set(RequestIDMetadataKey, request.RequestID().String())
	ctx = metadata.NewOutgoingContext(ctx, md)
	if deadline, ok := request.Deadline(); ok {
		result, cancel := context.WithDeadline(ctx, deadline)
		return result, cancel, nil
	}
	result, cancel := context.WithCancel(ctx)
	return result, cancel, nil
}

// RequestIDFromMetadata is for an explicitly trusted internal boundary only.
func RequestIDFromMetadata(md metadata.MD) (requestcontext.RequestID, bool, error) {
	values := md.Get(RequestIDMetadataKey)
	if len(values) == 0 {
		return requestcontext.RequestID{}, false, nil
	}
	id, err := requestcontext.ParseRequestID(values[0])
	return id, err == nil, err
}
func FailureKindForCode(code codes.Code) (failure.Kind, bool) {
	switch code {
	case codes.OK, codes.Canceled:
		return failure.Internal, false
	case codes.InvalidArgument:
		return failure.BadRequest, true
	case codes.Unauthenticated:
		return failure.Unauthorized, true
	case codes.PermissionDenied:
		return failure.Forbidden, true
	case codes.NotFound:
		return failure.NotFound, true
	case codes.AlreadyExists, codes.Aborted, codes.FailedPrecondition:
		return failure.Conflict, true
	case codes.ResourceExhausted:
		return failure.TooManyRequests, true
	case codes.DeadlineExceeded:
		return failure.DeadlineExceeded, true
	case codes.OutOfRange:
		return failure.Validation, true
	case codes.Unavailable:
		return failure.Unavailable, true
	case codes.Unimplemented:
		return failure.NotImplemented, true
	default:
		return failure.Internal, true
	}
}

// StatusFromFailure accepts only a message already safe for the remote client.
func StatusFromFailure(kind failure.Kind, safeMessage string) *status.Status {
	code := codes.Internal
	switch kind {
	case failure.BadRequest, failure.Validation, failure.UnsupportedMediaType:
		code = codes.InvalidArgument
	case failure.Unauthorized:
		code = codes.Unauthenticated
	case failure.Forbidden:
		code = codes.PermissionDenied
	case failure.NotFound:
		code = codes.NotFound
	case failure.Conflict:
		code = codes.FailedPrecondition
	case failure.TooManyRequests, failure.PayloadTooLarge:
		code = codes.ResourceExhausted
	case failure.DeadlineExceeded:
		code = codes.DeadlineExceeded
	case failure.MethodNotAllowed, failure.NotImplemented:
		code = codes.Unimplemented
	case failure.Unavailable:
		code = codes.Unavailable
	}
	return status.New(code, safeMessage)
}
