package httpapi

import (
	"errors"
	"time"
)

// OperationalPolicy has no universal default; each service must supply its limits.
// Its zero value is invalid and is rejected by request-processing entry points.
type OperationalPolicy struct {
	maxBodyBytes    int64
	requestTimeout  time.Duration
	defaultPageSize uint64
	maxPageSize     uint64
}

func NewOperationalPolicy(maxBodyBytes int64, requestTimeout time.Duration, defaultPageSize, maxPageSize uint64) (OperationalPolicy, error) {
	p := OperationalPolicy{maxBodyBytes, requestTimeout, defaultPageSize, maxPageSize}
	return p, p.Validate()
}
func (p OperationalPolicy) Validate() error {
	if p.maxBodyBytes <= 0 {
		return errors.New("maximum body bytes must be greater than zero")
	}
	if p.requestTimeout <= 0 {
		return errors.New("request timeout must be greater than zero")
	}
	if p.defaultPageSize == 0 || p.maxPageSize == 0 {
		return errors.New("page sizes must be greater than zero")
	}
	if p.defaultPageSize > p.maxPageSize {
		return errors.New("default page size must not exceed maximum page size")
	}
	return nil
}
func (p OperationalPolicy) MaxBodyBytes() int64           { return p.maxBodyBytes }
func (p OperationalPolicy) RequestTimeout() time.Duration { return p.requestTimeout }
func (p OperationalPolicy) DefaultPageSize() uint64       { return p.defaultPageSize }
func (p OperationalPolicy) MaxPageSize() uint64           { return p.maxPageSize }

type PaginationRequest struct {
	Limit  *uint64 `json:"limit,omitempty"`
	Cursor *string `json:"cursor,omitempty"`
}
type Pagination struct {
	limit  uint64
	cursor *string
}

func (p Pagination) Limit() uint64 { return p.limit }
func (p Pagination) Cursor() (string, bool) {
	if p.cursor == nil {
		return "", false
	}
	return *p.cursor, true
}
func (p PaginationRequest) Resolve(policy OperationalPolicy) (Pagination, error) {
	if err := policy.Validate(); err != nil {
		return Pagination{}, err
	}
	limit := policy.defaultPageSize
	if p.Limit != nil {
		limit = *p.Limit
	}
	fields := []FieldError{}
	if limit == 0 || limit > policy.maxPageSize {
		fields = append(fields, FieldError{Field: "/limit", Code: "invalid_page_limit"})
	}
	if p.Cursor != nil && *p.Cursor == "" {
		fields = append(fields, FieldError{Field: "/cursor", Code: "empty_cursor"})
	}
	if len(fields) > 0 {
		return Pagination{}, &RequestError{Kind: Validation, Fields: fields}
	}
	var cursor *string
	if p.Cursor != nil {
		value := *p.Cursor
		cursor = &value
	}
	return Pagination{limit, cursor}, nil
}
