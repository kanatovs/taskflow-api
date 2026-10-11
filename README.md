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

- HTTP server listening on `:8080`.
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

Port 8080 must be available; a startup failure exits with a nonzero code.
The listen address is fixed until environment configuration is added in TF-003.
Stop the process with `Ctrl+C`; graceful shutdown will be added in TF-005.

## Verification

```sh
go fmt ./...
go test ./...
go vet ./...
go build ./...
```
