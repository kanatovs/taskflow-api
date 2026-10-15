# TaskFlow v1.0.0 release process

Release preparation is local. Push, merge, tagging and publication require
separate authorization. This guide contains the proposed PR and maintainer
commands; the public release notes are [v1.0.0.md](v1.0.0.md).

## Suggested pull request

Title: `chore: prepare TaskFlow API v1.0.0`

Summary: Complete the TaskFlow v1 documentation, verification, CI and release
preparation epic. Document the implemented contract and make local/CI checks
reproducible using disposable PostgreSQL and Redis environments.

Main changes:

- Add OpenAPI schemas, access rules and validated examples for all 23 operations.
- Add clean migration up/down/up and dirty-state checks, strengthen cache access
  and fallback integration coverage, and add reusable check runners.
- Add read-only GitHub Actions CI for PRs and pushes to main.
- Complete setup/API documentation, architecture, ER/cache diagrams and test map.
- Review security/dependencies; patch Redis to 8.2.10, pin image digests, and
  document findings, regression tests and audit limitations.
- Prepare version 1.0.0, changelog and release notes.
- Correct expected SQL-error capture in Windows PowerShell 5.1 authentication
  smoke checks, and serialize both database-heavy Go runners for reliability.

API/database changes: No new HTTP operations or migrations; schema stays at 4.
OpenAPI is now 1.0.0; its nullable create defaults and cross-field search description
match the existing implementation. Redis is a patched dependency within 8.2.

Tests/manual verification: See the exact local command checklist below, the
readiness report and test guide. Checks include empty-database migrations,
real PostgreSQL/Redis integration, real Redis outage/recovery, API startup,
README request examples and graceful shutdown. Hosted CI still requires a push
and PR; local success is not a claim that GitHub Actions has already run.

Known limitations: Stateless access tokens without refresh/revocation, no rate
limiter or account recovery, development-only Compose credentials, TTL-bounded
cache staleness, serialized project writes and offset pagination. No container-wide
CVE scan or penetration test. The local history scan supports selected credential
patterns; OpenPGP advisory GO-2026-5932 belongs to an unimported package.

Roadmap tasks: TF-025, TF-026, TF-027, TF-028, TF-029, TF-030. Preserve the six
focused commits when merging; do not squash meaningful history.

## Local verification checklist

From the repository root in Windows PowerShell:

```powershell
go fmt ./...
go test ./...
go test -count=1 ./...
go vet ./...
go build ./...
go mod tidy
go mod verify
git diff --check
docker compose --env-file .env.example config --quiet
./.tmp/docs-venv/Scripts/python.exe scripts/validate-openapi.py
python -m unittest discover -s scripts -p 'test_*.py'
python scripts/audit_repository.py
powershell -NoProfile -ExecutionPolicy Bypass -File ./scripts/check-go.ps1
powershell -NoProfile -ExecutionPolicy Bypass -File ./scripts/integration.ps1
powershell -NoProfile -ExecutionPolicy Bypass -File ./scripts/auth-smoke.ps1
powershell -NoProfile -ExecutionPolicy Bypass -File ./scripts/projects-smoke.ps1
powershell -NoProfile -ExecutionPolicy Bypass -File ./scripts/tasks-smoke.ps1
powershell -NoProfile -ExecutionPolicy Bypass -File ./scripts/cache-smoke.ps1
```

Create the docs venv as described in README first. Go tests without integration
variables skip service-dependent tests; the integration/cache runners supply
isolated services and run uncached tests. Run smoke scripts sequentially. Their
cleanup must succeed; never use the development volume as a disposable database.
The cache smoke includes Redis-unavailable fallback.
`go test -race -p 1 -count=1 ./...` additionally passed on Windows with CGO enabled
and MSYS2 GCC 14.2.0.

## Verification record (2026-10-15)

| Check | Result |
| --- | --- |
| `go fmt ./...`, `go test ./...`, `go test -count=1 ./...` | Passed; standalone runs skip service-dependent tests without their variables |
| `go vet ./...`, `go build ./...`, `go mod tidy`, `go mod verify` | Passed; tidy left module files unchanged |
| `git diff --check` | Passed |
| `docker compose config --quiet` with local and example environments | Passed without printing expanded credentials |
| `scripts/integration.ps1` | Passed: empty DB 0 -> 4 -> 0 -> 4, repeated up, dirty rejection, uncached full suite with isolated PostgreSQL/Redis |
| `scripts/auth-smoke.ps1` under Windows PowerShell 5.1.26100.9444 | Passed: schema checks, registration/login, duplicate email, bad/expired JWT, PostgreSQL outage/recovery and clean shutdown |
| `scripts/projects-smoke.ps1` | Passed: migrations, CRUD, owner membership, roles, ownership protection and cross-project isolation |
| `scripts/tasks-smoke.ps1` | Passed: migration versions 4/3/2/4, task/comment CRUD, assignment/status, permissions, filters/pages, cascades |
| `scripts/cache-smoke.ps1` | Passed: uncached complete suite, cache policy, real outage/fallback, API startup with Redis down, recovery, disabled cache and clean shutdown |
| `scripts/check-go.ps1`, actionlint 1.7.12 | Passed; local CI-equivalent commands, not a hosted workflow result |
| OpenAPI validator 0.9.0 | Passed: 13 paths, 23 operations and 41 examples, including formats |
| Python scanner tests / local repository audit | Passed; selected credential patterns and repository hygiene only |
| Native Windows `go test -race -p 1 -count=1 ./...` | Passed with CGO and GCC 14.2.0; service integration verified separately |
| README API example block / local Markdown links | Passed in disposable Compose / filesystem verification |
| Original development PostgreSQL metadata | Version 4, dirty=false; not rolled back by tests |
| Temporary test containers and volumes | None left after runner cleanup |

The first final authentication smoke attempt stopped on an expected SQL error
because of PowerShell 5.1 native-stderr semantics. The capture function was fixed,
and every negative schema check and the complete smoke run then passed. Other
earlier failed attempts, the dependency advisory and limits of the security scan
are documented in [the readiness review](../release-readiness.md).
Hosted CI, push, merge, tag and publication remain separate maintainer actions.

## Steps after merge to create the stable release

These commands are instructions only. Run them **after** the reviewed PR is
merged into `main`, main's CI is green, and the working tree is clean. They
publish a tag/release and require separate explicit authorization in an agent
session. No tag was created during preparation.

```powershell
git switch main
git pull --ff-only origin main
git status --short
git log -10 --oneline
```

Confirm there are no status entries, TF-001 through TF-030 are checked, the PR's
six commits are present, and CI for the merge commit passed.
The HEAD being tagged must be the reviewed release merge commit; inspect any
later main changes before publishing. Every command must exit successfully.

Confirm that tag `v1.0.0` does not already exist locally or on origin:

```powershell
git tag --list v1.0.0
git ls-remote --tags origin refs/tags/v1.0.0
```

Both checks should be empty. Then create and push only the annotated tag:

```powershell
git tag -a v1.0.0 -m 'Release TaskFlow API v1.0.0'
git show --no-patch v1.0.0
git push origin v1.0.0
```

Create a GitHub release for that existing tag using the release contents and
known limitations above. With GitHub CLI installed/authenticated, the optional
publication command is:

```powershell
gh release create v1.0.0 --verify-tag --title 'TaskFlow API v1.0.0' --notes-file docs/releases/v1.0.0.md
```

Never force-push a tag, tag an unmerged feature branch, or automatically begin
LinkPulse. If the tag already exists, inspect it instead of recreating it.
