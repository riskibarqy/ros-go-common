package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"ros-go-common/httpapi"
	"ros-go-common/requestcontext"
)

func TestRequestIDIngress(t *testing.T) {
	handler := httpapi.RequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request, ok := requestcontext.FromContext(r.Context())
		if !ok {
			t.Error("missing context")
			return
		}
		id := request.RequestID().String()
		if id == "caller-selected" || r.Header.Get(requestcontext.RequestIDHeader) != id {
			t.Error("trusted public input")
		}
		w.Header().Set(requestcontext.RequestIDHeader, "handler-overwrite")
		w.WriteHeader(400)
	}))
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set(requestcontext.RequestIDHeader, "caller-selected")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	id := w.Result().Header.Get(requestcontext.RequestIDHeader)
	if w.Code != 400 || id == "caller-selected" || id == "handler-overwrite" || len(id) != 36 {
		t.Fatal(w.Code, id)
	}
	if r.Header.Get(requestcontext.RequestIDHeader) != "caller-selected" {
		t.Fatal("mutated caller request")
	}
}
func TestRequestIDPreservesFlusher(t *testing.T) {
	handler := httpapi.RequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("lost Flusher")
			return
		}
		w.Header().Set(requestcontext.RequestIDHeader, "wrong")
		flusher.Flush()
	}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if !w.Flushed || w.Result().Header.Get(requestcontext.RequestIDHeader) == "wrong" {
		t.Fatal(w)
	}
}
func TestTimeoutResponseAndCancellation(t *testing.T) {
	wrap, err := httpapi.RequestTimeoutMiddleware(policy(t, 1024, 20*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	completed := make(chan error, 1)
	handler := httpapi.RequestIDMiddleware(wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request, ok := requestcontext.FromContext(r.Context())
		if !ok {
			t.Error("missing context")
		}
		deadline, hasDeadline := request.Deadline()
		ctxDeadline, ctxHasDeadline := r.Context().Deadline()
		if !hasDeadline || !ctxHasDeadline || !deadline.Equal(ctxDeadline) {
			t.Error("deadline not propagated")
		}
		<-r.Context().Done()
		<-release
		_, err := io.WriteString(w, "late private output")
		completed <- err
	})))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("POST", "/", nil))
	close(release)
	select {
	case err := <-completed:
		if !errors.Is(err, http.ErrHandlerTimeout) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("handler did not exit")
	}
	var body struct {
		Status    int    `json:"status"`
		RequestID string `json:"request_id"`
		Code      string `json:"code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err, w.Body.String())
	}
	if w.Code != 504 || body.Status != 504 || body.Code != "deadline_exceeded" || body.RequestID != w.Header().Get(requestcontext.RequestIDHeader) || w.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatal(w, body)
	}
}
func TestDeadlineAlreadyExpiredSkipsHandler(t *testing.T) {
	wrap, _ := httpapi.RequestTimeoutMiddleware(policy(t, 1024, time.Minute))
	called := false
	handler := wrap(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	for _, useParent := range []bool{false, true} {
		request, _ := requestcontext.New(requestcontext.GenerateRequestID())
		expired := time.Now().Add(-time.Second)
		ctx := context.Background()
		cancel := func() {}
		if useParent {
			ctx, cancel = context.WithDeadline(ctx, expired)
		} else {
			request = request.WithDeadline(expired)
		}
		r := httptest.NewRequest("GET", "/", nil).WithContext(requestcontext.WithContext(ctx, request))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		cancel()
		if called || w.Code != 504 {
			t.Fatal(called, w.Code)
		}
	}
}
func TestTimeoutPreservesEarlierDeadlineAndSuccess(t *testing.T) {
	wrap, _ := httpapi.RequestTimeoutMiddleware(policy(t, 1024, time.Minute))
	for _, useParent := range []bool{false, true} {
		request, _ := requestcontext.New(requestcontext.GenerateRequestID())
		earlier := time.Now().Add(30 * time.Second)
		ctx := context.Background()
		cancel := func() {}
		if useParent {
			ctx, cancel = context.WithDeadline(ctx, earlier)
		} else {
			request = request.WithDeadline(earlier)
		}
		handler := wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			deadline, ok := r.Context().Deadline()
			if !ok || !deadline.Equal(earlier) {
				t.Error("extended earlier deadline", deadline)
			}
			w.WriteHeader(503)
			_, _ = io.WriteString(w, "ordinary unavailable")
		}))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", "/", nil).WithContext(requestcontext.WithContext(ctx, request)))
		cancel()
		if w.Code != 503 || w.Body.String() != "ordinary unavailable" {
			t.Fatal(w)
		}
	}
}
func TestCanceledParentProducesConsistentProblem(t *testing.T) {
	wrap, _ := httpapi.RequestTimeoutMiddleware(policy(t, 1024, time.Second))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	handler := wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/", nil).WithContext(ctx))
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if w.Code != 504 || body["status"] != float64(w.Code) {
		t.Fatal(w, body)
	}
}
func TestMiddlewareConcurrentRequests(t *testing.T) {
	wrap, _ := httpapi.RequestTimeoutMiddleware(policy(t, 1024, time.Second))
	handler := httpapi.RequestIDMiddleware(wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "ok") })))
	ids := make(chan string, 20)
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
			if w.Code != 200 || w.Body.String() != "ok" {
				t.Error(w)
			}
			ids <- w.Header().Get(requestcontext.RequestIDHeader)
		}()
	}
	wg.Wait()
	close(ids)
	seen := map[string]bool{}
	for id := range ids {
		if id == "" || seen[id] {
			t.Fatal(id)
		}
		seen[id] = true
	}
}
