# ros-go-common

Go port of the `ros-common` Rust workspace: explicit configuration parsing,
request context, shared failure classifications, HTTP contracts, gRPC adapters,
and OpenTelemetry observability. Requires Go 1.25 or later.

This is a library, not an application server. Importing a package does not read
configuration values, start a server, or install telemetry providers.

## Packages

| Rust crate | Go package | Responsibility |
| --- | --- | --- |
| `ros-common::config` | `github.com/riskibarqy/ros-go-common/config` | Pure parsing of caller-supplied configuration |
| `ros-context` | `github.com/riskibarqy/ros-go-common/requestcontext` | Validated request IDs and monotonic request timing |
| `ros-error` | `github.com/riskibarqy/ros-go-common/failure` | Stable public codes and transport-neutral failures |
| `ros-http` | `github.com/riskibarqy/ros-go-common/httpapi` | JSON responses, Problem Details, decoding, pagination, middleware |
| `ros-grpc` | `github.com/riskibarqy/ros-go-common/grpcapi` | grpc-go status mapping, metadata, deadlines, HTTP gateway classification |
| `ros-observability` | `github.com/riskibarqy/ros-go-common/observability` | W3C propagation, safe completion logs, OTLP traces and logs |

`config`, `failure`, and `requestcontext` use only the standard library. HTTP
middleware additionally uses `httpsnoop` to preserve the optional capabilities
of `http.ResponseWriter`, such as `http.Flusher`. The HTTP package does not import
gRPC or OpenTelemetry. Those integrations use the official grpc-go and
OpenTelemetry Go packages and are isolated in their respective packages.

The module path is `github.com/riskibarqy/ros-go-common`. Once the first tag
has been published to the public repository, install it in a consumer with:

```sh
go get github.com/riskibarqy/ros-go-common@latest
```

For example, import `github.com/riskibarqy/ros-go-common/config`. During local
development a consumer can override the published module:

```go
replace github.com/riskibarqy/ros-go-common => ../ros-go-common
```

## Automatic releases

A push to `main` runs formatting checks, race-enabled tests, and `go vet`.
Only after those checks pass does GitHub Actions publish a new lightweight
SemVer tag: `v0.1.0` for the first release, then `v0.1.1`, `v0.1.2`, and so on.
Pull requests and pushes to other branches never publish tags.

The release script refetches remote tags and retries tag-creation conflicts
without force-pushing. Rerunning an already released commit does nothing. If a
newer commit has already been released, an older build is skipped instead of
publishing old code as the latest version. Divergent release history fails
rather than guessing. The workflow currently supports the unsuffixed Go module's
`v0` and `v1` release lines; minor and major releases remain explicit decisions.
Do not merge a breaking change assuming an automatic patch tag makes it safe.

The release job uses the built-in `GITHUB_TOKEN` with `contents: write`; no
personal access token is needed. Repository/organization policy and tag rules
must permit that token to create `v*` tags, and GitHub Actions must be enabled.
Tag creation is sufficient for Go installation; this does not create a GitHub
Release page. A tag pushed by `GITHUB_TOKEN` does not trigger other tag-push
workflows, so future release steps should run in this same workflow.

Consumers still opt in to upgrades:

```sh
go get github.com/riskibarqy/ros-go-common@latest
go mod tidy
go test ./...
```

Commit the consumer's updated `go.mod` and `go.sum`. Publishing a tag does not
silently change applications already using an older version.

## Configuration

The caller owns environment access, defaults, secret sources, and production
policy. Pass a `map[string]string` explicitly:

```go
values := map[string]string{
    "SERVICE_NAME": " inventory ",
    "REQUEST_TIMEOUT": "250ms",
}
name := config.GetString(values, "SERVICE_NAME", "my-service")
timeout, err := config.GetDuration(values, "REQUEST_TIMEOUT", 10*time.Second)
if err != nil {
    return err
}
_, _ = name, timeout
```

- `GetOptionalString` returns `(value, present)` after trimming whitespace.
- `GetRequiredString` rejects missing or blank values.
- Scalar helpers use the supplied fallback for missing or blank values.
- `GetBool` accepts only lowercase `true` and `false`, not Go's other boolean spellings.
- `GetU64` accepts decimal unsigned 64-bit integers, including a leading `+`.
- `GetDurationSeconds` accepts unsigned integer seconds; `GetDuration` requires
  an integer followed by exactly `ms`, `s`, `m`, or `h`.
- Conversion overflow is an error. Unlike Rust's `Duration`, Go's signed
  nanosecond `time.Duration` is limited to approximately 290 years.

`ConfigError` identifies the key and reason without retaining the rejected
value. Configuration keys themselves must not contain secrets.

## HTTP responses and errors

`APIResponse[T]` serializes as `{"data":...}` with an optional typed `meta`.
`ResponseMeta` supports `request_id` and cursor pagination metadata. Pointer
fields distinguish absence from an explicitly supplied value.

```go
response := httpapi.APIResponse[map[string]string]{
    Data: map[string]string{"id": "123"},
}
if err := response.Write(w, http.StatusOK); err != nil {
    // Handle serialization or response-write errors in the server adapter.
}
```

Writing status 204 skips serialization and produces no body, content type, or
content length, even if the payload could not otherwise be serialized.
Other responses serialize before committing headers.

```go
problem := httpapi.NewProblem(failure.NotFound)
if request, ok := requestcontext.FromContext(r.Context()); ok {
    problem = problem.WithRequestID(request.RequestID())
}
_ = problem.Write(w)
```

Problems use RFC 9457, `application/problem+json`, and
`urn:ros:problem:<stable_code>`. The private status field owns both HTTP status
and JSON `status`. The zero problem safely becomes an Internal problem.
`WithDetail`, `WithInstance`, and `WithErrors` accept application-owned,
client-safe information. Never pass internal error strings or credentials to
these fields. Field errors retain the `field`, `code`, and optional `detail`
wire format; use stable field references such as `/profile/email`.

Failure mappings retain the Rust contract: malformed requests are 400, schema
and field validation errors are 422, oversized bodies are 413, unsupported media
are 415, and deadline expiry is 504. Internal diagnostics are never included in
these mappings.

## Bounded request handling

Create explicit limits; there are no universal operational defaults. The values
here are examples, not production recommendations:

```go
policy, err := httpapi.NewOperationalPolicy(1<<20, 10*time.Second, 25, 100)
if err != nil {
    return err
}
timeoutMiddleware, err := httpapi.RequestTimeoutMiddleware(policy)
if err != nil {
    return err
}

handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    type input struct {
        Name string `json:"name"`
    }
    value, err := httpapi.ReadJSON[input](r.Header, r.Body, policy,
        httpapi.DecodeOptions{DisallowUnknownFields: true})
    if err == nil {
        value, err = httpapi.ValidateRequest(value, func(v input) []httpapi.FieldError {
            if v.Name == "" {
                return []httpapi.FieldError{{Field: "/name", Code: "required"}}
            }
            return nil
        })
    }
    if err != nil {
        var rejection *httpapi.RequestError
        problem := httpapi.NewProblem(failure.Internal)
        if errors.As(err, &rejection) {
            problem = rejection.IntoProblem()
        }
        if request, ok := requestcontext.FromContext(r.Context()); ok {
            problem = problem.WithRequestID(request.RequestID())
        }
        _ = problem.Write(w)
        return
    }
    _ = (httpapi.APIResponse[input]{Data: value}).Write(w, http.StatusOK)
})

// Request IDs wrap timeout and rejection responses, not just successful handlers.
wrapped := httpapi.RequestIDMiddleware(timeoutMiddleware(handler))
_ = wrapped // Install on the application's http.Server or ServeMux.
```

`ReadJSON` checks media type before reading and bounds actual bytes, regardless
of `Content-Length`. `ParseJSON` handles an already bounded byte slice; never
collect an unbounded request just to call it. Bodies must contain one UTF-8 JSON
object. Only absent or `identity` content encoding is supported. Content type
must be one concrete `application/json` or `application/*+json` media type, with
at most one UTF-8 charset. Duplicate charset declarations are rejected.
Reader diagnostics and JSON parser messages are not returned to clients.

Go decoding deliberately makes the following differences explicit:

- Missing required fields and `null` values do not get Rust serde's automatic
  non-optional-field checks. Validate presence and semantic requirements in the
  endpoint validator, using pointers or a custom `UnmarshalJSON` when absence
  must be distinguished from a zero value.
- Unknown fields are permitted unless `DisallowUnknownFields` is enabled.
- Duplicate object keys are rejected with 422 everywhere, including maps;
  this is stricter than serde's map decoding. Malformed syntax takes priority
  and returns 400 even when a field also fails schema validation.
- Other DTO decoding details follow `encoding/json`, including case-insensitive
  struct-field matching and replacement of unpaired escaped Unicode surrogates.
  Use endpoint-owned custom decoding when stricter lexical rules are required.

`PaginationRequest.Resolve` applies the policy default only for an absent limit.
Zero and above-maximum limits are rejected, never clamped. Empty cursors are
rejected; other cursors remain unchanged. Query decoding, unknown query fields,
cursor signing/decoding, ordering, and database access remain application-owned.
The zero operational policy is invalid and is rejected by entry points.

## Request identity and timeout semantics

`RequestIDMiddleware` generates a new UUID v4 at public ingress, replaces the
incoming `X-Request-ID`, stores a `RequestContext` in `context.Context`, and sets
the same response header. It preserves optional writer interfaces. For trusted
internal boundaries only, `ParseRequestID` accepts 1-128 ASCII letters, digits,
`-`, `_`, and `.`. IDs are diagnostic metadata, not authorization or idempotency
proof. Zero request IDs are invalid.

`RequestContext` records `time.Now()` with its monotonic component and returns
copies from `WithTimeout` and `WithDeadline`. A timeout is measured from the
recorded start, not from the end of parsing. Remaining time saturates at zero.
The HTTP timeout middleware uses the earliest policy, request-context, or parent
context deadline. Expired requests do not invoke the handler. A live request
that times out gets a correlated 504 problem, and downstream context is canceled.
Parent cancellation also produces a consistent 504 problem if a response can
still be written. Ordinary handler-produced 503 responses are not remapped.

Go cannot forcibly drop a running handler like Rust can drop a future. The
middleware uses `http.TimeoutHandler` for synchronized response buffering, panic
forwarding, and rejection of late writes (`http.ErrHandlerTimeout`). Handlers
and downstream operations must honor cancellation and return. Response expiry
wins over a handler completing at the same deadline. The timeout middleware is
not suitable for streaming, flushing, connection hijacking, or unbounded response
bodies. Configure server read/header/idle timeouts, body transport behavior,
response limits, and global concurrency limits in the consumer. A context
cancellation alone cannot interrupt every arbitrary body reader or undo remote
side effects; transactions, idempotency, and detached work remain consumer-owned.

## gRPC

`grpcapi.InsertRequestContext` copies the request ID into outgoing metadata and
applies its deadline without extending an earlier parent deadline. grpc-go
encodes `grpc-timeout` on the wire. No deadline is invented for an untimed request.
Always call the returned cancel function:

```go
ctx, cancel, err := grpcapi.InsertRequestContext(parent, requestContext)
if err != nil {
    return err
}
defer cancel()
// response, err := client.Operation(ctx, message)
_ = ctx
```

`RequestIDFromMetadata` validates IDs at trusted boundaries.
`StatusFromFailure` requires a client-safe message. `FailureKindForCode` returns
`ok == false` for OK and Canceled; applications must handle those explicitly.
`ProblemFromGRPCStatus` takes an application-owned classifier and never copies
the upstream gRPC message into public details. RPC schemas, authentication,
resource visibility, retries, and interceptors remain application-owned.

## Observability

`ExtractHTTPContext` / `InjectHTTPContext` and `ExtractGRPCContext` /
`InjectGRPCContext` use OpenTelemetry's W3C Trace Context propagator.
Pass the active `context.Context` explicitly; Go has no implicit current span.
HTTP injection mutates the supplied headers; gRPC injection returns a metadata
copy so shared inputs are not modified.

`RequestCompletionMiddleware(logger)` creates a server span and logs method,
path without query, status, elapsed time, and an available request ID. It never
reads headers or bodies into logs. Paths may themselves contain sensitive
identifiers: choose routes and collector access policy accordingly. Install it
inside request identity, for example:

```go
wrapped := httpapi.RequestIDMiddleware(
    observability.RequestCompletionMiddleware(slog.Default())(
        timeoutMiddleware(handler),
    ),
)
_ = wrapped
```

Unlike Tower's response future, `net/http` invokes a handler until it returns,
so the completion duration includes any streaming work inside that handler.
net/http handlers do not return service errors; map application errors to HTTP
inside the handler. Panics retain the server's normal panic handling behavior.

For OTLP traces and slog logs, explicitly initialize once before constructing
handlers. This installs the global tracer provider and replaces the default
`slog` logger. Exporters are asynchronous; successful initialization is not a
collector connectivity check.

```go
guard, err := observability.InitTelemetry(ctx, observability.TelemetryConfig{
    ServiceName: "inventory",
    Endpoint: "http://localhost:4317",
    LogLevel: slog.LevelInfo,
})
if err != nil {
    return err
}
// During graceful shutdown, use a fresh bounded context, not a canceled request.
shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
if err := guard.Shutdown(shutdownCtx); err != nil {
    return err
}
```

`http://` explicitly selects plaintext; `https://` uses TLS. Collector URLs must
not contain credentials, paths other than `/`, query parameters, or fragments.
Log filtering uses explicit `slog` levels, defaulting to INFO, rather than
`RUST_LOG`. Standard OpenTelemetry exporter environment settings still apply
where not overridden by the explicit endpoint options. Both trace and log
providers are shut down even if one returns an error.

Only the configured collector receives exported logs. Applications remain
responsible for safe error text, collector access, database instrumentation,
redaction, and avoiding secrets, SQL literals, or customer documents in logs.

## Verification

```sh
gofmt -w config requestcontext failure httpapi grpcapi observability
go test -race -cover ./...
go vet ./...
```

Tests cover configuration boundaries, failure mappings, wire-format response
contracts, bounded reads, media-type rejection, validation, pagination, request
identity, cancellation, timeout correlation, metadata/deadline propagation over
an in-memory gRPC connection, W3C propagation, and trace/log export to a local
OTLP collector. Tests do not contact external infrastructure. Actual consumer
framework compatibility and deployment/collector policies still require
consumer-side validation.
