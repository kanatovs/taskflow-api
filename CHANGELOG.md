# Changelog

Notable changes are listed in a Keep a Changelog-style format.

## [Unreleased]

### Security

- Limit login and registration to a shared budget of 10 attempts per direct
  client IP in a one-minute fixed window. Excess attempts receive JSON 429 with
  `Retry-After`; the in-memory client table is bounded to 4096 entries.

## [1.0.0] - 2026-10-15

Prepared release contents. This date records preparation; the Git tag and GitHub
release are intentionally pending PR merge into `main`.

### Added

- User registration with normalized unique email and bcrypt password hashing;
  login with HS256 JWT access tokens and authenticated current-user lookup.
- Project CRUD, atomic owner membership, owner/admin/member role management,
  and current-membership authorization with locked write-time rechecks.
- Project-scoped task CRUD, assignment rules, status transitions, nullable
  assignee/due-date updates, priorities, and task comments with author permissions.
- Task filters, case-insensitive literal search, stable sorting and pagination.
- PostgreSQL repositories with parameterized SQL, deadlines and four reversible
  schema migrations for users, projects/memberships, tasks and comments.
- Optional Redis project/task detail caching, TTLs, invalidation, malformed-value
  handling and PostgreSQL fallback, including API startup during Redis outages.
- HTTP health/readiness, bounded JSON requests, consistent JSON errors, request
  IDs, structured logs, panic recovery and graceful shutdown.
- Non-root multi-stage Docker API build and local Compose services.
- Unit/HTTP/repository/integration tests, isolated migration checks including
  dirty-state detection, and authentication/project/task/cache smoke runners.
- GitHub Actions formatting, vet, tests, build, Compose, migration/integration and
  OpenAPI checks; a validated specification covering 23 HTTP operations.
- Setup/API examples, architecture and ER/cache diagrams, test guide and release
  readiness/dependency review.

### Security

- Redis updated to patched 8.2.10; all external base/service images pinned by
  digest. No runtime Go library additions in the final release epic.
- JWT method/expiration validation, input limits, scoped access checks, safe
  internal-error responses and omission of password/token data from normal logs.
- Local redacted credential-pattern/history and repository-hygiene audit tooling.

### Fixed

- Authentication smoke checks now capture expected native SQL errors correctly
  in Windows PowerShell 5.1 before evaluating their exit codes.
- Both database-heavy Go runners serialize packages to reduce contention and
  timeout flakes on small local/CI machines.

### Known limitations

- No refresh/revocation, password reset, email verification, invitations, owner
  transfer, rate limiter, frontend or deployment automation.
- Development Compose credentials are public/privileged; deployment requires
  appropriate secrets, access boundaries and TLS.
- Cache staleness is bounded by TTL after failed invalidation or an in-flight
  stale refill. Project writes serialize; offset pages can shift under writes;
  text search has no full-text index.
- No license has been selected. See the release-readiness review for audit limits
  and the unimported OpenPGP module advisory.
