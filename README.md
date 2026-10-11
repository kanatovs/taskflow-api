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
Routing errors currently use Chi's default responses. A consistent JSON error
format will be added in TF-004.

The configured listen address must be available; a startup failure exits with a
nonzero code.
Stop the process with `Ctrl+C`; graceful shutdown will be added in TF-005.

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
