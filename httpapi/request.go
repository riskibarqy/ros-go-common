package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"unicode/utf8"

	"ros-go-common/failure"
)

type RequestErrorKind uint8

const (
	UnsupportedMediaType RequestErrorKind = iota
	PayloadTooLarge
	MalformedJSON
	InvalidJSONValue
	Validation
	BodyReadFailed
)

// RequestError never retains parser diagnostics or raw input.
type RequestError struct {
	Kind   RequestErrorKind
	Fields []FieldError
}

func (e *RequestError) Error() string {
	switch e.Kind {
	case UnsupportedMediaType:
		return "unsupported JSON media type"
	case PayloadTooLarge:
		return "request body too large"
	case MalformedJSON:
		return "malformed JSON"
	case InvalidJSONValue:
		return "invalid request schema"
	case Validation:
		return "request validation failed"
	default:
		return "request body read failed"
	}
}
func (e *RequestError) IntoProblem() ProblemDetails {
	var kind failure.Kind
	var detail string
	switch e.Kind {
	case UnsupportedMediaType:
		kind = failure.UnsupportedMediaType
		detail = "The request must use a UTF-8 JSON content type."
	case PayloadTooLarge:
		kind = failure.PayloadTooLarge
		detail = "The request body exceeds the configured size limit."
	case MalformedJSON:
		kind = failure.BadRequest
		detail = "The request body is not valid JSON."
	case InvalidJSONValue:
		kind = failure.Validation
		detail = "The JSON object does not match the request schema."
	case Validation:
		return NewProblem(failure.Validation).WithDetail("The request contains invalid fields.").WithErrors(e.Fields)
	default:
		kind = failure.BadRequest
		detail = "The request body could not be read."
	}
	return NewProblem(kind).WithDetail(detail)
}

// DecodeOptions makes unknown-field policy explicit for endpoint-owned DTOs.
// Go does not enforce required fields: use ValidateRequest after decoding.
type DecodeOptions struct{ DisallowUnknownFields bool }

func ParseJSON[T any](headers http.Header, body []byte, policy OperationalPolicy, options DecodeOptions) (T, error) {
	var zero T
	if err := policy.Validate(); err != nil {
		return zero, err
	}
	if int64(len(body)) > policy.maxBodyBytes {
		return zero, &RequestError{Kind: PayloadTooLarge}
	}
	if err := checkContentType(headers); err != nil {
		return zero, err
	}
	return decodeJSON[T](body, options)
}
func ReadJSON[T any](headers http.Header, body io.Reader, policy OperationalPolicy, options DecodeOptions) (T, error) {
	var zero T
	if err := policy.Validate(); err != nil {
		return zero, err
	}
	if err := checkContentType(headers); err != nil {
		return zero, err
	}
	if body == nil {
		return zero, &RequestError{Kind: MalformedJSON}
	}
	data, err := io.ReadAll(http.MaxBytesReader(nil, io.NopCloser(body), policy.maxBodyBytes))
	if err != nil {
		var oversized *http.MaxBytesError
		if errors.As(err, &oversized) {
			return zero, &RequestError{Kind: PayloadTooLarge}
		}
		return zero, &RequestError{Kind: BodyReadFailed}
	}
	return decodeJSON[T](data, options)
}
func ValidateRequest[T any](value T, validate func(T) []FieldError) (T, error) {
	if fields := validate(value); len(fields) > 0 {
		var zero T
		return zero, &RequestError{Kind: Validation, Fields: fields}
	}
	return value, nil
}
func checkContentType(headers http.Header) error {
	invalid := &RequestError{Kind: UnsupportedMediaType}
	encodings := headers.Values("Content-Encoding")
	if len(encodings) > 1 || len(encodings) == 1 && !strings.EqualFold(encodings[0], "identity") {
		return invalid
	}
	values := headers.Values("Content-Type")
	if len(values) != 1 {
		return invalid
	}
	media, params, err := mime.ParseMediaType(values[0])
	if err != nil {
		return invalid
	}
	subtype, ok := strings.CutPrefix(media, "application/")
	if !ok || strings.Contains(subtype, "*") || !(subtype == "json" || strings.HasSuffix(subtype, "+json")) {
		return invalid
	}
	if charset, ok := params["charset"]; ok && !strings.EqualFold(charset, "utf-8") {
		return invalid
	}
	// mime.ParseMediaType permits repeated identical parameters; the contract does not.
	charsetCount := 0
	quoted, escaped := false, false
	start := 0
	for i := 0; i <= len(values[0]); i++ {
		if i < len(values[0]) {
			ch := values[0][i]
			if escaped {
				escaped = false
				continue
			}
			if quoted && ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				quoted = !quoted
			}
			if ch != ';' || quoted {
				continue
			}
		}
		if start > 0 {
			name, _, _ := strings.Cut(values[0][start:i], "=")
			if strings.EqualFold(strings.TrimSpace(name), "charset") {
				charsetCount++
			}
		}
		start = i + 1
	}
	if charsetCount > 1 {
		return invalid
	}
	return nil
}
func decodeJSON[T any](body []byte, options DecodeOptions) (T, error) {
	var value T
	if !utf8.Valid(body) || !json.Valid(body) {
		return value, &RequestError{Kind: MalformedJSON}
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return value, &RequestError{Kind: InvalidJSONValue}
	}
	// encoding/json otherwise silently accepts duplicate keys. Reject them before
	// decoding, including nested maps, to avoid ambiguous security-sensitive input.
	if err := checkJSONKeys(json.NewDecoder(bytes.NewReader(body))); err != nil {
		return value, &RequestError{Kind: InvalidJSONValue}
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	if options.DisallowUnknownFields {
		decoder.DisallowUnknownFields()
	}
	if err := decoder.Decode(&value); err != nil {
		var zero T
		return zero, &RequestError{Kind: InvalidJSONValue}
	}
	return value, nil
}
func checkJSONKeys(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	if delim == '{' {
		seen := make(map[string]bool)
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name := key.(string)
			if seen[name] {
				return errors.New("duplicate JSON key")
			}
			seen[name] = true
			if err := checkJSONKeys(decoder); err != nil {
				return err
			}
		}
	} else if delim == '[' {
		for decoder.More() {
			if err := checkJSONKeys(decoder); err != nil {
				return err
			}
		}
	}
	_, err = decoder.Token()
	return err
}
