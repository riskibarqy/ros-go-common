package httpapi_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/riskibarqy/ros-go-common/failure"
	"github.com/riskibarqy/ros-go-common/httpapi"
	"github.com/riskibarqy/ros-go-common/requestcontext"
)

func policy(t *testing.T, bytes int64, timeout time.Duration) httpapi.OperationalPolicy {
	t.Helper()
	p, err := httpapi.NewOperationalPolicy(bytes, timeout, 25, 100)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func jsonHeaders() http.Header { return http.Header{"Content-Type": []string{"application/json"}} }
func requireRequestError(t *testing.T, err error, kind httpapi.RequestErrorKind) *httpapi.RequestError {
	t.Helper()
	var rejection *httpapi.RequestError
	if !errors.As(err, &rejection) || rejection.Kind != kind {
		t.Fatalf("got %v, want rejection %d", err, kind)
	}
	return rejection
}
func TestResponseContracts(t *testing.T) {
	id := requestcontext.GenerateRequestID()
	cursor := "opaque"
	response := httpapi.APIResponse[map[string]int]{Data: map[string]int{"id": 123}}
	w := httptest.NewRecorder()
	if err := response.Write(w, 200); err != nil {
		t.Fatal(err)
	}
	if w.Body.String() != `{"data":{"id":123}}` || w.Header().Get("Content-Type") != "application/json" {
		t.Fatal(w.Body.String(), w.Header())
	}
	response.Meta = &httpapi.ResponseMeta{RequestID: &id, Pagination: &httpapi.PaginationMeta{Limit: 25, NextCursor: &cursor}}
	encoded, err := json.Marshal(response)
	if err != nil || !strings.Contains(string(encoded), `"next_cursor":"opaque"`) || !strings.Contains(string(encoded), id.String()) {
		t.Fatal(string(encoded), err)
	}
	w = httptest.NewRecorder()
	w.Header().Set("Content-Type", "old/type")
	w.Header().Set("Content-Length", "100")
	if err := (httpapi.APIResponse[chan int]{Data: make(chan int)}).Write(w, 204); err != nil {
		t.Fatal(err)
	}
	if w.Code != 204 || w.Body.Len() != 0 || w.Header().Get("Content-Type") != "" || w.Header().Get("Content-Length") != "" {
		t.Fatal(w)
	}
	w = httptest.NewRecorder()
	if err := (httpapi.APIResponse[chan int]{Data: make(chan int)}).Write(w, 200); err == nil || w.Body.Len() != 0 || w.Header().Get("Content-Type") != "" {
		t.Fatal("serialization committed headers", err, w)
	}
}
func TestProblems(t *testing.T) {
	for kind := failure.Internal; kind <= failure.Unavailable; kind++ {
		p := httpapi.NewProblem(kind)
		w := httptest.NewRecorder()
		if err := p.Write(w); err != nil {
			t.Fatal(err)
		}
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		status, code := kind.Mapping()
		if w.Code != status || body["status"] != float64(status) || body["code"] != string(code) || body["type"] != "urn:ros:problem:"+string(code) || w.Header().Get("Content-Type") != "application/problem+json" {
			t.Fatal(w, body)
		}
		for _, field := range []string{"detail", "instance", "request_id", "errors"} {
			if _, ok := body[field]; ok {
				t.Fatal("unexpected optional field", field)
			}
		}
	}
	id := requestcontext.GenerateRequestID()
	p := httpapi.NewProblem(failure.Validation).WithDetail("Safe detail").WithInstance("/problems/1").WithRequestID(id).WithErrors(nil)
	body, err := json.Marshal(p)
	if err != nil || !strings.Contains(string(body), `"errors":[]`) {
		t.Fatal(string(body), err)
	}
	w := httptest.NewRecorder()
	if err := (httpapi.ProblemDetails{}).Write(w); err != nil || w.Code != 500 {
		t.Fatal(w, err)
	}
}
func TestPolicyAndPagination(t *testing.T) {
	for _, tc := range []struct {
		bytes                int64
		timeout              time.Duration
		defaultSize, maxSize uint64
	}{
		{0, time.Second, 25, 100}, {-1, time.Second, 25, 100}, {1, 0, 25, 100}, {1, -1, 25, 100}, {1, time.Second, 0, 100}, {1, time.Second, 25, 0}, {1, time.Second, 101, 100},
	} {
		if _, err := httpapi.NewOperationalPolicy(tc.bytes, tc.timeout, tc.defaultSize, tc.maxSize); err == nil {
			t.Fatal("accepted policy", tc)
		}
	}
	p := policy(t, 1024, time.Second)
	page, err := (httpapi.PaginationRequest{}).Resolve(p)
	if err != nil || page.Limit() != 25 {
		t.Fatal(page, err)
	}
	if _, ok := page.Cursor(); ok {
		t.Fatal("invented cursor")
	}
	limit := uint64(100)
	cursor := " untouched opaque cursor "
	page, err = (httpapi.PaginationRequest{Limit: &limit, Cursor: &cursor}).Resolve(p)
	if err != nil || page.Limit() != 100 {
		t.Fatal(page, err)
	}
	original := cursor
	cursor = "changed"
	if got, _ := page.Cursor(); got != original {
		t.Fatal("cursor mutated", got)
	}
	for _, limit := range []uint64{0, 101} {
		empty := ""
		_, err := (httpapi.PaginationRequest{Limit: &limit, Cursor: &empty}).Resolve(p)
		rejection := requireRequestError(t, err, httpapi.Validation)
		if len(rejection.Fields) != 2 || rejection.Fields[0].Code != "invalid_page_limit" || rejection.Fields[1].Code != "empty_cursor" {
			t.Fatal(rejection)
		}
	}
	if _, err := (httpapi.PaginationRequest{}).Resolve(httpapi.OperationalPolicy{}); err == nil {
		t.Fatal("accepted zero policy")
	}
}
func TestParseJSON(t *testing.T) {
	type input struct {
		Name string `json:"name"`
	}
	p := policy(t, 1024, time.Second)
	for _, tc := range []struct {
		body  string
		kind  httpapi.RequestErrorKind
		valid bool
	}{
		{`{"name":"service"}`, 0, true},
		{`{"name":"service","extra":true}`, 0, true},
		{``, httpapi.MalformedJSON, false}, {`{"name":`, httpapi.MalformedJSON, false},
		{`{"name":3,`, httpapi.MalformedJSON, false}, {`{"name":3}`, httpapi.InvalidJSONValue, false},
		{`null`, httpapi.InvalidJSONValue, false}, {`[]`, httpapi.InvalidJSONValue, false}, {`42`, httpapi.InvalidJSONValue, false},
		{`{} {}`, httpapi.MalformedJSON, false}, {`{"name":"one","name":"two"}`, httpapi.InvalidJSONValue, false},
		{`{"nested":{"x":1,"x":2}}`, httpapi.InvalidJSONValue, false}, {"{\"name\":\"\xff\"}", httpapi.MalformedJSON, false},
	} {
		t.Run(tc.body, func(t *testing.T) {
			value, err := httpapi.ParseJSON[input](jsonHeaders(), []byte(tc.body), p, httpapi.DecodeOptions{})
			if tc.valid {
				if err != nil || value.Name != "service" {
					t.Fatal(value, err)
				}
			} else {
				requireRequestError(t, err, tc.kind)
			}
		})
	}
	_, err := httpapi.ParseJSON[input](jsonHeaders(), []byte(`{"name":"a","extra":true}`), p, httpapi.DecodeOptions{DisallowUnknownFields: true})
	requireRequestError(t, err, httpapi.InvalidJSONValue)
	value, err := httpapi.ValidateRequest(input{}, func(v input) []httpapi.FieldError {
		if v.Name == "" {
			return []httpapi.FieldError{{Field: "/name", Code: "required"}}
		}
		return nil
	})
	requireRequestError(t, err, httpapi.Validation)
	if value.Name != "" {
		t.Fatal(value)
	}
	value, err = httpapi.ValidateRequest(input{Name: "ok"}, func(input) []httpapi.FieldError { return nil })
	if err != nil || value.Name != "ok" {
		t.Fatal(value, err)
	}
}
func TestContentType(t *testing.T) {
	p := policy(t, 1024, time.Second)
	for _, tc := range []struct {
		value string
		valid bool
	}{
		{"application/json", true}, {"Application/JSON", true}, {"application/problem+json", true},
		{`application/json; charset="UTF-8"`, true}, {`application/json; profile="a;b"; charset=utf-8`, true},
		{"", false}, {"text/json", false}, {"application/*", false}, {"application/*+json", false}, {"*/*", false},
		{"application/json; charset=latin1", false}, {"application/json; charset=utf-8; charset=utf-8", false},
		{"application/json, application/json", false}, {"application/json; broken", false},
	} {
		t.Run(tc.value, func(t *testing.T) {
			headers := http.Header{"Content-Type": []string{tc.value}}
			_, err := httpapi.ParseJSON[map[string]any](headers, []byte(`{}`), p, httpapi.DecodeOptions{})
			if tc.valid {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				requireRequestError(t, err, httpapi.UnsupportedMediaType)
			}
		})
	}
	for _, headers := range []http.Header{
		{}, {"Content-Type": []string{"application/json", "application/json"}},
		{"Content-Type": []string{"application/json"}, "Content-Encoding": []string{"gzip"}},
		{"Content-Type": []string{"application/json"}, "Content-Encoding": []string{"identity", "identity"}},
	} {
		_, err := httpapi.ParseJSON[map[string]any](headers, []byte(`{}`), p, httpapi.DecodeOptions{})
		requireRequestError(t, err, httpapi.UnsupportedMediaType)
	}
	headers := jsonHeaders()
	headers.Set("Content-Encoding", "IDENTITY")
	if _, err := httpapi.ParseJSON[map[string]any](headers, []byte(`{}`), p, httpapi.DecodeOptions{}); err != nil {
		t.Fatal(err)
	}
}

type failingReader struct{ reads int }

func (r *failingReader) Read([]byte) (int, error) {
	r.reads++
	return 0, errors.New("secret transport diagnostic")
}
func TestBoundedReadingAndSafeErrors(t *testing.T) {
	p := policy(t, 2, time.Second)
	headers := jsonHeaders()
	headers.Set("Content-Length", "0")
	value, err := httpapi.ReadJSON[map[string]any](headers, strings.NewReader(`{}`), p, httpapi.DecodeOptions{})
	if err != nil || value == nil {
		t.Fatal(value, err)
	}
	_, err = httpapi.ReadJSON[map[string]any](headers, io.MultiReader(strings.NewReader("{"), strings.NewReader(" }")), p, httpapi.DecodeOptions{})
	requireRequestError(t, err, httpapi.PayloadTooLarge)
	_, err = httpapi.ParseJSON[map[string]any](nil, []byte("abc"), p, httpapi.DecodeOptions{})
	requireRequestError(t, err, httpapi.PayloadTooLarge)
	reader := &failingReader{}
	_, err = httpapi.ReadJSON[map[string]any](nil, reader, p, httpapi.DecodeOptions{})
	requireRequestError(t, err, httpapi.UnsupportedMediaType)
	if reader.reads != 0 {
		t.Fatal("read unsupported body")
	}
	_, err = httpapi.ReadJSON[map[string]any](headers, reader, p, httpapi.DecodeOptions{})
	rejection := requireRequestError(t, err, httpapi.BodyReadFailed)
	w := httptest.NewRecorder()
	if err := rejection.IntoProblem().Write(w); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(w.Body.String(), "secret") || strings.Contains(rejection.Error(), "secret") {
		t.Fatal("leaked diagnostic")
	}
	_, err = httpapi.ReadJSON[map[string]any](headers, nil, p, httpapi.DecodeOptions{})
	requireRequestError(t, err, httpapi.MalformedJSON)
	if _, err := httpapi.ParseJSON[map[string]any](headers, []byte(`{}`), httpapi.OperationalPolicy{}, httpapi.DecodeOptions{}); err == nil {
		t.Fatal("accepted zero policy")
	}
	for _, tc := range []struct {
		kind   httpapi.RequestErrorKind
		status int
	}{
		{httpapi.UnsupportedMediaType, 415}, {httpapi.PayloadTooLarge, 413}, {httpapi.MalformedJSON, 400},
		{httpapi.InvalidJSONValue, 422}, {httpapi.Validation, 422}, {httpapi.BodyReadFailed, 400},
	} {
		p := (&httpapi.RequestError{Kind: tc.kind}).IntoProblem()
		if p.Status() != tc.status {
			t.Fatal(p)
		}
	}
}
