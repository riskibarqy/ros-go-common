// Package failure defines transport-neutral failure categories and stable public codes.
package failure

// Kind is a transport-neutral category. Its zero value maps to Internal.
type Kind uint8

const (
	Internal Kind = iota
	BadRequest
	Unauthorized
	Forbidden
	NotFound
	Conflict
	Validation
	TooManyRequests
	PayloadTooLarge
	UnsupportedMediaType
	DeadlineExceeded
	MethodNotAllowed
	NotImplemented
	Unavailable
)

// ErrorCode is the stable snake_case wire classification.
type ErrorCode string

const (
	CodeInvalidRequest       ErrorCode = "invalid_request"
	CodeUnauthenticated      ErrorCode = "unauthenticated"
	CodeForbidden            ErrorCode = "forbidden"
	CodeNotFound             ErrorCode = "not_found"
	CodeConflict             ErrorCode = "conflict"
	CodeValidationFailed     ErrorCode = "validation_failed"
	CodeRateLimited          ErrorCode = "rate_limited"
	CodePayloadTooLarge      ErrorCode = "payload_too_large"
	CodeUnsupportedMediaType ErrorCode = "unsupported_media_type"
	CodeDeadlineExceeded     ErrorCode = "deadline_exceeded"
	CodeMethodNotAllowed     ErrorCode = "method_not_allowed"
	CodeNotImplemented       ErrorCode = "not_implemented"
	CodeUnavailable          ErrorCode = "unavailable"
	CodeInternal             ErrorCode = "internal"
)

func (k Kind) Mapping() (int, ErrorCode) {
	switch k {
	case BadRequest:
		return 400, CodeInvalidRequest
	case Unauthorized:
		return 401, CodeUnauthenticated
	case Forbidden:
		return 403, CodeForbidden
	case NotFound:
		return 404, CodeNotFound
	case Conflict:
		return 409, CodeConflict
	case Validation:
		return 422, CodeValidationFailed
	case TooManyRequests:
		return 429, CodeRateLimited
	case PayloadTooLarge:
		return 413, CodePayloadTooLarge
	case UnsupportedMediaType:
		return 415, CodeUnsupportedMediaType
	case DeadlineExceeded:
		return 504, CodeDeadlineExceeded
	case MethodNotAllowed:
		return 405, CodeMethodNotAllowed
	case NotImplemented:
		return 501, CodeNotImplemented
	case Unavailable:
		return 503, CodeUnavailable
	default:
		return 500, CodeInternal
	}
}
func (k Kind) Title() string {
	switch k {
	case BadRequest:
		return "Bad Request"
	case Unauthorized:
		return "Unauthorized"
	case Forbidden:
		return "Forbidden"
	case NotFound:
		return "Not Found"
	case Conflict:
		return "Conflict"
	case Validation:
		return "Validation Failed"
	case TooManyRequests:
		return "Too Many Requests"
	case PayloadTooLarge:
		return "Payload Too Large"
	case UnsupportedMediaType:
		return "Unsupported Media Type"
	case DeadlineExceeded:
		return "Gateway Timeout"
	case MethodNotAllowed:
		return "Method Not Allowed"
	case NotImplemented:
		return "Not Implemented"
	case Unavailable:
		return "Service Unavailable"
	default:
		return "Internal Server Error"
	}
}
