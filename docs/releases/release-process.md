# TaskFlow API release process

Use this checklist to prepare and publish a release. The
[testing guide](../testing.md) describes test isolation and coverage; previous
release notes are in this directory.

## Prepare the release

1. Update [CHANGELOG.md](../../CHANGELOG.md) and add
   `docs/releases/vX.Y.Z.md` with the release scope, setup changes, verification
   results and known limitations.
2. Check that [OpenAPI](../openapi.yaml), [README](../../README.md), migrations
   and environment examples match the version being released.
3. Run the checks below against the release candidate. Record their actual
   results, including skipped tests and unavailable tools.
4. Merge the reviewed changes into `main` and require the
   [CI workflow](../../.github/workflows/ci.yml) to pass for the commit being tagged.

## Verify the release candidate

From the repository root in Windows PowerShell:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File ./scripts/check-go.ps1
go mod verify
git diff --check
docker compose --env-file .env.example config --quiet
./.tmp/docs-venv/Scripts/python.exe scripts/validate-openapi.py
python -m unittest discover -s scripts -p 'test_*.py'
python scripts/audit_repository.py
powershell -NoProfile -ExecutionPolicy Bypass -File ./scripts/integration.ps1
powershell -NoProfile -ExecutionPolicy Bypass -File ./scripts/auth-smoke.ps1
powershell -NoProfile -ExecutionPolicy Bypass -File ./scripts/projects-smoke.ps1
powershell -NoProfile -ExecutionPolicy Bypass -File ./scripts/tasks-smoke.ps1
powershell -NoProfile -ExecutionPolicy Bypass -File ./scripts/cache-smoke.ps1
```

Create the documentation virtual environment using the
[README instructions](../../README.md) first. The Go check runner verifies
formatting, vet, tests, build and whitespace. Without integration variables,
service-dependent Go tests skip; the integration runner supplies isolated
PostgreSQL and Redis services and runs uncached tests. Run the smoke scripts
sequentially and confirm their cleanup succeeds. Use only disposable test
resources, never the development database volume.

For changes affecting concurrency, also run `go test -race -p 1 -count=1 ./...`
with CGO and a supported C compiler. Unit tests without service variables do not
replace PostgreSQL/Redis integration coverage. The repository audit checks
selected credential patterns and file hygiene; it is not a complete secret or
container vulnerability scan.

## Tag and publish

Start from a clean working tree on an up-to-date `main`:

```powershell
git switch main
git pull --ff-only origin main
git status --short
git log -5 --oneline
```

Require empty status output. Confirm that `HEAD` is the reviewed release commit
and its hosted CI checks passed. Local verification does not establish a hosted
CI result.

Set the intended version, then check that it does not exist locally or on origin:

```powershell
$version = 'vX.Y.Z'
git tag --list $version
git ls-remote --tags origin "refs/tags/$version"
```

Replace `vX.Y.Z` with the release version. Both checks must succeed and return no
matching tag. If the tag exists, inspect it instead of recreating it. Check that
`docs/releases/$version.md` contains the final release notes, then create and
publish the annotated tag:

```powershell
git tag -a $version -m "Release TaskFlow API $version"
git show --no-patch $version
git push origin $version
gh release create $version --verify-tag --title "TaskFlow API $version" --notes-file "docs/releases/$version.md"
```

Check every command's exit status before continuing. The final command requires
an authenticated GitHub CLI. Never force-push a tag or tag an unmerged feature
branch.
