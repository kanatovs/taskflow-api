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

## OpenAPI contract

[`docs/openapi.yaml`](docs/openapi.yaml) describes all implemented endpoints.
Validate its structure, local references and examples with Python 3.10 or newer:

```powershell
python -m venv .tmp/docs-venv
./.tmp/docs-venv/Scripts/python.exe -m pip install -r scripts/requirements-docs.txt
./.tmp/docs-venv/Scripts/python.exe scripts/validate-openapi.py
```

The validator is a development tool, not an API runtime dependency.

## Current functionality

- HTTP server with environment configuration and startup validation, listening
  on `:8080` by default.
- Structured JSON logging, HTTP panic recovery, and graceful shutdown.
- Docker Compose environment for the API and PostgreSQL with persistent storage.
- PostgreSQL connection pooling and public `GET /ready` readiness checks.
- Protected project CRUD with owner/admin/member permissions and member-only listing.
- Project membership management using existing user IDs, with protected owner roles.
- Protected project-scoped task CRUD with nullable assignment/due dates,
  database constraints, and integration tests.
- Task comments with author/owner/admin editing permissions.
- User registration with bcrypt, login with JWT, and authenticated current-user
  lookup at `GET /api/v1/users/me`.
- Public `GET /health` endpoint returning `200 OK` and JSON:

  ```json
  {"data":{"status":"ok"}}
  ```

The health endpoint confirms that the API can serve HTTP requests. It does not
check database or cache availability.

## Run locally

Requires Go 1.26.9 or a newer supported, patched release. Keep the local
toolchain current: Go security fixes also update the standard library compiled
into the API binary. First create `.env` and start/migrate PostgreSQL as described
in the Docker Compose section below. Then run from the repository root:

```powershell
$env:DATABASE_URL = "postgres://taskflow:local_example_only@127.0.0.1:5432/taskflow?sslmode=disable"
$env:JWT_SECRET = "local_example_only_change_me_32_bytes_minimum"
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
docker compose up -d --wait postgres
docker compose run --rm migrate up
docker compose up -d --build --wait api
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
environment; `go run ./cmd/api` does not load `.env`. Compose derives
`DATABASE_URL` for both `api` and `migrate` from PostgreSQL settings when unset.
Set an explicit URL when credentials need percent-encoding; its hostname and
port inside Compose must be `postgres:5432`. Remove a host-only `DATABASE_URL`
override before Compose commands (`Remove-Item Env:DATABASE_URL` in PowerShell,
or `unset DATABASE_URL` in a POSIX shell). Migrations receive only the database
URL, not the API's JWT secret.

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
**Warning: `docker compose down -v` deletes the local PostgreSQL volume and all
database data.** It is not a migration or repair command.
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

`POST /api/v1/auth/login` accepts `email` and `password` under the same strict
JSON rules. Login passwords contain 1 to 72 bytes; email uses the registration
format and normalization. Missing or invalid fields return `400`. Unknown
emails and incorrect passwords return the same `401` error: `Invalid email or
password`. Successful login returns `200` and a non-cacheable `data` object:

```json
{"data":{"access_token":"<signed JWT>","token_type":"Bearer","expires_in":900}}
```

Tokens use HS256 with a secret read from `JWT_SECRET` (at least 32 bytes), a
15-minute default lifetime, and only `sub` (decimal user ID), `iat`, and `exp`
claims. There are no refresh tokens. Choose a random secret for a deployment;
the public `.env.example` secret is for local checks only. If an existing `.env`
predates authentication, add `JWT_SECRET` and optionally `JWT_ACCESS_TTL` before
starting the updated API. The application never logs tokens or password data.

`GET /api/v1/users/me` requires `Authorization: Bearer <access token>` and returns
the current user in `data` without password data. The middleware verifies HS256,
signature, required expiration and issued-at timestamps, and a positive decimal
user ID in `sub`, then puts the ID in the request context. Missing, malformed,
invalid, or expired credentials return `401` with `WWW-Authenticate: Bearer`.
An authenticated token whose user no longer exists also returns `401`.
Unexpected repository failures return a safe `500`. The response is not cached.
`/health`, `/ready`, registration, and login remain public.

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
| `JWT_SECRET` | Required | HMAC secret, at least 32 bytes, not blank |
| `JWT_ACCESS_TTL` | `15m` | Access token lifetime, at least one second |

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
$env:JWT_SECRET = "local_example_only_change_me_32_bytes_minimum"
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
docker compose run --rm migrate version
```

The `migrate` tools-profile service waits for the Compose PostgreSQL healthcheck,
uses golang-migrate and read-only SQL files from `./migrations`, and exits with a
nonzero status on failure. It is selected directly by `compose run`; no explicit
profile flag or host migrate installation is needed. Repeating `up` reports
`no change`. golang-migrate records the version and dirty flag in
`public.schema_migrations`; `version` should report `2` without `(dirty)` after
the user and project migrations succeed. Inspect the database state directly:

```powershell
docker compose exec -T postgres sh -c 'exec psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1 -c "SELECT version, dirty FROM public.schema_migrations;" -c "\dt public.*" -c "\d public.users"'
Get-Content -Raw ./scripts/check-users-schema.sql | docker compose exec -T postgres sh -c 'exec psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1'
```

The SQL check is read-only. It fails if the migration state, users columns,
defaults, or constraints differ from migration 1, including an absent table.
In a POSIX shell, feed the same file with
`docker compose exec -T postgres sh -c 'exec psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1' < scripts/check-users-schema.sql`.
`GET /ready` checks connectivity only; a `200` does not prove tables exist.
Run migrations and this schema check before authentication requests.

To roll back exactly one migration deliberately:

```powershell
docker compose run --rm migrate down 1
```

**Warning: rolling back migration 2 drops projects and memberships. Rolling back
migration 1 drops `users` and deletes all user data.**
Use rollback only when that data loss is intended, preferably in a disposable
database. Restore the schema with `docker compose run --rm migrate up`; deleted
data is not restored. A failed migration leaves a dirty state: inspect the error
and repair the cause before continuing. Do not use `force` to hide a failed
migration, or remove the development volume to repair migration state.

Migration 1 creates `users`
with a generated bigint ID, TIMESTAMPTZ timestamps, a unique normalized email,
and checks for nonempty password hashes and display names. Repositories use
parameterized SQL with per-operation deadlines. User JSON omits password hashes.

Migration 2 creates `projects` and `project_members`. Foreign keys reference
users, deleting a project cascades to its memberships, each project/user pair
is unique, and roles are constrained to `owner`, `admin`, or `member`. A partial
unique index permits only one owner membership per project. Project creation
inserts the project and its owner membership in one transaction. User deletion
is restricted while projects or memberships reference that user.

## Project API

All project endpoints require a Bearer access token. The creator is the owner.

| Method | Path | Permission | Success |
| --- | --- | --- | --- |
| POST | `/api/v1/projects` | Authenticated user | 201 |
| GET | `/api/v1/projects` | Only the caller's projects | 200 |
| GET | `/api/v1/projects/{projectID}` | Any project member | 200 |
| PATCH | `/api/v1/projects/{projectID}` | Owner or admin | 200 |
| DELETE | `/api/v1/projects/{projectID}` | Owner | 204, empty body |

Create accepts `{"name":"Portfolio","description":"Optional description"}`.
Names are trimmed and contain 1 to 100 characters without control characters.
Descriptions default to an empty string and contain at most 2000 characters;
line breaks and tabs are allowed. JSON bodies are limited to 4096 bytes and
unknown fields are rejected. PATCH accepts `name`, `description`, or both;
omitted/null fields are preserved, and an empty patch returns 400. Set
`description` to `""` to clear it. Ownership cannot be changed.

Listing accepts only `page` (default 1) and `page_size` (default 20, maximum
100), orders by project ID, and returns `data` plus `meta` with `page`,
`page_size`, and `total`. Empty pages return `data: []`. No search or advanced
filters are supported. Missing authentication returns 401, non-members or
insufficient roles 403, missing resources 404, and invalid input 400.

## Project membership API

| Method | Path | Success |
| --- | --- | --- |
| GET | `/api/v1/projects/{projectID}/members` | 200 |
| POST | `/api/v1/projects/{projectID}/members` | 201 |
| PATCH | `/api/v1/projects/{projectID}/members/{userID}` | 200 |
| DELETE | `/api/v1/projects/{projectID}/members/{userID}` | 204, empty body |

All endpoints require authentication and project membership. Any member can
list members; the same `page`/`page_size` limits apply, ordered by user ID.
POST requires `{"user_id":42,"role":"member"}`; PATCH requires
`{"role":"admin"}`. Target users must already exist. No email invitations
are sent. Responses contain only `project_id`, `user_id`, `role`, and
`joined_at`. Membership listing queries never select passwords, and responses
never contain user credentials.

| Actor | Add | Change role | Remove |
| --- | --- | --- | --- |
| Owner | Admin or member | Admin/member to admin/member | Admin or member |
| Admin | Member only | Member to member only (no-op) | Member only |
| Member | Forbidden | Forbidden | Forbidden |

Owner membership cannot be demoted or removed, including by the owner.
Ownership transfer is not supported. Duplicate membership returns 409;
missing users/memberships/projects return 404, invalid roles return 400, and
insufficient permissions return 403. Each membership operation is scoped to
the project in the URL. Mutations lock the project row, then recheck actor and
target roles inside the transaction, so concurrent permission changes cannot
bypass the service's checks. This serializes writes within a project; writes
to separate projects can proceed independently.

Run the project and membership Docker smoke checks from PowerShell:

```powershell
./scripts/projects-smoke.ps1
```

The script uses a fresh `taskflow-project-smoke-*` project, a separate volume,
public example credentials, and ports 18082/15436 (overridable with `-ApiPort`
and `-PostgresPort`). It verifies migration up/repeated-up/down/up, schema
constraints, registration/login, project CRUD, membership permissions, owner
protection, cross-project rejection, secret-free responses/logs, and graceful
shutdown. It removes only its own test project/volume and restores environment
overrides. The development database and `.env` are preserved.

For Go integration checks, set `TEST_POSTGRES_URL` to a disposable local
PostgreSQL instance and run `go test ./...`. Each test uses its own database.
Tests also exercise transaction rollback, duplicate membership races,
independent concurrent PATCH fields, role changes while a write waits on a
project lock, request cancellation/deadlines, and connection cleanup.

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

Authentication tests cover strict JSON and validation boundaries, normalized
duplicate emails (including concurrent registration), stored bcrypt hashes,
login without account disclosure, JWT signing and verification, malformed
Bearer headers, current-user lookup, repository failures, SQL constraints,
query cancellation, deadlines, connection cleanup, and migration down/up.
An HTTP integration test runs registration/login/current-user requests against
the real PostgreSQL repository in a fresh database and checks secret-free logs.

Run the repeatable Docker smoke script from Windows PowerShell:

```powershell
./scripts/auth-smoke.ps1
```

It creates a fresh `taskflow-auth-smoke-*` Compose project with public example
settings, ports `18081` and `15434`, and a separate volume. Use `-ApiPort` and
`-PostgresPort` to choose other available ports. It verifies migration
up/repeated-up/down/up and migration state, rejects missing/invalid users schemas
with the SQL regression check, builds the API, checks authentication responses and
safe logs, stops only its own PostgreSQL, verifies `/health` stays `200` while
`/ready` returns `503`, then checks recovery and graceful API shutdown. Finally
it removes only its own project and test volume and restores environment
overrides. It never edits `.env` or touches the development volume.

This epic supports ASCII emails and password-based access tokens. Refresh
tokens, token revocation, project permissions, and rate limiting are outside
its scope. Tokens expire according to `JWT_ACCESS_TTL`; rotating `JWT_SECRET`
invalidates signatures created with the old secret. A deleted user's token is
rejected at current-user lookup. Migrations are an explicit setup step.

```sh
go fmt ./...
go test ./...
go vet ./...
go build ./...
```

## Tasks

All task routes require a Bearer JWT and current project membership:

- `POST /api/v1/projects/{projectID}/tasks` creates a task (201).
- `GET /api/v1/projects/{projectID}/tasks` lists tasks (200).
- `GET /api/v1/projects/{projectID}/tasks/{taskID}` reads a task (200).
- `PATCH /api/v1/projects/{projectID}/tasks/{taskID}` changes fields (200).
- `DELETE /api/v1/projects/{projectID}/tasks/{taskID}` deletes a task (204).

Create accepts `title` (required, trimmed, 1–200 characters), `description`
(up to 2000 characters), `status` (`todo` by default), `priority` (`medium`
by default), optional `assignee_id`, and optional RFC3339 `due_date`.
The server sets the creator and project IDs. PATCH preserves omitted fields;
`assignee_id: null` and `due_date: null` clear their values. Empty patches,
unknown fields, invalid IDs, enum values, timestamps, and oversized bodies are
rejected. Dates are returned in UTC. Collections use `page`/`page_size`
(default 1/20, maximum size 100), stable ID order, and total metadata.
Task lookup and mutation always use both project and task IDs.

Task listing accepts any combination of `status`, `priority`, `assignee_id`,
`creator_id`, `due_from`, `due_to`, and `search`. Due-date bounds are inclusive
RFC3339 timestamps; tasks without a due date do not match a date bound. Search
is a case-insensitive literal substring of title or description (up to 200
characters); `%` and `_` are ordinary characters. URL-encode query values.

`sort` accepts `id` (default), `created_at`, `updated_at`, `due_date`, `priority`,
`status`, or `title`; `order` is `asc` (default) or `desc`. Priority sorts
low/medium/high and status sorts todo/in_progress/done. Null dates always sort
last. ID in the same direction breaks ties. Repeated/unknown parameters,
invalid values, and reversed date ranges return 400. Count and page share one
PostgreSQL snapshot; the total is retained even for an empty page. Offset
pagination is deterministic for unchanged data; separate requests can observe
concurrent changes. Search scans the selected project's text without a full-text
index; no new search service or dependency is introduced.
All current members may create and read tasks. Owners/admins and the task creator
may edit, assign, or delete a task. The current assignee may update only `status`;
other members may not mutate it. Every new task starts in `todo`.

Allowed transitions are `todo -> in_progress`, `in_progress -> todo`,
`in_progress -> done`, and `done -> in_progress`. Repeating the current status
is allowed; other transitions return 409. Assignment requires a current member
of the same project (400 otherwise). Removing membership preserves historical
creator/assignee IDs but immediately removes access; a manager can clear or
replace the assignment. Permissions, assignment, and transitions are checked
again inside the write transaction under the same project lock used by member
changes, so a stale service check cannot authorize a write.

## Task comments

Current project members may create and list comments:

- `POST /api/v1/projects/{projectID}/tasks/{taskID}/comments` (201).
- `GET /api/v1/projects/{projectID}/tasks/{taskID}/comments` (200).
- `PATCH /api/v1/projects/{projectID}/tasks/{taskID}/comments/{commentID}` (200).
- `DELETE /api/v1/projects/{projectID}/tasks/{taskID}/comments/{commentID}` (204).

Create and PATCH accept only `body`, trimmed to 1–2000 characters. The server
sets the author. Only the comment author or a current project owner/admin may
edit or delete it. Task creation/assignment does not grant moderation rights.
Collections use the same page defaults and stable ID order as tasks. Mixed
project/task/comment IDs return 404 after the project membership check.
Deleting a task or project cascades to its comments. Member removal preserves
authorship, but a removed author cannot read or mutate comments.

## Task and comment verification

Run all Go tests against a separate local PostgreSQL instance:

```powershell
$env:TEST_POSTGRES_URL = "postgres://taskflow:local_example_only@127.0.0.1:15437/taskflow?sslmode=disable"
go test -count=1 ./...
Remove-Item Env:TEST_POSTGRES_URL
```

The existing test helper creates and drops only randomly named `taskflow_test_*`
databases. Never point it at development data or production. Without the variable,
PostgreSQL integration tests are skipped. Tests cover task/comment CRUD, input,
authentication, authorization, mixed IDs, repository failures, context propagation,
deadlines, nullable PATCH fields, constraints, cascades, filters/search/pagination,
and writes waiting on concurrent membership/role/status changes.

```powershell
powershell -ExecutionPolicy Bypass -File ./scripts/tasks-smoke.ps1
```

The smoke script uses a randomly named Compose project, API port 18083 and
PostgreSQL port 15438 (override with `-ApiPort` / `-PostgresPort`). It verifies
migration up/repeated-up/down/down/up and clean versions 4/3/2/4, task/comment
schema, registration/login, all new HTTP routes, permissions, nullable fields,
combined filters, stable pages, cross-project/task rejection, logs, and graceful
shutdown. It removes only its own containers and volume in `finally`, and
restores process environment variables. It never rolls back the development DB.

## Optional Redis detail cache

PostgreSQL is authoritative. Redis only accelerates single project and task
detail GET requests. Lists, filters, search, comments, memberships, users, password
hashes, tokens, HTTP envelopes, errors and authorization decisions are not cached.

Each detail request verifies the JWT, then reads the current user and membership
from PostgreSQL before accessing Redis. A cache hit avoids the entity SELECT;
authorization still uses PostgreSQL. On a miss, the detail repository reads
PostgreSQL and stores the successful domain entity as JSON with an explicit TTL.
Centralized keys are `taskflow:v1:project:{projectID}` and
`taskflow:v1:project:{projectID}:task:{taskID}`. Task keys include both IDs and
decoded identities are checked, preventing cross-project cache reuse.

| Variable | Default | Meaning |
| --- | --- | --- |
| `REDIS_ADDR` | empty on host; `redis:6379` in Compose | Host:port; empty disables caching |
| `REDIS_PASSWORD` | empty | Optional client password; provision server authentication separately |
| `REDIS_DB` | `0` | Standalone database number, 0 through 15 |
| `CACHE_TTL` | `1m` | Positive TTL, at least 1ms |
| `REDIS_DIAL_TIMEOUT` | `200ms` | Connection timeout, at least 1ms |
| `REDIS_OPERATION_TIMEOUT` | `100ms` | Per-operation context/socket/pool timeout, at least 1ms |

```powershell
docker compose up -d --wait postgres redis
docker compose run --rm migrate up
docker compose up -d --build --wait api
```

The pinned Redis container has a healthcheck, a 128MB eviction limit, no published
port and no persistent volume or persistence files. Its data is disposable: Redis
can be restarted, removed or recreated safely. `docker compose up -d --force-recreate
redis` clears this cache without changing PostgreSQL. Never remove the PostgreSQL
volume as a cache repair step. Redis is not a readiness dependency: `/ready` only
checks PostgreSQL. A lazy client permits startup during Redis downtime and closes
after HTTP shutdown.

Redis timeouts, connection failures, malformed JSON or mismatched entity IDs
produce safe warnings and fall back to PostgreSQL. Malformed entries are deleted
when possible. Warnings exclude raw errors, cached payloads and credentials.
Only successful entity reads populate the cache; negative results are not cached.
Concurrent misses may duplicate entity reads; there are no distributed locks,
stampede prevention, list caching or cache warming. PostgreSQL reads remain
necessary for authentication and authorization even on a hit.

Successful project updates/deletes invalidate that project's detail key.
Successful task updates/deletes invalidate the task detail key, including
assignment, status, priority and due-date changes. Creation does not invalidate
detail keys, and comments do not invalidate tasks because task details contain no
derived comment information. Writes and their permission checks always use the
PostgreSQL repositories, never cached entities.

Invalidation failures are logged and never roll back a committed PostgreSQL
mutation or turn it into an HTTP 500. Stale data can survive a failed invalidation
until its TTL expires. A read already in flight can also refill an old value
after a concurrent invalidation; this accepted cache-aside race is bounded by the
TTL of that fill. A successful invalidation removes the existing entry; subsequent
uncontended reads load the new database value. There are no consistency guarantees
for cached details beyond this bounded staleness. Authorization remains live in
PostgreSQL, including when a member is removed or a project is deleted. Task keys
left by a project cascade become inaccessible and expire without a key scan.

Run `go test ./...` for unit tests. For the Redis client integration test, publish
an isolated disposable Redis on loopback (the normal Compose service has no host
port), set `TEST_REDIS_ADDR=127.0.0.1:<port>`, and run
`go test -count=1 ./internal/platform/cache`. It only deletes its own unique test
key and never flushes a database. PostgreSQL integration tests continue to use
`TEST_POSTGRES_URL` and unique disposable databases as documented above.

```powershell
powershell -ExecutionPolicy Bypass -File ./scripts/cache-smoke.ps1
```

This script creates a random isolated Compose project with PostgreSQL on 15440,
Redis on 16380 and the API on 18084, all on loopback (override with `-PostgresPort`,
`-RedisPort`, `-ApiPort`). `scripts/compose.cache-test.yaml` publishes Redis only
for this test environment. The script runs migrations twice, the complete Go
suite with real PostgreSQL/Redis, and project/task cache HTTP smoke checks. It
verifies hit/miss, TTL, JSON safety, permissions, removed members, malformed
values, invalidation, deletion, uncached lists/search, fallback, writes and
authentication during an outage, API startup with Redis down, recovery, disabled
caching and graceful shutdown. It never flushes Redis and removes only its own
containers/volume, restoring environment variables in `finally`.
