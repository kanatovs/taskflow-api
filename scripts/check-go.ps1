$ErrorActionPreference = 'Stop'
$repositoryRoot = Split-Path -Parent $PSScriptRoot

Push-Location -LiteralPath $repositoryRoot
try {
    $goFiles = @(git ls-files -- '*.go')
    if ($LASTEXITCODE -ne 0 -or $goFiles.Count -eq 0) { throw 'Could not list Go source files' }
    $unformatted = @(& gofmt -l @goFiles)
    if ($LASTEXITCODE -ne 0) { throw 'gofmt failed' }
    if ($unformatted.Count -gt 0) { throw ('Unformatted Go files: ' + ($unformatted -join ', ')) }
    & go vet ./...
    if ($LASTEXITCODE -ne 0) { throw 'go vet failed' }
    & go test ./...
    if ($LASTEXITCODE -ne 0) { throw 'go test failed' }
    & go build ./...
    if ($LASTEXITCODE -ne 0) { throw 'go build failed' }
    & git diff --check
    if ($LASTEXITCODE -ne 0) { throw 'git diff --check failed' }
    Write-Output 'Go formatting, vet, tests, build and whitespace checks passed'
} finally { Pop-Location }
