package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/felixge/httpsnoop"

	"ros-go-common/failure"
	"ros-go-common/requestcontext"
)

// RequestIDMiddleware always replaces an untrusted caller-supplied ID.
func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := requestcontext.GenerateRequestID()
		request, _ := requestcontext.New(id)
		r = r.Clone(requestcontext.WithContext(r.Context(), request))
		_ = id.InsertIntoHeaders(r.Header)
		_ = id.InsertIntoHeaders(w.Header())
		wrapped := httpsnoop.Wrap(w, httpsnoop.Hooks{
			WriteHeader: func(next httpsnoop.WriteHeaderFunc) httpsnoop.WriteHeaderFunc {
				return func(status int) { _ = id.InsertIntoHeaders(w.Header()); next(status) }
			},
			Write: func(next httpsnoop.WriteFunc) httpsnoop.WriteFunc {
				return func(body []byte) (int, error) { _ = id.InsertIntoHeaders(w.Header()); return next(body) }
			},
			Flush: func(next httpsnoop.FlushFunc) httpsnoop.FlushFunc {
				return func() { _ = id.InsertIntoHeaders(w.Header()); next() }
			},
			ReadFrom: func(next httpsnoop.ReadFromFunc) httpsnoop.ReadFromFunc {
				return func(reader io.Reader) (int64, error) {
					_ = id.InsertIntoHeaders(w.Header())
					return next(reader)
				}
			},
		})
		next.ServeHTTP(wrapped, r)
		_ = id.InsertIntoHeaders(w.Header())
	})
}

// RequestTimeoutMiddleware buffers responses using net/http.TimeoutHandler.
// It does not support streaming, Hijacker, or Flusher. Work must observe context
// cancellation; a timed-out Go handler cannot be forcibly terminated.
// Install RequestIDMiddleware outside this middleware for correlated problems.
func RequestTimeoutMiddleware(policy OperationalPolicy) (func(http.Handler) http.Handler, error) {
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			request, ok := requestcontext.FromContext(r.Context())
			if !ok {
				request, _ = requestcontext.New(requestcontext.GenerateRequestID())
			}
			deadline := request.StartedAt().Add(policy.requestTimeout)
			if existing, ok := request.Deadline(); ok && existing.Before(deadline) {
				deadline = existing
			}
			if existing, ok := r.Context().Deadline(); ok && existing.Before(deadline) {
				deadline = existing
			}
			request = request.WithDeadline(deadline)
			problem := NewProblem(failure.DeadlineExceeded).WithRequestID(request.RequestID())
			_ = request.RequestID().InsertIntoHeaders(w.Header())
			if !time.Now().Before(deadline) {
				_ = problem.Write(w)
				return
			}
			ctx, cancel := context.WithDeadline(requestcontext.WithContext(r.Context(), request), deadline)
			defer cancel()
			body, _ := json.Marshal(problem)
			writer := &deadlineWriter{ResponseWriter: w, ctx: ctx, deadline: deadline, timeoutBody: body}
			http.TimeoutHandler(next, time.Until(deadline), string(body)).ServeHTTP(writer, r.WithContext(ctx))
		})
	}, nil
}

// TimeoutHandler owns worker lifetime, panic forwarding and synchronized writes.
// This adapter emits the shared 504 problem and gives expiry priority even if
// TimeoutHandler selected a completed handler at the same time as cancellation.
// A canceled parent uses the same correlated response when it is still writable.
type deadlineWriter struct {
	http.ResponseWriter
	ctx             context.Context
	deadline        time.Time
	timeoutBody     []byte
	timedOut        bool
	timeoutWriteErr error
}

func (w *deadlineWriter) WriteHeader(status int) {
	w.timedOut = w.ctx.Err() != nil || !time.Now().Before(w.deadline)
	if w.timedOut {
		status = http.StatusGatewayTimeout
		w.Header().Set("Content-Type", "application/problem+json")
		w.Header().Del("Content-Length")
		w.Header().Del("Content-Encoding")
	}
	w.ResponseWriter.WriteHeader(status)
	if w.timedOut {
		_, w.timeoutWriteErr = w.ResponseWriter.Write(w.timeoutBody)
	}
}
func (w *deadlineWriter) Write(body []byte) (int, error) {
	if !w.timedOut {
		return w.ResponseWriter.Write(body)
	}
	return len(body), w.timeoutWriteErr
}
