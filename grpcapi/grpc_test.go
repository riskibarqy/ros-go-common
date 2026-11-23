package grpcapi_test

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"ros-go-common/failure"
	"ros-go-common/grpcapi"
	"ros-go-common/requestcontext"
)

func TestMappings(t *testing.T) {
	for _, tc := range []struct {
		code codes.Code
		kind failure.Kind
		ok   bool
	}{
		{codes.OK, failure.Internal, false}, {codes.Canceled, failure.Internal, false},
		{codes.InvalidArgument, failure.BadRequest, true}, {codes.Unauthenticated, failure.Unauthorized, true},
		{codes.PermissionDenied, failure.Forbidden, true}, {codes.NotFound, failure.NotFound, true},
		{codes.AlreadyExists, failure.Conflict, true}, {codes.Aborted, failure.Conflict, true}, {codes.FailedPrecondition, failure.Conflict, true},
		{codes.ResourceExhausted, failure.TooManyRequests, true}, {codes.DeadlineExceeded, failure.DeadlineExceeded, true},
		{codes.OutOfRange, failure.Validation, true}, {codes.Unavailable, failure.Unavailable, true}, {codes.Unimplemented, failure.NotImplemented, true},
		{codes.Unknown, failure.Internal, true}, {codes.Internal, failure.Internal, true}, {codes.DataLoss, failure.Internal, true},
	} {
		kind, ok := grpcapi.FailureKindForCode(tc.code)
		if kind != tc.kind || ok != tc.ok {
			t.Fatal(tc, kind, ok)
		}
	}
	for _, tc := range []struct {
		kind failure.Kind
		code codes.Code
	}{
		{failure.BadRequest, codes.InvalidArgument}, {failure.Validation, codes.InvalidArgument}, {failure.UnsupportedMediaType, codes.InvalidArgument},
		{failure.Unauthorized, codes.Unauthenticated}, {failure.Forbidden, codes.PermissionDenied}, {failure.NotFound, codes.NotFound},
		{failure.Conflict, codes.FailedPrecondition}, {failure.TooManyRequests, codes.ResourceExhausted}, {failure.PayloadTooLarge, codes.ResourceExhausted},
		{failure.DeadlineExceeded, codes.DeadlineExceeded}, {failure.MethodNotAllowed, codes.Unimplemented}, {failure.NotImplemented, codes.Unimplemented},
		{failure.Unavailable, codes.Unavailable}, {failure.Internal, codes.Internal},
	} {
		s := grpcapi.StatusFromFailure(tc.kind, "safe message")
		if s.Code() != tc.code || s.Message() != "safe message" {
			t.Fatal(tc, s)
		}
	}
	s := status.New(codes.Internal, "private diagnostic")
	problem := grpcapi.ProblemFromGRPCStatus(s, func(s *status.Status) failure.Kind { return failure.NotFound })
	if problem.Status() != 404 || problem.Detail != nil {
		t.Fatal(problem)
	}
}
func TestRequestMetadata(t *testing.T) {
	for _, tc := range []struct {
		md               metadata.MD
		present, invalid bool
	}{
		{metadata.MD{}, false, false}, {metadata.Pairs("x-request-id", "trusted.id-1"), true, false}, {metadata.Pairs("x-request-id", "bad id"), false, true},
	} {
		_, ok, err := grpcapi.RequestIDFromMetadata(tc.md)
		if ok != tc.present || (err != nil) != tc.invalid {
			t.Fatal(tc, ok, err)
		}
	}
	request, _ := requestcontext.New(requestcontext.GenerateRequestID())
	original := metadata.Pairs("authorization", "private", "x-request-id", "old")
	parent := metadata.NewOutgoingContext(context.Background(), original)
	ctx, cancel, err := grpcapi.InsertRequestContext(parent, request)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	md, _ := metadata.FromOutgoingContext(ctx)
	if md.Get("x-request-id")[0] != request.RequestID().String() || md.Get("authorization")[0] != "private" || original.Get("x-request-id")[0] != "old" {
		t.Fatal("metadata was lost or mutated")
	}
	if _, ok := ctx.Deadline(); ok {
		t.Fatal("invented timeout")
	}
	request, _ = request.WithTimeout(time.Minute)
	deadline := time.Now().Add(30 * time.Second)
	parent, parentCancel := context.WithDeadline(parent, deadline)
	defer parentCancel()
	ctx, cancel, err = grpcapi.InsertRequestContext(parent, request)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	if got, ok := ctx.Deadline(); !ok || !got.Equal(deadline) {
		t.Fatal(got)
	}
	if _, _, err := grpcapi.InsertRequestContext(parent, requestcontext.RequestContext{}); err == nil {
		t.Fatal("accepted zero request context")
	}
}

type observedHealth struct {
	healthpb.UnimplementedHealthServer
	t  *testing.T
	id string
}

func (s *observedHealth) Check(ctx context.Context, _ *healthpb.HealthCheckRequest) (*healthpb.HealthCheckResponse, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	id, ok, err := grpcapi.RequestIDFromMetadata(md)
	if err != nil || !ok || id.String() != s.id {
		s.t.Error("missing wire request ID", err)
	}
	if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) <= 0 || time.Until(deadline) > time.Minute {
		s.t.Error("missing wire deadline", deadline)
	}
	return &healthpb.HealthCheckResponse{Status: healthpb.HealthCheckResponse_SERVING}, nil
}
func TestGRPCWirePropagation(t *testing.T) {
	listener := bufconn.Listen(1024 * 1024)
	defer listener.Close()
	request, _ := requestcontext.New(requestcontext.GenerateRequestID())
	request, _ = request.WithTimeout(time.Minute)
	server := grpc.NewServer()
	healthpb.RegisterHealthServer(server, &observedHealth{t: t, id: request.RequestID().String()})
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	defer func() { server.Stop(); <-serveDone }()
	connection, err := grpc.NewClient("passthrough:///bufnet", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }))
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	parent, parentCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer parentCancel()
	ctx, cancel, err := grpcapi.InsertRequestContext(parent, request)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	response, err := healthpb.NewHealthClient(connection).Check(ctx, &healthpb.HealthCheckRequest{})
	if err != nil || response.Status != healthpb.HealthCheckResponse_SERVING {
		t.Fatal(response, err)
	}
}
