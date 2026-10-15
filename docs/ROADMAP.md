# TaskFlow API Roadmap

This file is the source of truth for implementation order.

Rules:

- Work on one task at a time.
- Do not implement later tasks early.
- Every behavior-changing task includes relevant tests.
- Mark a task complete only in the commit that completes it.
- Keep main runnable.
- Preserve meaningful commits when merging feature branches.

## Epic 1 — Foundation

Branch: `feature/foundation`

- [x] TF-001 — Initialize the Go module and minimal repository structure.
  Commit: `chore: initialize Go module and application layout`

- [x] TF-002 — Add the HTTP server and public health endpoint.
  Endpoint: `GET /health`
  Commit: `feat: add HTTP server and health endpoint`

- [x] TF-003 — Add environment configuration loading and startup validation.
  Commit: `feat(config): load and validate environment configuration`

- [x] TF-004 — Add JSON response helpers and consistent API error handling.
  Commit: `feat(api): add JSON response and error handling`

- [x] TF-005 — Add structured logging, panic recovery, and graceful shutdown.
  Commit: `feat: add structured logging and graceful shutdown`

- [x] TF-006 — Add Dockerfile, Docker Compose, PostgreSQL, and `.env.example`.
  Commit: `chore(docker): add local development environment`

Acceptance:

- API starts locally.
- `GET /health` returns successful JSON.
- Configuration errors fail fast.
- Application shuts down gracefully.
- PostgreSQL runs through Docker Compose.
- Epic is reviewed and merged into main without squashing meaningful commits.

## Epic 2 — PostgreSQL and Authentication

Branch: `feature/authentication`

- [x] TF-007 — Add pgx PostgreSQL pool and database readiness check.
  Commit: `feat(db): add PostgreSQL connection pool`

- [x] TF-008 — Add user migrations, domain model, and PostgreSQL repository.
  Commit: `feat(users): add users schema and repository`

- [x] TF-009 — Implement registration with validation and bcrypt.
  Endpoint: `POST /api/v1/auth/register`
  Commit: `feat(auth): implement user registration`

- [x] TF-010 — Implement login and JWT access-token generation.
  Endpoint: `POST /api/v1/auth/login`
  Commit: `feat(auth): implement login and JWT issuance`

- [x] TF-011 — Add authentication middleware and current-user endpoint.
  Endpoint: `GET /api/v1/users/me`
  Commit: `feat(auth): add authentication middleware`

- [x] TF-012 — Complete authentication handler, service, and repository tests.
  Commit: `test(auth): cover authentication flow`

Acceptance:

- Users can register and log in.
- Duplicate emails are rejected.
- Passwords are stored only as hashes.
- Protected endpoints reject missing, invalid, and expired tokens.
- Authentication tests pass.

## Epic 3 — Project Management

Branch: `feature/project-management`

- [x] TF-013 — Add project and membership migrations, models, and repositories.
  Commit: `feat(projects): add project membership schema`

- [x] TF-014 — Implement project CRUD.
  Endpoints:
  - `POST /api/v1/projects`
  - `GET /api/v1/projects`
  - `GET /api/v1/projects/{projectID}`
  - `PATCH /api/v1/projects/{projectID}`
  - `DELETE /api/v1/projects/{projectID}`
  Commit: `feat(projects): implement project CRUD`

- [x] TF-015 — Implement membership and role management.
  Endpoints:
  - `GET /api/v1/projects/{projectID}/members`
  - `POST /api/v1/projects/{projectID}/members`
  - `PATCH /api/v1/projects/{projectID}/members/{userID}`
  - `DELETE /api/v1/projects/{projectID}/members/{userID}`
  Commit: `feat(projects): add membership and role management`

- [x] TF-016 — Complete project authorization and permission tests.
  Commit: `test(projects): cover project permissions`

Acceptance:

- Owners manage projects.
- Owners/admins manage members according to documented rules.
- Forbidden operations and cross-project access are rejected.
- Authorization tests pass.

## Epic 4 — Task Management

Branch: `feature/task-management`

- [x] TF-017 — Add task migrations, model, and PostgreSQL repository.
  Commit: `feat(tasks): add task schema and repository`

- [x] TF-018 — Implement task CRUD.
  Endpoints:
  - `POST /api/v1/projects/{projectID}/tasks`
  - `GET /api/v1/projects/{projectID}/tasks`
  - `GET /api/v1/projects/{projectID}/tasks/{taskID}`
  - `PATCH /api/v1/projects/{projectID}/tasks/{taskID}`
  - `DELETE /api/v1/projects/{projectID}/tasks/{taskID}`
  Commit: `feat(tasks): implement task CRUD`

- [x] TF-019 — Add assignment rules, status transitions, and authorization.
  Commit: `feat(tasks): add assignment and authorization rules`

- [x] TF-020 — Add task comments.
  Endpoints:
  - `POST /api/v1/projects/{projectID}/tasks/{taskID}/comments`
  - `GET /api/v1/projects/{projectID}/tasks/{taskID}/comments`
  - `PATCH /api/v1/projects/{projectID}/tasks/{taskID}/comments/{commentID}`
  - `DELETE /api/v1/projects/{projectID}/tasks/{taskID}/comments/{commentID}`
  Commit: `feat(comments): implement task comments`

- [x] TF-021 — Add filtering, search, sorting, and pagination.
  Filters: status, priority, assignee, creator, due date, text search.
  Commit: `feat(tasks): add filtering search and pagination`

- [x] TF-022 — Complete task and comment tests.
  Commit: `test(tasks): cover task and comment flows`

Acceptance:

- Project members can perform permitted operations.
- Invalid assignments and mixed project/task IDs are rejected.
- Filtering and pagination are stable.
- Task/comment tests pass.

## Epic 5 — Redis Cache

Branch: `feature/redis-cache`

- [x] TF-023 — Add Redis configuration, lifecycle, and read-through caching.
  Commit: `feat(cache): add Redis read-through caching`

- [x] TF-024 — Add invalidation, failure fallback, and cache tests.
  Commit: `test(cache): cover invalidation and failure fallback`

Acceptance:

- PostgreSQL remains source of truth.
- Cache hits reduce database reads.
- Mutations invalidate affected entries.
- Correctness remains when Redis is unavailable.

## Epic 6 — Documentation, Integration Tests, and CI

Branches as appropriate:

- `test/integration-suite`
- `docs/api-documentation`
- `ci/github-actions`

- [x] TF-025 — Add and validate OpenAPI specification and examples.
  File: `docs/openapi.yaml`
  Commit: `docs(api): add OpenAPI specification`

- [x] TF-026 — Add PostgreSQL and Redis integration tests.
  Commit: `test(integration): cover persistence and cache`

- [x] TF-027 — Add GitHub Actions for formatting, vetting, tests, and build.
  Commit: `ci: add automated Go checks`

- [x] TF-028 — Complete README, setup, architecture, ER diagram, and examples.
  Commit: `docs: complete project documentation`

- [x] TF-029 — Perform final security, reliability, and dependency review.
  Commit: `refactor: complete release readiness review`

- [ ] TF-030 — Prepare the first stable release.
  Commit: `chore: prepare v1 release`

Final acceptance:

- A new developer can start from README.
- Docker Compose starts required services.
- Migrations work from a clean database.
- Unit/integration tests and GitHub Actions pass.
- OpenAPI matches actual behavior.
- No secrets exist in Git history.
- Main is stable and runnable.

## Future repositories

Do not implement them from this repository.

### LinkPulse

Repository: `github.com/kanatovs/linkpulse`

Focus: URL shortening, redirects, PostgreSQL, Redis, TTL, rate limiting,
background analytics, goroutines, channels, worker pools, concurrency safety,
Docker, tests, and CI.

### NotifyHub

Repository: `github.com/kanatovs/notifyhub`

Focus: notification API, PostgreSQL, RabbitMQ, producer/consumer,
workers, retries, dead-letter handling, status tracking, graceful shutdown,
Docker, tests, and CI.

TaskFlow must reach a stable release before LinkPulse begins.
LinkPulse must reach a stable release before NotifyHub begins.
