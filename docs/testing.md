# Testing TaskFlow

## Unit tests

From the repository root:

```powershell
go test ./...
go vet ./...
go build ./...
```

Without `TEST_POSTGRES_URL` / `TEST_REDIS_ADDR`, tests requiring those services
explicitly skip. A green unit-only run does not prove integration coverage.

## Authentication rate limit checks

When changing the authentication limiter, run the handler tests, then the
isolated integration runner below because login and registration behavior is
part of the HTTP contract:

```powershell
go test -count=1 ./internal/handler
```

Verify these cases without real-time sleeps by controlling the limiter clock
in unit tests:

- Login and registration share 10 attempts per direct client IP in a one-minute
  fixed window. Successful requests and invalid input both consume the budget.
- The next attempt returns JSON 429 with `rate_limited`, the documented message,
  `Cache-Control: no-store`, and a positive `Retry-After` rounded up to seconds.
  A rejected request does not reach authentication work or extend the window.
- The budget resets at the exact expiry boundary; distinct client IPs have
  independent budgets. Ports and IPv4-mapped IPv6 cannot create extra budgets.
- Changing `Forwarded`, `X-Forwarded-For` or `X-Real-IP` does not change identity.
  Unrelated routes and unsupported methods keep their existing behavior.
- Concurrent requests cannot admit more than 10 attempts in one window.
- The tracked-IP count never exceeds 4096. At capacity, unknown IPs receive 429,
  existing budgets survive, and expired entries are reclaimed lazily.

The fixed-window policy can admit more than 10 attempts in a rolling minute near
a boundary; do not test it as a rolling-minute limit. Run real HTTP integration
tests with fresh router instances so limiter state is not shared across unrelated
tests.

## HTTP processing deadline checks

When changing the shared HTTP deadline, run handler and configuration tests,
then the isolated integration runner below:

```powershell
go test -count=1 ./internal/config ./internal/handler
```

The deadline coverage should verify:

- `HTTP_REQUEST_TIMEOUT` defaults to `5s`; malformed, empty, zero and negative
  values are rejected, as are values greater than or equal to
  `HTTP_WRITE_TIMEOUT`.
- Successful responses retain their existing status, body and headers. The
  handler receives a deadline, a shorter parent deadline is preserved, and the
  middleware cancels its child context when the handler finishes.
- Waiting for the request context to expire before writing or returning yields
  exactly one JSON 503 `request_timeout`, the documented message, empty details,
  `Cache-Control: no-store`, and the same `X-Request-ID`. Headers intended for the
  abandoned response are removed, and subsequent writes return
  `http.ErrHandlerTimeout` without appending output.
- All routes share the same deadline mechanism. Client cancellation is not
  reclassified as a server deadline, and a dependency's shorter local deadline
  preserves the existing error mapping while the request context remains live.
- If expiry is observed after a response starts, handling aborts with
  `http.ErrAbortHandler`; no JSON is appended to a partial response.
- A real HTTP current-user request whose PostgreSQL read is blocked by an
  exclusive table lock returns 503 `request_timeout` when the shared HTTP budget
  expires first. With a shorter database operation timeout and a live HTTP
  context, the same blocked read returns 500 `internal_error`. After releasing
  the lock, a new current-user request succeeds.

Use context completion or explicit synchronization for deadline tests rather
than relying on a handler sleep just shorter than its deadline. Run integration
checks against disposable PostgreSQL/Redis services. These checks establish
cooperative cancellation; they cannot establish a strict response-time bound
for bcrypt, blocked body reads, or handlers that ignore their contexts.

## Redis parent deadline check

`TestRedisContextDeadline` uses a TCP server that accepts a connection but never
answers the Redis handshake. Its operation timeout exceeds the test's one-second
response bound, so the shorter parent deadline must bound the wait. The test
requires a context or network timeout and waits for `ctx.Done()` before checking
`ctx.Err()`: the socket deadline can fire before the context timer runs.

## Isolated integration run

Requires Go, Docker with Linux containers, and Windows PowerShell 5.1 or newer:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File ./scripts/integration.ps1
```

The runner creates a random `taskflow-integration-<hex>` Compose project. It
publishes PostgreSQL on loopback port 15441 and Redis on 16381. Override occupied
ports with `-PostgresPort 15442 -RedisPort 16382`. Only public example credentials
are used. No `.env` is read: `.env.example` and explicit process overrides supply
the test configuration. Previous process environment values are restored.

The runner verifies the actual golang-migrate CLI against an empty disposable
database: up to clean version 4, repeated up, all four down migrations, up again,
and rejection of a simulated `dirty=true` version. Because the schema itself is
unchanged by the simulation, `force 4` safely resets this test database's metadata.
**Never copy that recovery step into a real database without checking its schema.**

Next, `go test -p 1 -count=1 ./...` exercises real PostgreSQL and Redis. Packages
run sequentially to reduce database/CPU contention on small CI runners; tests
remain independent of execution order. PostgreSQL
fixtures create random `taskflow_test_*` databases and drop only those databases.
The Go helper applies the SQL files directly; CLI migration metadata is covered
separately by the runner. Redis tests delete only fixture-owned keys and never
flush a database. Shared integration fixtures do not use `t.Parallel` because the
public cache-key namespace is intentionally identical to the application.

Cleanup runs in `finally`, checks the generated project-name prefix, and removes
only that project's containers/network/volume. Development resources are not
selected. Failed native commands throw, including failed cleanup.

## Coverage map

| Area | Tests / script | Main cases |
| --- | --- | --- |
| Migrations | `scripts/integration.ps1` | Clean 0/4/0/4, repeated up, dirty rejection, five tables |
| Constraints / rollback | `internal/repository/postgres/*_test.go` | Foreign keys, uniqueness, validation constraints, atomic project/owner creation, cancellation |
| Authentication | `internal/handler/auth*_integration_test.go` | Register, duplicate email, login, safe credential errors, current user, invalid/expired JWT |
| HTTP processing deadline | `internal/config/config_test.go`, `internal/handler/*timeout*_test.go` | Configuration limits, shared/parent deadlines, context cleanup, client cancellation, safe 503, partial-response abort, real PostgreSQL cancellation |
| Projects / membership | `internal/handler/project*_integration_test.go` | CRUD, owner membership, role matrix, cross-project access, concurrent member changes |
| Tasks | `internal/handler/task*_integration_test.go` | CRUD, assignment, status transitions, nullable PATCH, permissions, mixed IDs, filters/search/sort/pages |
| Comments | `internal/handler/comment_integration_test.go` | CRUD, author/owner/admin rules, mixed task/comment IDs, cascades |
| Redis client | `internal/platform/cache/redis_test.go` | Real miss/hit, explicit TTL and expiry, delete, deadline and unavailable server |
| Cache HTTP | `internal/handler/cache_integration_test.go` | Real cache, malformed payload, invalidation, removed members, no Redis access before authorization, real closed-client fallback with PostgreSQL |
| Cache policy | `internal/repository/cache/*_test.go` | Entity-read counts on miss/hit, no negative cache, safe logs, cancellation, invalidation failure |
| Container outage | `scripts/cache-smoke.ps1` | Stop real Redis, read/write/login fallback, API startup during outage, recovery, disabled cache |

## Continuous integration

`.github/workflows/ci.yml` runs on pull requests and pushes to `main`. One Ubuntu
job uses PowerShell 7 to run the same Go checks and isolated integration runner
as local development, validates Compose and OpenAPI, and cleans up even after a
failed integration step. Permissions are limited to reading repository contents;
superseded runs are cancelled. It does not publish artifacts or deploy anything.

The reusable Go check does not format files in place:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File ./scripts/check-go.ps1
go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12 .github/workflows/ci.yml
```

Hosted workflow results become available after a push and PR; local checks do
not establish that the hosted workflow has already passed.

## Container smoke tests

Each script creates and cleans its own random Compose project. Run sequentially:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File ./scripts/auth-smoke.ps1
powershell -NoProfile -ExecutionPolicy Bypass -File ./scripts/projects-smoke.ps1
powershell -NoProfile -ExecutionPolicy Bypass -File ./scripts/tasks-smoke.ps1
powershell -NoProfile -ExecutionPolicy Bypass -File ./scripts/cache-smoke.ps1
```

These use loopback ports documented in each script's `param` block. The cache
smoke script also runs the uncached complete Go suite with real dependencies.
Both database-heavy runners use `-p 1` to reduce package contention.
Never point integration variables at personal or production services, run
`FLUSHDB`, or remove the development PostgreSQL volume as a test cleanup step.
