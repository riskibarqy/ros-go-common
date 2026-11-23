package failure_test

import (
	"testing"

	"ros-go-common/failure"
)

func TestMappings(t *testing.T) {
	for _, tc := range []struct {
		kind   failure.Kind
		status int
		code   failure.ErrorCode
		title  string
	}{
		{failure.BadRequest, 400, failure.CodeInvalidRequest, "Bad Request"},
		{failure.Unauthorized, 401, failure.CodeUnauthenticated, "Unauthorized"},
		{failure.Forbidden, 403, failure.CodeForbidden, "Forbidden"},
		{failure.NotFound, 404, failure.CodeNotFound, "Not Found"},
		{failure.Conflict, 409, failure.CodeConflict, "Conflict"},
		{failure.Validation, 422, failure.CodeValidationFailed, "Validation Failed"},
		{failure.TooManyRequests, 429, failure.CodeRateLimited, "Too Many Requests"},
		{failure.PayloadTooLarge, 413, failure.CodePayloadTooLarge, "Payload Too Large"},
		{failure.UnsupportedMediaType, 415, failure.CodeUnsupportedMediaType, "Unsupported Media Type"},
		{failure.DeadlineExceeded, 504, failure.CodeDeadlineExceeded, "Gateway Timeout"},
		{failure.MethodNotAllowed, 405, failure.CodeMethodNotAllowed, "Method Not Allowed"},
		{failure.NotImplemented, 501, failure.CodeNotImplemented, "Not Implemented"},
		{failure.Unavailable, 503, failure.CodeUnavailable, "Service Unavailable"},
		{failure.Internal, 500, failure.CodeInternal, "Internal Server Error"},
		{failure.Kind(255), 500, failure.CodeInternal, "Internal Server Error"},
	} {
		status, code := tc.kind.Mapping()
		if status != tc.status || code != tc.code || tc.kind.Title() != tc.title {
			t.Fatal(tc, status, code, tc.kind.Title())
		}
	}
}
