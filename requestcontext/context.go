// Package requestcontext owns validated diagnostic IDs and request timing.
package requestcontext

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

const RequestIDHeader = "X-Request-ID"

var ErrInvalidRequestID = errors.New("request ID must be 1-128 ASCII letters, digits, '-', '_' or '.'")

// RequestID cannot contain an unchecked external value. Its zero value is invalid.
type RequestID struct{ value string }

func ParseRequestID(value string) (RequestID, error) {
	if len(value) == 0 || len(value) > 128 {
		return RequestID{}, ErrInvalidRequestID
	}
	for i := 0; i < len(value); i++ {
		b := value[i]
		if !(b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '-' || b == '_' || b == '.') {
			return RequestID{}, ErrInvalidRequestID
		}
	}
	return RequestID{value}, nil
}
func GenerateRequestID() RequestID {
	var id [16]byte
	// crypto/rand.Read terminates the process on an unrecoverable RNG failure in Go 1.25.
	_, _ = rand.Read(id[:])
	id[6] = id[6]&0x0f | 0x40
	id[8] = id[8]&0x3f | 0x80
	return RequestID{fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])}
}
func (id RequestID) String() string { return id.value }
func (id RequestID) Valid() bool    { return id.value != "" }
func (id RequestID) MarshalJSON() ([]byte, error) {
	if !id.Valid() {
		return nil, ErrInvalidRequestID
	}
	return json.Marshal(id.value)
}
func (id RequestID) InsertIntoHeaders(headers http.Header) error {
	if !id.Valid() {
		return ErrInvalidRequestID
	}
	headers.Set(RequestIDHeader, id.value)
	return nil
}

// RequestContext is immutable by convention; timing methods return a copy.
type RequestContext struct {
	id        RequestID
	startedAt time.Time
	deadline  time.Time
}

func New(id RequestID) (RequestContext, error) {
	if !id.Valid() {
		return RequestContext{}, ErrInvalidRequestID
	}
	return RequestContext{id: id, startedAt: time.Now()}, nil
}
func (c RequestContext) RequestID() RequestID        { return c.id }
func (c RequestContext) StartedAt() time.Time        { return c.startedAt }
func (c RequestContext) Deadline() (time.Time, bool) { return c.deadline, !c.deadline.IsZero() }
func (c RequestContext) Remaining() (time.Duration, bool) {
	if c.deadline.IsZero() {
		return 0, false
	}
	return max(time.Until(c.deadline), 0), true
}
func (c RequestContext) WithTimeout(timeout time.Duration) (RequestContext, error) {
	if timeout < 0 {
		return RequestContext{}, errors.New("request timeout must not be negative")
	}
	c.deadline = c.startedAt.Add(timeout)
	return c, nil
}
func (c RequestContext) WithDeadline(deadline time.Time) RequestContext {
	c.deadline = deadline
	return c
}

type contextKey struct{}

func WithContext(ctx context.Context, request RequestContext) context.Context {
	return context.WithValue(ctx, contextKey{}, request)
}
func FromContext(ctx context.Context) (RequestContext, bool) {
	request, ok := ctx.Value(contextKey{}).(RequestContext)
	return request, ok && request.id.Valid() && !request.startedAt.IsZero()
}
