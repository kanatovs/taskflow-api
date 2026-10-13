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
- Docker Compose environment for the API and PostgreSQL with persistent storage.
- Public `GET /health` endpoint returning `200 OK` and JSON:

  ```json
  {"data":{"status":"ok"}}
  ```

The health endpoint confirms that the API can serve HTTP requests. It does not
check database or cache availability.

## Run locally

Requires Go 1.26.9 or a newer supported, patched release. Keep the local
toolchain current: Go security fixes also update the standard library compiled
into the API binary. Run from the repository root:

```powershell
$env:DATABASE_URL = "postgres://taskflow:local_example_only@127.0.0.1:5432/taskflow?sslmode=disable"
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

## Run with Docker Compose

Requires a running Docker Engine with Linux containers and Docker Compose with
`up --wait` support. On Windows, start Docker Desktop first. Go is not required
on the host for this workflow.

Create a local `.env` from the example once. In Windows PowerShell:

```powershell
Copy-Item .env.example .env
```

On Linux or macOS, use `cp .env.example .env`. Keep an existing `.env` when
repeating setup. The supplied password is a public example for local development;
do not put real credentials in `.env.example` or commit `.env`.

From the repository root:

```powershell
docker compose config --quiet
docker compose build
docker compose up -d --wait
docker compose ps
curl.exe -i http://localhost:8080/health
docker compose exec -T postgres psql -U taskflow -d taskflow -v ON_ERROR_STOP=1 -c "SELECT 1;"
```

On Linux or macOS, use `curl` instead of `curl.exe`. Both services should become
healthy, the HTTP response should contain `{"data":{"status":"ok"}}`, and the
SQL query should return `1`. The SQL commands use the example database/user;
adjust `-U` and `-d` if you change those settings.

Compose reads `.env` for variable interpolation and explicitly passes HTTP
settings to `api` and database settings to `postgres`. Shell environment values
take precedence over `.env`. The Go application still reads only its process
environment; `go run ./cmd/api` does not load `.env`. Compose derives the API's
`DATABASE_URL` from PostgreSQL settings when unset. Set an explicit URL when
credentials need percent-encoding; its hostname inside Compose is `postgres`.

| Compose setting | Example/default | Meaning |
| --- | --- | --- |
| `API_PORT` | `8080` | API port on the host |
| `POSTGRES_PORT` | `5432` | PostgreSQL port on the host |
| `API_STOP_GRACE_PERIOD` | `30s` | Container stop deadline before Docker sends SIGKILL |
| `POSTGRES_DB` | `taskflow` | Database initialized on the first PostgreSQL start |
| `POSTGRES_USER` | `taskflow` | Local development database user initialized on the first start |
| `POSTGRES_PASSWORD` | `local_example_only` | Public example password; required and non-empty |

Both published ports bind to `127.0.0.1`. Change the host port settings if `8080`
or `5432` is occupied, and use the selected API port in HTTP checks. Inside the
containers, the ports stay `8080` and `5432`. Compose fixes `HTTP_ADDR=:8080` so
port forwarding and the API healthcheck use the same address. HTTP timeouts use
the settings described below; explicitly empty values are passed through and
rejected by application validation.

The Dockerfile builds a static binary with Go 1.26.9, matching the module's
minimum version, then copies only that binary into an Alpine runtime image.
The API runs as UID/GID `10001`, without root privileges. The direct `ENTRYPOINT`
makes the binary PID 1 so it receives Docker's SIGTERM. `.dockerignore` excludes
local environment files, Git data, and build output from the build context.

PostgreSQL uses the official `postgres:17-alpine` image and a named
`postgres_data` volume at `/var/lib/postgresql/data`. The `POSTGRES_USER` created
by this image is a database superuser; these credentials are intended only for
this local development environment. The API uses a lazy pgxpool connection pool.
PostgreSQL outages do not prevent HTTP startup or `GET /health`. `GET /ready`
pings PostgreSQL with a deadline: it returns `200` with
`{"data":{"status":"ready"}}` or a safe `503` error. The PostgreSQL healthcheck
uses `pg_isready`; the API healthcheck calls `/health`.

To inspect logs and check graceful shutdown:

```powershell
docker compose logs --tail 20 api postgres
docker compose stop api
docker compose logs --tail 10 api
docker compose ps -a
docker compose up -d --wait api
```

After stopping, the API should log `HTTP server shutting down` and
`HTTP server stopped`, and its container should have exit code `0`. Keep
`API_STOP_GRACE_PERIOD` longer than `HTTP_SHUTDOWN_TIMEOUT`, with a margin for
process exit; the examples use `30s` and `10s` respectively.

Stop and remove the development containers while keeping database data:

```powershell
docker compose down
```

The named volume survives `down` and is reused on the next `up`.
`POSTGRES_DB`, `POSTGRES_USER`, and `POSTGRES_PASSWORD` only initialize an empty
data directory. Editing `.env` does not change credentials in an existing
database. Do not delete a volume to change a password if it contains data you
need. Recreate containers with `docker compose up -d --wait` after changing
environment settings; `docker compose restart` does not apply those changes.

If a command cannot reach Docker Engine, check that Docker Desktop is running
with Linux containers. For an unhealthy container, inspect `docker compose ps`
and `docker compose logs --tail 50 api postgres`. `config --quiet` validates the
Compose configuration without printing expanded credentials; avoid sharing the
output of plain `docker compose config` or a full container inspection.

### Docker smoke checks

These checks exercise the container environment without changing application
code or adding application tables. Use the example configuration in a disposable
Compose project with separate host ports, while keeping the normal development
volume untouched. Run this PowerShell block in a fresh terminal from the root;
the temporary port overrides are restored even if a check fails:

```powershell
$previousApiPort = $env:API_PORT
$previousPostgresPort = $env:POSTGRES_PORT
try {
    $env:API_PORT = "18080"
    $env:POSTGRES_PORT = "15432"
    docker compose --env-file .env.example -p taskflow-tf006-smoke up -d --build --wait
    curl.exe -i http://localhost:18080/health
    docker compose --env-file .env.example -p taskflow-tf006-smoke exec -T postgres psql -U taskflow -d taskflow -v ON_ERROR_STOP=1 -c "CREATE TABLE tf006_smoke (value TEXT NOT NULL); INSERT INTO tf006_smoke VALUES ('volume survives container recreation');"
    docker compose --env-file .env.example -p taskflow-tf006-smoke down
    docker compose --env-file .env.example -p taskflow-tf006-smoke up -d --wait
    docker compose --env-file .env.example -p taskflow-tf006-smoke exec -T postgres psql -U taskflow -d taskflow -v ON_ERROR_STOP=1 -c "SELECT value FROM tf006_smoke;"
    docker compose --env-file .env.example -p taskflow-tf006-smoke run --rm --no-deps -e HTTP_WRITE_TIMEOUT=0s api
    if ($LASTEXITCODE -ne 1) { throw "Invalid timeout must exit with code 1" }
    docker compose --env-file .env.example -p taskflow-tf006-smoke stop api
    docker compose --env-file .env.example -p taskflow-tf006-smoke logs --tail 10 api
    docker compose --env-file .env.example -p taskflow-tf006-smoke ps -a
} finally {
    docker compose --env-file .env.example -p taskflow-tf006-smoke down --volumes
    $env:API_PORT = $previousApiPort
    $env:POSTGRES_PORT = $previousPostgresPort
}
```

Check that the persisted row is returned after `down`/`up`, the invalid run logs
`HTTP_WRITE_TIMEOUT` and exits with `1`, and the stopped API exits cleanly with
`0`. The `finally` block deletes only the disposable project's containers and
volume, including its test table. Reserve `taskflow-tf006-smoke` for these checks;
do not store development data in it. Other failed native commands must also be
investigated; this manual block does not automatically assert every result.

## JSON responses and errors

`POST /api/v1/auth/register` accepts a JSON object with `email`, `password`, and
`display_name`. Unknown fields, malformed JSON, multiple JSON values, and bodies
over 4096 bytes are rejected with `400`. Email is trimmed and lowercased; this
epic supports ASCII email addresses with a dotted DNS domain. Display names are
trimmed and contain 1 to 100 characters without control characters. Passwords
contain 12 to 72 bytes (bcrypt's maximum), are not trimmed, and are stored only
as bcrypt hashes at the default cost. Blank-only passwords are rejected.

Successful registration returns `201` with the user in `data`, including its ID
and UTC timestamps, without password data. Duplicate normalized email returns
`409`; validation returns `400`; unexpected persistence failures return a safe
`500`. Database uniqueness handles concurrent duplicate registration.

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
| `DATABASE_URL` | Required | PostgreSQL URL, including credentials and database name |
| `DATABASE_TIMEOUT` | `3s` | Connection and readiness timeout |

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
$env:DATABASE_URL = "postgres://taskflow:local_example_only@127.0.0.1:5432/taskflow?sslmode=disable"
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

### User schema and migrations

Apply migrations explicitly before using database-backed endpoints; the API does
not modify the schema at startup:

```powershell
docker compose up -d --wait postgres
docker compose run --rm migrate up
```

The tools-profile service uses golang-migrate and read-only SQL files from
`migrations/`. Repeating `up` reports `no change`. Migration 1 creates `users`
with a generated bigint ID, TIMESTAMPTZ timestamps, a unique normalized email,
and checks for nonempty password hashes and display names. Repositories use
parameterized SQL with per-operation deadlines. User JSON omits password hashes.

To run PostgreSQL tests, start a separate Compose project and set the test URL:

```powershell
$env:POSTGRES_PORT = "15433"
docker compose --env-file .env.example -p taskflow-auth-check up -d --wait postgres
$env:TEST_POSTGRES_URL = "postgres://taskflow:local_example_only@127.0.0.1:15433/taskflow?sslmode=disable"
go test ./...
Remove-Item Env:TEST_POSTGRES_URL
Remove-Item Env:POSTGRES_PORT
```

Reserve this project for tests. Repository tests create uniquely named
`taskflow_test_*` databases, apply the migration, and drop only those databases
at cleanup. Without `TEST_POSTGRES_URL`, PostgreSQL tests are skipped. Use only
the disposable local Compose instance. The ordinary development volume is
never removed by the tests. Migration down/up checks run only on disposable
databases. Readiness tests also exercise an unavailable pool.

```sh
go fmt ./...
go test ./...
go vet ./...
go build ./...
```
