# TaskFlow API Repository Instructions

## Product

TaskFlow is a REST API for collaborative project and task management.

Users can:

- register and sign in;
- create projects;
- manage project members and roles;
- create, assign, update, filter, and search tasks;
- comment on tasks.

This repository contains only the backend API. A frontend is outside current scope.

## Go module

Use:

github.com/kanatovs/taskflow-api

## Approved stack

- Go
- net/http
- Chi
- PostgreSQL
- pgx
- raw parameterized SQL
- golang-migrate
- JWT
- bcrypt
- Redis
- Docker and Docker Compose
- OpenAPI
- standard testing package and httptest
- GitHub Actions

Do not use an ORM, unnecessary microservices, RabbitMQ, Kafka, Kubernetes,
gRPC, GraphQL, or code generators without explicit approval.
Do not introduce Redis before the Redis roadmap epic.

## Architecture

Use:

HTTP Handler -> Service -> Repository -> PostgreSQL/Redis

Expected layout as packages become necessary:

cmd/
  api/
    main.go
internal/
  config/
  domain/
  handler/
  middleware/
  service/
  repository/
    postgres/
  platform/
    database/
    cache/
migrations/
docs/

Rules:

- main.go only composes dependencies and starts the application.
- Handlers parse HTTP input and produce HTTP output.
- Handlers never contain SQL.
- Services contain business rules.
- Repositories contain persistence logic.
- Database-specific types must not leak into HTTP handlers.
- Pass context.Context through handler, service, and repository layers.
- Do not store request contexts in long-lived structs.
- Prefer constructor-based dependency injection.
- Avoid mutable global state.
- Define interfaces close to consumers.
- Do not create interfaces or packages only for appearance.
- Do not create empty future abstractions.

## API conventions

Base path:

/api/v1

Use JSON.

Single resource:

{
  "data": {}
}

Collection:

{
  "data": [],
  "meta": {
    "page": 1,
    "page_size": 20,
    "total": 0
  }
}

Error:

{
  "error": {
    "code": "validation_error",
    "message": "Request validation failed",
    "details": {}
  }
}

Rules:

- Use correct methods and status codes.
- Set application/json.
- Reject malformed JSON and unknown fields where practical.
- Limit request body size.
- Do not expose SQL/internal errors.
- Use stable machine-readable error codes.
- Validate body, path, and query parameters.
- Default page size: 20. Maximum: 100.
- Use UTC internally and RFC3339 timestamps externally.
- Protect authenticated endpoints with Bearer JWT.
- Treat authentication and authorization separately.
- Verify project membership and role for project operations.

## Domain

### User

- id
- email
- password_hash
- display_name
- created_at
- updated_at

### Project

- id
- name
- description
- owner_id
- created_at
- updated_at

### ProjectMember

- project_id
- user_id
- role
- joined_at

Roles:

- owner
- admin
- member

### Task

- id
- project_id
- title
- description
- status
- priority
- assignee_id
- creator_id
- due_date
- created_at
- updated_at

Statuses:

- todo
- in_progress
- done

Priorities:

- low
- medium
- high

### Comment

- id
- task_id
- author_id
- body
- created_at
- updated_at

Do not expand the domain without roadmap and user approval.

## Authentication

- Hash passwords with bcrypt.
- Never store or return plaintext passwords.
- Never return password hashes.
- Load JWT secrets from environment variables.
- Validate signature and expiration.
- Do not place sensitive data in claims.
- Use a small documented claim set.
- Use 401 for missing/invalid authentication.
- Use 403 for authenticated users without permission.
- Do not add refresh tokens until explicitly approved.

## PostgreSQL

- Use up/down migrations.
- Do not edit shared applied migrations; create a new migration.
- Use TIMESTAMPTZ.
- Enforce key invariants with constraints and foreign keys.
- Add indexes only for justified query patterns.
- Use parameterized SQL only.
- Use transactions for atomic multi-write operations.
- Roll back failed transactions.
- Close rows and check rows.Err().
- Use pgx pooling and context timeouts.
- Do not use SELECT * in application queries.
- Do not place destructive seed data in migrations.

## Redis

Redis begins only in its roadmap epic.

- PostgreSQL remains source of truth.
- Cache failure must not corrupt persistent data.
- Document cache-key naming.
- Use explicit TTLs.
- Invalidate or update affected entries after mutations.
- Never cache authentication secrets or password data.
- Test hit, miss, invalidation, and Redis-unavailable fallback.

## Errors and logging

- Return expected errors instead of panicking.
- Wrap lower-level errors with context using %w.
- Use domain errors for not found, conflict, validation, unauthenticated, and forbidden.
- Map domain errors to HTTP responses consistently.
- Log internal detail server-side and return safe client messages.
- Use log/slog and structured fields.
- Never log secrets.
- Include request/user/project/task IDs when available.
- Implement graceful HTTP shutdown.

## Testing

Every behavior-changing task includes relevant tests.

Use:

- testing
- httptest
- small handwritten fakes where practical
- PostgreSQL integration tests for repositories/migrations
- Redis integration tests after Redis is introduced

Cover:

- success
- validation
- authentication
- authorization
- not found
- conflict
- repository failure
- important edge cases

Do not connect tests to production services or depend on test order.
Do not weaken production code for easier tests.

## Documentation

Maintain:

- README.md
- .env.example
- docs/ROADMAP.md
- docs/openapi.yaml after OpenAPI work starts
- architecture documentation once stable

Never claim unfinished functionality is implemented.

## Verification

When applicable, run from repository root:

- go fmt ./...
- go vet ./...
- go test ./...
- docker compose config
- docker compose up -d
- docker compose ps

Do not claim Docker verification when Docker was unavailable.

## Definition of done

A roadmap task is complete only when:

- requested behavior is implemented;
- code is formatted;
- relevant tests exist and pass;
- go vet passes;
- errors are handled;
- no secrets are committed;
- documentation is updated when behavior changes;
- no unrelated changes remain;
- the roadmap task is marked complete in its final commit;
- the commit is focused and meaningful.
