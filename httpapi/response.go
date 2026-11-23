// Package httpapi provides JSON contracts, bounded decoding, and net/http middleware.
package httpapi

import (
	"encoding/json"
	"net/http"

	"ros-go-common/failure"
	"ros-go-common/requestcontext"
)

type APIResponse[T any] struct {
	Data T             `json:"data"`
	Meta *ResponseMeta `json:"meta,omitempty"`
}
type ResponseMeta struct {
	RequestID  *requestcontext.RequestID `json:"request_id,omitempty"`
	Pagination *PaginationMeta           `json:"pagination,omitempty"`
}
type PaginationMeta struct {
	Limit      uint64  `json:"limit"`
	NextCursor *string `json:"next_cursor,omitempty"`
}

// Write marshals before committing headers; a 204 never serializes data.
func (response APIResponse[T]) Write(w http.ResponseWriter, status int) error {
	if status == http.StatusNoContent {
		w.Header().Del("Content-Type")
		w.Header().Del("Content-Length")
		w.WriteHeader(status)
		return nil
	}
	body, err := json.Marshal(response)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, err = w.Write(body)
	return err
}

type FieldError struct {
	Field  string  `json:"field"`
	Code   string  `json:"code"`
	Detail *string `json:"detail,omitempty"`
}

// ProblemDetails keeps its HTTP and JSON status bound to the same private value.
// The zero value is a safe Internal problem. Details must be client-safe.
type ProblemDetails struct {
	TypeURI   string `json:"type"`
	Title     string `json:"title"`
	status    int
	Detail    *string                   `json:"detail,omitempty"`
	Instance  *string                   `json:"instance,omitempty"`
	Code      failure.ErrorCode         `json:"code,omitempty"`
	RequestID *requestcontext.RequestID `json:"request_id,omitempty"`
	Errors    *[]FieldError             `json:"errors,omitempty"`
}

func NewProblem(kind failure.Kind) ProblemDetails {
	status, code := kind.Mapping()
	return ProblemDetails{TypeURI: "urn:ros:problem:" + string(code), Title: kind.Title(), status: status, Code: code}
}
func (p ProblemDetails) Status() int {
	if p.status == 0 {
		return 500
	}
	return p.status
}
func (p ProblemDetails) MarshalJSON() ([]byte, error) {
	if p.status == 0 {
		p = NewProblem(failure.Internal)
	}
	type fields ProblemDetails
	return json.Marshal(struct {
		fields
		Status int `json:"status"`
	}{fields(p), p.Status()})
}
func (p ProblemDetails) WithDetail(detail string) ProblemDetails { p.Detail = &detail; return p }
func (p ProblemDetails) WithInstance(instance string) ProblemDetails {
	p.Instance = &instance
	return p
}
func (p ProblemDetails) WithRequestID(id requestcontext.RequestID) ProblemDetails {
	p.RequestID = &id
	return p
}
func (p ProblemDetails) WithErrors(fields []FieldError) ProblemDetails {
	if fields == nil {
		fields = []FieldError{}
	}
	p.Errors = &fields
	return p
}
func (p ProblemDetails) Write(w http.ResponseWriter) error {
	body, err := json.Marshal(p)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.Status())
	_, err = w.Write(body)
	return err
}
