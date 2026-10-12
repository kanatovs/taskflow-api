# TaskFlow API

A Go backend service for collaborative project and task management.

> Status: early development. Features are added incrementally according to
> [`docs/ROADMAP.md`](docs/ROADMAP.md).

## Planned stack

- Go
- Chi
- PostgreSQL with pgx
- Redis
- JWT authentication
- Docker Compose
- OpenAPI
- Unit and integration tests
- GitHub Actions

## Development workflow

Development follows small, reviewable roadmap tasks. The repository-level
[`AGENTS.md`](AGENTS.md) defines architecture, testing, security, and Git rules.

## Current functionality

- HTTP server with environment configuration and startup validation, listening
  on `:8080` by default.
- Structured JSON logging, HTTP panic recovery, and graceful shutdown.
- Public `GET /health` endpoint returning `200 OK` and JSON:

  ```json
  {"data":{"status":"ok"}}
  ```

The health endpoint confirms that the API can serve HTTP requests. It does not
check database or cache availability.

## Run locally

Requires Go 1.25.5 or later. Run from the repository root:

```sh
go run ./cmd/api
```

In a second terminal:

```sh
curl -i http://localhost:8080/health
```

In Windows PowerShell, use `curl.exe` to avoid the `curl` alias.

The response has status `200 OK` and `Content-Type: application/json`.
`POST /health` returns `405 Method Not Allowed`; unknown paths return `404 Not Found`.
Both routing errors return JSON. A `405` response includes `Allow: GET` for
`/health`.

The configured listen address must be available; a startup failure exits with a
nonzero code.
Stop the process with `Ctrl+C`. The API stops accepting connections, waits for
active HTTP requests to finish, and exits with code `0` after a clean shutdown.
`SIGTERM` also triggers shutdown on systems that support it. If the shutdown
deadline expires, remaining connections are closed and the process exits with
code `1`. For direct process/signal control, build and run the executable rather
than signaling the `go run` wrapper:

```powershell
go build -o ./bin/taskflow-api.exe ./cmd/api
./bin/taskflow-api.exe
```

## JSON responses and errors

Successful resource responses use a `data` envelope. Errors use a separate
`error` envelope with a stable machine-readable code, a safe message, and a
`details` object. When there are no details, it is an empty object, not `null`.

For example, `GET /unknown` returns `404 Not Found`:

```json
{
  "error": {
    "code": "not_found",
    "message": "Route not found",
    "details": {}
  }
}
```

| Status | Code | Message |
| --- | --- | --- |
| 404 | `not_found` | Route not found |
| 405 | `method_not_allowed` | Method not allowed |
| 500 | `internal_error` | Internal server error |

Response helpers serialize JSON before sending the status and body. If
serialization fails, the API returns the safe `500` JSON response and logs
the internal error server-side. If writing to the client fails after the
response starts, the error is logged without attempting a second response.
HTTP middleware recovers handler panics. Before the response starts, it returns
the same safe `500` JSON error. After headers or body bytes have been sent, it
aborts the connection instead of appending an error to the partial response.
An intentional `http.ErrAbortHandler` remains an abort. Recovery only covers
the goroutine executing the HTTP handler; it does not recover panics in other
goroutines. Streaming handlers should use `http.ResponseController` to flush
through the response wrapper.

To check routing errors in Windows PowerShell while the API is running:

```powershell
curl.exe -i http://localhost:8080/unknown
curl.exe -i -X POST http://localhost:8080/health
```

## Logging

The application writes JSON logs to stdout using `log/slog`. Startup,
configuration/listener failures, shutdown, and internal HTTP server errors use
the same logger. Each request gets a server-generated `X-Request-ID` response
header; a client-supplied ID is not reused. Request completion logs include
`request_id`, `method`, `path`, `status`, `response_bytes`, `duration_ms`, and
`aborted`. Response serialization/write errors and panic logs carry the same
request ID so they can be correlated with the request completion event.

Request bodies, response bodies, query strings, and authorization headers are
not logged. Panic logs contain a stack trace without the panic value, which
could contain sensitive data. Paths are logged, so do not put credentials in
URL paths. An aborted request with no committed response has log status `0`;
after the response starts, the log preserves the status already sent.

## Configuration

Settings are read once from the process environment before the HTTP server
starts. Unset variables use the defaults below. Explicitly empty or invalid
values cause a startup error naming the variable, and the application exits
with code `1` before opening the HTTP listener. Configuration changes require
a restart. The application does not read `.env` files.

| Variable | Default | Meaning |
| --- | --- | --- |
| `HTTP_ADDR` | `:8080` | HTTP listen address in `host:port` format |
| `HTTP_READ_HEADER_TIMEOUT` | `5s` | Maximum time to read request headers |
| `HTTP_READ_TIMEOUT` | `10s` | Maximum time to read the entire request, including its body |
| `HTTP_WRITE_TIMEOUT` | `10s` | Maximum time to write the response |
| `HTTP_IDLE_TIMEOUT` | `60s` | Maximum time to wait for the next request on a keep-alive connection |
| `HTTP_SHUTDOWN_TIMEOUT` | `10s` | Maximum time to wait for active requests during shutdown |

`HTTP_ADDR` accepts an empty host, an IP address, or a hostname, with a numeric
port from `1` to `65535`. For example: `:8080`, `127.0.0.1:8081`,
`localhost:8081`, or `[::1]:8081`. IPv6 addresses require brackets. Whitespace
is rejected. An empty host listens on all available interfaces. Validation
checks the address format and port; hostname resolution and port availability
are checked when the server starts listening.

Timeouts use Go duration syntax, such as `500ms`, `5s`, or `1m30s`, and must be
greater than zero. Zero and negative values are rejected to keep explicit time
limits for every setting. Very short positive timeouts are accepted but can
interrupt requests.

To use a different address in Windows PowerShell:

```powershell
$env:HTTP_ADDR = "127.0.0.1:8081"
go run ./cmd/api
```

In a second terminal:

```powershell
curl.exe -i http://127.0.0.1:8081/health
```

After stopping the server, remove the override to return to the default:

```powershell
Remove-Item Env:HTTP_ADDR
```

For example, setting `HTTP_WRITE_TIMEOUT=0s` prevents startup with an error
containing `HTTP_WRITE_TIMEOUT: must be greater than zero`.

## Verification

```sh
go fmt ./...
go test ./...
go vet ./...
go build ./...
```
