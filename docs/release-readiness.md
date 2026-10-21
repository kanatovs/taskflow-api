# TaskFlow v1 release-readiness review

Review date: 2026-10-15. Scope: the v1.0.0 release candidate's backend behavior
and release tooling. Results below were recorded during that review; they do not
assess subsequent changes on the current branch. This is a code/configuration
review with automated evidence, not a penetration test or certification.

## Confirmed findings and changes

| Finding | Resolution | Evidence |
| --- | --- | --- |
| Redis 8.2.2 omitted subsequent security patches | Updated within the existing 8.2 line to 8.2.10; pinned exact image digest | Official Redis release notes and successful full PostgreSQL/Redis suite plus cache/outage smoke |
| Container tags could resolve to different content later | Pinned PostgreSQL, Redis, migrate, Go builder and Alpine runtime by registry digest | Compose validation and real Docker API build/start |
| API documentation described search as separate title/description predicates | Documented actual literal search over `title + ' ' + description`, including cross-boundary matches | Added HTTP integration case `Alpha Needle` |
| Create schemas understated accepted null defaults; nullable enum values needed explicit null entries | Corrected OpenAPI create/PATCH schemas, added a nullable create example and real HTTP default test | OpenAPI validation now checks 41 examples, including RFC3339 formats |
| Python audit tooling could leave bytecode in source directories | Ignored `__pycache__` and `*.pyc` | Git status / tracked-file hygiene scan |

The Redis update is a security patch to an existing dependency, not a new
technology or business capability. The [8.2.10 release](https://github.com/redis/redis/releases/tag/8.2.10)
and [8.2 maintenance notes](https://redis.io/docs/latest/operate/oss_and_stack/stack-with-enterprise/release-notes/redisce/redisos-8.2-release-notes/)
document upstream security fixes. No application production library was added,
replaced or upgraded, and no business endpoint/schema migration was added.

## Code review coverage

| Area | Reviewed behavior |
| --- | --- |
| Authentication | bcrypt default cost, byte limits, normalized email, dummy comparison for missing users, safe identical credential errors |
| JWT | HS256 allowlist and method check, required expiration/issued-at, canonical positive int64 subject, environment secret length; no sensitive claims |
| HTTP | 4096-byte JSON limits, unknown/malformed/trailing JSON, ID/query validation, method/Allow handling, safe error envelope, JSON content type |
| Logging / panics | Generated request IDs, actor/project/task fields, no request/response bodies or authorization headers, redacted repository error categories; stack without panic value |
| Permissions | Current PostgreSQL user/membership checks, role matrix, owner protection, scoped task/comment IDs, comment authorship, removed-member denial before Redis |
| SQL | Parameters for external values; sort expressions/direction allowlisted; explicit column lists; rows closed and rows.Err checked |
| Transactions | Atomic project/owner creation; consistent project-first locks; permissions/assignment/status rechecked while locked; deferred bounded rollback, explicit commit |
| Context / resources | Request contexts propagated; PostgreSQL/Redis deadlines; lazy clients; SQL pool and Redis close after bounded HTTP drain |
| Cache | Entity-only values, scoped keys and identity validation, positive TTL, miss/hit, malformed cleanup, invalidation after success, safe failure fallback |
| Reliability | Shutdown joins serving goroutine, forced closure on deadline, safe startup failures, health/readiness split, cancellation and role-race tests |

The authenticated data path contains no mutable global application state. Shared
pgx/Redis clients are used through their concurrency-safe APIs. Unit/HTTP race
verification on Windows with GCC 14.2.0: **passed**, using
`CGO_ENABLED=1` and `go test -race -p 1 -count=1 ./...`. This race run does
not set integration service variables; database/Redis integration tests run
separately with real isolated services.

## Dependency review

Executed `go mod tidy`, `go mod verify`, `go list -m -u all`, and
`go run golang.org/x/vuln/cmd/govulncheck@v1.1.4 -show verbose ./...`.
Tidy left `go.mod`/`go.sum` unchanged; module checksums verified successfully.

Govulncheck with Go 1.26.9 found **zero reachable vulnerable symbols and zero
vulnerable imported packages**. It reported one module-only advisory:
[GO-2026-5932](https://pkg.go.dev/vuln/GO-2026-5932), the unmaintained/unsafe
`golang.org/x/crypto/openpgp` package, with no fixed version. TaskFlow imports
`golang.org/x/crypto/bcrypt`, not OpenPGP. The advisory is retained here instead
of claiming the complete module is advisory-free or replacing bcrypt.

Available non-security updates included Chi 5.5.0 (installed 5.3.2) and puddle
2.2.3 (installed 2.2.2). No reachable issue justified unrelated library upgrades.
The module graph also contains upstream test/tool dependencies; listing them
does not mean they are linked into the API. Development validators are separate
from Go runtime dependencies.

## History and repository hygiene

`python scripts/audit_repository.py` scans tracked working files and every
reachable Git blob in a full clone locally. It checks private-key headers,
AWS/GitHub/Slack and service API token shapes, signed JWT shapes, historical
`.env` paths, tracked environment/build/IDE artifacts, and files larger than
1 MiB. Matches
are reported only by rule/path/object ID, without credential values. Ignored
local environment files are never read.

No configured credential patterns, tracked `.env`, unexpected tracked binaries,
IDE files or oversized files were found. `.env` and local tooling are ignored.
`.gitattributes` defines LF source/docs and CRLF PowerShell working files. The
scanner's supported credential shapes/placeholders have two passing unit tests.

Gitleaks did **not** run. The local script is a limited pattern-based scanner.
It cannot detect arbitrary passwords, every provider's
token format, revoked credentials, or secrets outside reachable history; a clean
scan is not mathematical proof of their absence. Existing example passwords,
dummy bcrypt hashes and test JWT secrets are public fixtures, not real credentials.

## Verification and limits

- Formatting, `go vet ./...`, `go test ./...`, `go build ./...` and whitespace
  checks passed.
- The uncached full suite passed with real disposable PostgreSQL and Redis
  8.2.10, including nullable create defaults and boundary-spanning search.
- Cache smoke passed hit/miss, TTL, malformed data, authorization, invalidation,
  real Redis outage, reads/writes/login/registration, API startup during outage,
  recovery, disabled cache and graceful shutdown.
- OpenAPI structure, local references and 41 examples passed validation.
- All five external base/service images are pinned; API runs as UID/GID 10001.
  Docker build context excludes `.git`, `.env`, local tools and credentials.

Operational note: an uncached run timed out in three PostgreSQL tests while
another Go tool was compiling. The integration runner uses `-p 1` to reduce
package contention; its repeated run passed. The successful race check used
native Windows with CGO and GCC 14.2.0; it does not establish a Linux race result.

## Limitations at the time of review

The v1.0.0 release candidate had no rate limiter, password-reset/email-verification
flow, refresh tokens or explicit token revocation. This is a historical finding;
see the [README](../README.md) and [changelog](../CHANGELOG.md) for current behavior.

Other limits included privileged public development credentials, no supplied
TLS/deployment hardening, Redis staleness bounded by TTL, serialized project
writes, shifting offset pages under concurrent writes and no full-text index.
No container-wide CVE scanner or penetration test was run. Digest pins require
deliberate future security updates. Local checks did not establish a
GitHub-hosted CI result.
