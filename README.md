# TaskFlow API

[![CI](https://github.com/kanatovs/taskflow-api/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/kanatovs/taskflow-api/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/kanatovs/taskflow-api?label=release)](https://github.com/kanatovs/taskflow-api/releases/tag/v1.0.0)

TaskFlow is a Go REST backend for collaborative project and task management.
It demonstrates explicit business rules, parameterized SQL, transactions,
JWT authentication, and optional Redis caching without an ORM or microservices.
The repository contains the backend only.

**TaskFlow API v1.0.0 has been released** and is available in
[GitHub Releases](https://github.com/kanatovs/taskflow-api/releases/tag/v1.0.0).
TF-001 through TF-030 are complete in [the roadmap](docs/ROADMAP.md).
CI on `main` is passing; see the badge above and the
[successful release CI run](https://github.com/kanatovs/taskflow-api/actions/runs/38736189076).

- [OpenAPI contract](docs/openapi.yaml)
- [Architecture and diagrams](docs/architecture.md)
- [Testing guide](docs/testing.md) and [CI workflow](.github/workflows/ci.yml)
- [Security and reliability review](docs/release-readiness.md)
- [Changelog](CHANGELOG.md) and [v1.0.0 release notes](docs/releases/v1.0.0.md)

## Capabilities and stack

- Registration, bcrypt password hashing, login, HS256 JWT access tokens, and an
  authentication rate limit shared by login and registration.
- Projects with owner/admin/member roles and project-scoped authorization.
- Tasks with assignment, status transitions, due dates, and comments.
- Task filtering, literal substring search, stable sorting and pagination.
- PostgreSQL persistence with four up/down migrations and optional Redis detail cache.
- JSON errors, request IDs, structured logs, deadlines, and graceful shutdown.
- Docker Compose, OpenAPI, unit/integration tests, smoke scripts and GitHub Actions.

Stack: Go 1.26.9+, `net/http`, Chi, pgx, raw SQL, PostgreSQL 17, JWT v5,
`golang.org/x/crypto/bcrypt`, Redis 8.2, go-redis, golang-migrate, Docker and
the standard `testing` / `httptest` packages. See [go.mod](go.mod) for versions.

## Architecture and request flow

```text
HTTP -> middleware -> handler -> service -> repository -> PostgreSQL
                                              |
                                  authorized detail reads -> Redis
```

Handlers parse HTTP input and map errors. Services own business rules and access
checks. PostgreSQL repositories own SQL and transactions. Cache decorators wrap
only project/task detail readers. `cmd/api/main.go` composes these dependencies,
starts the server, and closes clients after HTTP shutdown. Context and deadlines
flow through all layers; no request context is stored on a service.

```text
cmd/api/                       Application composition and startup
internal/config/               Environment validation
internal/domain/               Entities, domain errors and shared rules
internal/handler/              Routes, JSON, middleware and HTTP tests
internal/service/              Business rules and consumer interfaces
internal/repository/postgres/  SQL persistence
internal/repository/cache/     Project/task cache decorators
internal/platform/             Database, Redis, JWT and HTTP lifecycle
internal/testutil/              Disposable PostgreSQL fixtures
migrations/                    Four versioned up/down migration pairs
scripts/                       Checks and isolated integration/smoke runners
docs/                          OpenAPI, architecture, tests and roadmap
.github/workflows/             CI
```

[Architecture, ER diagram and cache sequence](docs/architecture.md) explain
transaction boundaries, authorization, data relationships and failure behavior.

## Local setup (Windows PowerShell)

Requirements: Git, running Docker Desktop with Linux containers, and Docker
Compose with `up --wait` support. Host Go is needed for host execution/tests,
not for the containerized API. Python 3.10+ is needed only for OpenAPI validation.
The commands below run from the repository root.

Create the local environment file once; preserve an existing file:

```powershell
if (-not (Test-Path .env)) { Copy-Item .env.example .env }
docker compose config --quiet
docker compose up -d --wait postgres redis
docker compose run --rm migrate up
docker compose run --rm migrate version
docker compose up -d --build --wait api
docker compose ps
Invoke-RestMethod http://localhost:8080/health
Invoke-RestMethod http://localhost:8080/ready
```

The migration version must be **4**, without `(dirty)`. PostgreSQL, Redis and API
should be healthy. `/health` returns `{"data":{"status":"ok"}}`; `/ready`
returns `{"data":{"status":"ready"}}` after a successful PostgreSQL ping.
The API does not apply migrations automatically, and `/ready` does not validate
the schema. Redis availability does not affect readiness.

Ports 8080 and 5432 bind to loopback. Redis is internal to Compose. PostgreSQL
data persists in a named volume; Redis has no volume or persistence files.

To run Go on the host after starting/migrating the database:

```powershell
docker compose stop api
$env:DATABASE_URL = 'postgres://taskflow:local_example_only@127.0.0.1:5432/taskflow?sslmode=disable'
$env:JWT_SECRET = 'local_example_only_change_me_32_bytes_minimum'
$env:REDIS_ADDR = ''
go run ./cmd/api
```

`go run` reads process variables, not `.env`. Ctrl+C shuts down the host API.
Before returning to Compose, remove the host overrides:

```powershell
Remove-Item Env:DATABASE_URL, Env:JWT_SECRET, Env:REDIS_ADDR -ErrorAction SilentlyContinue
```

Compose needs `postgres:5432` in its database URL; a host process needs
`127.0.0.1:<published port>`. Process variables override `.env`. Special characters
in URL credentials must be percent-encoded. The public example credentials and
secret are for local development only.

## Environment variables

All durations use Go syntax (`200ms`, `3s`, `1m`) and must be positive. Explicitly
empty invalid settings are rejected rather than silently defaulted.

| Setting | Default / requirement | Purpose |
| --- | --- | --- |
| `HTTP_ADDR` | `:8080` on host; fixed `:8080` in Compose | Listen host:port; port 1-65535 |
| `HTTP_READ_HEADER_TIMEOUT` | `5s` | Read request headers |
| `HTTP_READ_TIMEOUT` | `10s` | Read entire request |
| `HTTP_REQUEST_TIMEOUT` | `5s`, less than `HTTP_WRITE_TIMEOUT` | Shared context deadline for HTTP processing |
| `HTTP_WRITE_TIMEOUT` | `10s` | Write response |
| `HTTP_IDLE_TIMEOUT` | `60s` | Keep-alive idle limit |
| `HTTP_SHUTDOWN_TIMEOUT` | `10s` | Drain active requests |
| `DATABASE_URL` | Required on host; derived in Compose | PostgreSQL URL |
| `DATABASE_TIMEOUT` | `3s` | Connect, readiness and repository-operation deadline |
| `JWT_SECRET` | Required, nonblank, at least 32 bytes | HS256 signing secret |
| `JWT_ACCESS_TTL` | `15m`, at least `1s` | Access token lifetime |
| `REDIS_ADDR` | Unset disables on host; `redis:6379` in Compose | Optional cache host:port |
| `REDIS_PASSWORD` | Empty | Client password; server authentication provisioned separately |
| `REDIS_DB` | `0`, allowed 0-15 | Standalone Redis database |
| `CACHE_TTL` | `1m`, at least `1ms` | Detail entry lifetime |
| `REDIS_DIAL_TIMEOUT` | `200ms`, at least `1ms` | Connection timeout |
| `REDIS_OPERATION_TIMEOUT` | `100ms`, at least `1ms` | Redis operation/socket/pool wait limit |
| `API_PORT` | `8080` | Compose host API port |
| `POSTGRES_PORT` | `5432` | Compose host database port |
| `POSTGRES_DB`, `POSTGRES_USER`, `POSTGRES_PASSWORD` | Required in Compose | Database initialization settings |
| `API_STOP_GRACE_PERIOD` | `30s` | Docker stop deadline; keep above HTTP shutdown timeout |

PostgreSQL initialization variables only affect a new, empty volume. Changing
`.env` does not change existing database credentials. Do not delete data to fix
credentials. Recreate containers after changing settings; `restart` does not
reload environment variables. The API runs as non-root UID/GID 10001.

## HTTP processing deadline

Each request receives one context deadline when it enters the global middleware
after its headers have been read. `HTTP_REQUEST_TIMEOUT` defaults to `5s`, must
be positive, and must be strictly less than `HTTP_WRITE_TIMEOUT`. The same
deadline flows through the handler, service, PostgreSQL and Redis calls. An
earlier parent deadline remains in effect; database and Redis operation limits
may shorten the remaining budget but cannot reset or extend it.

If this request context expires before the response starts, the next response
write or handler return produces JSON 503 with code `request_timeout`, message
`Request processing timed out`, empty `details`, `Cache-Control: no-store`, and
the request ID. Headers prepared for an abandoned response are cleared. If a
response has already started, the server aborts it rather than appending a JSON
error to a partial body. Client cancellation is distinct from a server deadline.
A database operation's shorter deadline still uses the existing internal-error
response if the HTTP context remains active; readiness failures remain 503
`not_ready` unless the shared request context has expired.

This is cooperative cancellation: code must observe the context to stop work.
The middleware does not launch a background handler or buffer responses, and it
cannot forcibly interrupt bcrypt, a blocked body read, or code that ignores the
context. Its timeout response is produced at the next write or handler return,
not at a strict wall-clock instant. Header/read/write/idle socket timeouts remain
separate protections. In particular, the write timeout limits network I/O; it
does not itself cancel downstream application work. A timed-out mutation may
already have committed, so inspect resource state before retrying it.

## Authentication and authorization

Registration normalizes email and hashes a 12-72 **byte** password with bcrypt.
Display names are trimmed, 1-100 characters without controls. Login accepts a
1-72 byte password and returns an access token with `token_type` and `expires_in`.
Unknown emails and wrong passwords return the same 401. Tokens contain only
`sub` (decimal user ID), `iat`, and `exp`; signatures, HS256 method and timestamps
are verified. Login and current-user responses use `Cache-Control: no-store`.

Login and registration share a budget of **10 POST attempts per client IP in a
one-minute fixed window**, starting with that IP's first allowed attempt.
Successful requests and invalid bodies use the same budget. Excess attempts
return JSON `429` with code `rate_limited`, message `Too many authentication
attempts`, `Cache-Control: no-store`, and `Retry-After` in whole seconds rounded
up (at least 1). Rejected requests do not extend the window.

The limiter uses the direct connection's `RemoteAddr` IP, ignoring ports and
normalizing IPv4-mapped IPv6. `Forwarded`, `X-Forwarded-For` and `X-Real-IP` are
not trusted. It keeps at most 4096 IP entries in memory; when full, new IPs receive
429 until an entry expires, while existing IP budgets are preserved. State is
local to each API process and resets on restart. Users behind one NAT or reverse
proxy share a budget. Bursts across a window boundary can exceed 10 attempts in a
rolling minute. This is basic authentication abuse protection, not a global
denial-of-service defense. There are no additional environment settings or
dependencies.

Authentication establishes identity; services separately verify that the user
still exists and has the necessary current project membership. Passwords/hashes
are never returned. A token belonging to a deleted user is rejected.

| Operation | Permission |
| --- | --- |
| Create project | Existing authenticated user; becomes owner atomically |
| List/read project, members, tasks, comments | Current member; project list contains only caller's projects |
| Update / delete project | Owner or admin / owner only |
| Manage members | Owner manages admins/members; admin manages ordinary members only |
| Create task or comment | Any current project member |
| Update/delete task | Owner, admin or task creator; assignee may update status only |
| Update/delete comment | Owner, admin or comment author |

Owner membership cannot be changed/removed. Assignments must refer to a current
member of the same project. Removing a member preserves historical assignments
and authorship but rejects subsequent requests from that user. Project, task
and comment IDs must match their parent scope.

New tasks start in `todo`. Transitions are `todo -> in_progress`,
`in_progress -> todo/done`, and `done -> in_progress`; repeating status is allowed.
Priority is `low`, `medium` (default), or `high`.

## API examples

Use a fresh example email if it is already registered. These requests create
local development data; the smoke scripts below provide disposable equivalents.

```powershell
$base = 'http://localhost:8080'
$email = 'alex-' + [Guid]::NewGuid().ToString('N') + '@example.com'
$registration = @{ email=$email; password='local-example-password-123'; display_name='Alex' } | ConvertTo-Json
$user = Invoke-RestMethod -Method Post -Uri "$base/api/v1/auth/register" -ContentType 'application/json' -Body $registration
$credentials = @{ email=$email; password='local-example-password-123' } | ConvertTo-Json
$login = Invoke-RestMethod -Method Post -Uri "$base/api/v1/auth/login" -ContentType 'application/json' -Body $credentials
$headers = @{ Authorization='Bearer ' + $login.data.access_token }
Invoke-RestMethod -Uri "$base/api/v1/users/me" -Headers $headers
$projectBody = @{ name='Portfolio'; description='TaskFlow release' } | ConvertTo-Json
$project = Invoke-RestMethod -Method Post -Uri "$base/api/v1/projects" -Headers $headers -ContentType 'application/json' -Body $projectBody
$tasks = "$base/api/v1/projects/$($project.data.id)/tasks"
$taskBody = @{ title='Review documentation'; priority='high'; due_date='2026-10-20T10:00:00Z' } | ConvertTo-Json
$task = Invoke-RestMethod -Method Post -Uri $tasks -Headers $headers -ContentType 'application/json' -Body $taskBody
Invoke-RestMethod -Uri "${tasks}?status=todo&priority=high&search=documentation&sort=due_date&order=asc&page=1&page_size=20" -Headers $headers
```

Success envelopes are `{"data":{...}}`; collections are
`{"data":[],"meta":{"page":1,"page_size":20,"total":0}}`. Errors use
`{"error":{"code":"validation_error","message":"Request validation failed","details":{}}}`.
Deletes return 204 with an empty body. JSON responses use `application/json`.
Bodies are limited to 4096 bytes; unknown fields, malformed/trailing JSON are
rejected with 400. IDs must be positive int64. Times use UTC/RFC3339.

Task-list filters: `status`, `priority`, `assignee_id`, `creator_id`, `due_from`,
`due_to`, `search`, `sort`, `order`, `page`, `page_size`. Filters combine with AND;
search matches the combined `title + " " + description` case-insensitively,
including across that boundary; `%`, `_` and `\` are literal. Due bounds are
inclusive RFC3339 timestamps; encode timezone
`+` as `%2B`. Sort fields: `id`, `created_at`, `updated_at`, `due_date`, `priority`,
`status`, `title`; order `asc` or `desc`. Default: `id asc`, page 1, size 20
(max 100). Ties use ID in the same direction, null dates sort last.
Other collection endpoints accept only `page` and `page_size`. Empty, repeated
or unsupported list parameters are rejected. `PATCH` with null clears only
`assignee_id`/`due_date`; omit them to preserve values.

Full route/schema/validation/status-code reference: [OpenAPI](docs/openapi.yaml).
There is no runtime Swagger UI. Validate the document and its examples:

```powershell
python -m venv .tmp/docs-venv
./.tmp/docs-venv/Scripts/python.exe -m pip install -r scripts/requirements-docs.txt
./.tmp/docs-venv/Scripts/python.exe scripts/validate-openapi.py
```

The validator is a development dependency only.

## Database and migrations

The five application tables are `users`, `projects`, `project_members`, `tasks`
and `comments`. See the [ER diagram](docs/architecture.md#database-model).

| Version | Tables added | Down migration removes |
| --- | --- | --- |
| 1 | users | Users |
| 2 | projects, project_members | Projects and memberships |
| 3 | tasks | Tasks |
| 4 | comments | Comments |

Apply with `docker compose run --rm migrate up`; inspect with
`docker compose run --rm migrate version`. Up is repeatable. Deliberate rollback
of one version uses `docker compose run --rm migrate down 1` and **deletes that
version's data**. Exercise rollback through `scripts/integration.ps1`, which
creates its own empty database. Do not edit shared applied migrations.

A dirty migration requires inspecting/repairing the actual schema before any
metadata correction. Never blindly use `force`, or delete a development volume
to repair migration state. Constraints, foreign keys, justified indexes and
transaction details are documented in [architecture](docs/architecture.md).

## Redis cache-aside behavior

Only single project/task detail reads are cached, after PostgreSQL-backed
authorization. Keys: `taskflow:v1:project:{projectID}` and
`taskflow:v1:project:{projectID}:task:{taskID}`. A hit skips the entity query;
a miss reads PostgreSQL and fills the cache for `CACHE_TTL`. Updates/deletes
invalidate the affected detail after commit. PostgreSQL remains authoritative.

Redis errors, timeouts and malformed values fall back to PostgreSQL. A failed
cache invalidation cannot undo a successful database write; stale data may remain
until TTL. An in-flight read can refill an old value after invalidation; its TTL
bounds that stale value. There is no strict cache consistency, stampede prevention
or distributed locking. Lists, users, credentials, JWTs, memberships, comments,
HTTP envelopes and authorization decisions are not cached. See the
[sequence diagram](docs/architecture.md#authenticated-task-detail-read).

## Verification and smoke tests

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File ./scripts/check-go.ps1
powershell -NoProfile -ExecutionPolicy Bypass -File ./scripts/integration.ps1
powershell -NoProfile -ExecutionPolicy Bypass -File ./scripts/auth-smoke.ps1
powershell -NoProfile -ExecutionPolicy Bypass -File ./scripts/projects-smoke.ps1
powershell -NoProfile -ExecutionPolicy Bypass -File ./scripts/tasks-smoke.ps1
powershell -NoProfile -ExecutionPolicy Bypass -File ./scripts/cache-smoke.ps1
```

Run smoke scripts sequentially. Each creates/removes only its own random Compose
project and restores process variables. Integration tests use random databases
and fixture-owned cache keys; they skip without their test environment variables.
Never use personal/production services. The [test guide](docs/testing.md) contains
the coverage map, ports and CI-equivalent commands. The cache smoke includes a
real Redis outage, startup with Redis down, recovery and disabled-cache mode.

The [CI workflow](.github/workflows/ci.yml) runs formatting verification, vet,
tests/build, Compose validation, migrations, PostgreSQL/Redis integration tests,
and OpenAPI validation on PRs and pushes to `main`. It does not deploy or publish.

## Operations and troubleshooting

```powershell
docker compose ps -a
docker compose logs --tail 50 api postgres redis
docker compose stop api
docker compose up -d --wait api
docker compose down
```

`down` preserves PostgreSQL data. **`down --volumes` deletes it** and must not be
used on a development database that contains data you need. The isolated test
scripts use it only on their generated projects. Structured API logs include
request IDs, method/path, status, size and duration; bodies, query strings,
passwords, tokens, authorization headers and raw repository errors are excluded.
Panic values are excluded; call stacks are logged. Recovery aborts responses
that already started. Graceful shutdown drains requests within the configured
deadline; exceeding it forces closure and a nonzero exit.

| Symptom | Check / action |
| --- | --- |
| Docker pipe/daemon error | Start Docker Desktop in Linux-container mode |
| Port already allocated | Change `API_PORT` / `POSTGRES_PORT`, then recreate affected containers |
| Compose cannot interpolate settings | Create `.env`; remove unintended process overrides; run `config --quiet` |
| `/health` works, `/ready` is 503 | PostgreSQL health, URL/hostname/credentials and deadline |
| `/ready` is 200, resource requests fail | Apply migrations; readiness checks connectivity only |
| 401 / 403 | Valid unexpired Bearer JWT / current membership and role |
| 429 on login or registration | Wait for `Retry-After`; both endpoints and clients sharing one direct IP use the same budget |
| 503 with `request_timeout` | The shared HTTP processing deadline expired; check dependency latency and timeout settings; inspect state before retrying a mutation |
| Redis unavailable | Detail reads/writes continue via PostgreSQL; inspect safe warnings |
| Integration tests skip | Use the isolated integration runner; a unit-only pass is insufficient |
| Integration timeout on a loaded machine | Avoid simultaneous builds; runner serializes packages with `-p 1` |
| Dirty migration | Inspect schema and failure; repair deliberately before changing metadata |

## Security, limitations and license

Keep `.env` ignored. Use strong non-example secrets, least-privilege database
credentials, TLS and a trusted deployment boundary before exposing the API.
Compose is a loopback development environment; its initialized database user is
a superuser and its internal disposable Redis has no authentication. Request
limits and timeouts exist; email verification, password reset,
refresh tokens and token revocation are outside v1 scope. There is no owner
transfer, invitation system or user-list endpoint.

Pagination uses offsets: pages requested separately may shift after concurrent
writes. Search is a project-scoped scan, not indexed full-text search. Project
writes serialize on a project row lock. Cached detail values can be stale within
their documented TTL bounds. There is no frontend, cloud deployment or metrics
stack. These limitations are deliberate; see [the roadmap](docs/ROADMAP.md).

No software license has been selected; this repository currently grants no
explicit open-source license.
