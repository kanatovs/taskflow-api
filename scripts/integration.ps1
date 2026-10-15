param(
    [ValidateRange(1024, 65535)][int]$PostgresPort = 15441,
    [ValidateRange(1024, 65535)][int]$RedisPort = 16381
)

$ErrorActionPreference = 'Stop'
$repositoryRoot = Split-Path -Parent $PSScriptRoot
$testProject = 'taskflow-integration-' + [Guid]::NewGuid().ToString('N').Substring(0, 8)
$composeArguments = @('--env-file', '.env.example', '-p', $testProject, '-f', 'compose.yaml', '-f', 'scripts/compose.cache-test.yaml')
$settings = @{
    POSTGRES_PORT = "$PostgresPort"; REDIS_TEST_PORT = "$RedisPort"
    POSTGRES_DB = 'taskflow'; POSTGRES_USER = 'taskflow'; POSTGRES_PASSWORD = 'local_example_only'
    DATABASE_URL = 'postgres://taskflow:local_example_only@postgres:5432/taskflow?sslmode=disable'
    JWT_SECRET = 'local_example_only_change_me_32_bytes_minimum'
    TEST_POSTGRES_URL = "postgres://taskflow:local_example_only@127.0.0.1:$PostgresPort/taskflow?sslmode=disable"
    TEST_REDIS_ADDR = "127.0.0.1:$RedisPort"
}
$previousSettings = @{}

function Invoke-Compose([string[]]$Arguments) {
    & docker compose @composeArguments @Arguments
    if ($LASTEXITCODE -ne 0) { throw "Docker Compose failed with exit code $LASTEXITCODE" }
}

function Read-SQL([string]$Query) {
    $result = $Query | & docker compose @composeArguments exec -T postgres sh -ec 'exec psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -At -v ON_ERROR_STOP=1'
    if ($LASTEXITCODE -ne 0) { throw 'Integration SQL failed' }
    return ($result -join "`n").Trim()
}

function Assert-Version4 {
    if ((Read-SQL 'SELECT version, dirty FROM schema_migrations;') -ne '4|f') {
        throw 'Expected clean migration version 4'
    }
    $tables = Read-SQL "SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name IN ('users','projects','project_members','tasks','comments');"
    if ($tables -ne '5') { throw 'Expected all five application tables' }
}

Push-Location -LiteralPath $repositoryRoot
try {
    foreach ($name in $settings.Keys) { $previousSettings[$name] = [Environment]::GetEnvironmentVariable($name, 'Process') }
    foreach ($name in $settings.Keys) { [Environment]::SetEnvironmentVariable($name, $settings[$name], 'Process') }
    Invoke-Compose @('config', '--quiet')
    Invoke-Compose @('up', '-d', '--wait', 'postgres', 'redis')
    if ((Read-SQL "SELECT to_regclass('public.users') IS NULL;") -ne 't') { throw 'Test database is not clean' }
    Invoke-Compose @('run', '--rm', 'migrate', 'up')
    Assert-Version4
    Invoke-Compose @('run', '--rm', 'migrate', 'up')
    Invoke-Compose @('run', '--rm', 'migrate', 'down', '4')
    if ((Read-SQL "SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name IN ('users','projects','project_members','tasks','comments');") -ne '0') {
        throw 'Down migrations left application tables behind'
    }
    Invoke-Compose @('run', '--rm', 'migrate', 'up')
    Assert-Version4

    # Simulate an interrupted migration only in this newly created disposable DB.
    $null = Read-SQL 'UPDATE schema_migrations SET dirty=true WHERE version=4;'
    if ((Read-SQL 'SELECT version, dirty FROM schema_migrations;') -ne '4|t') { throw 'Dirty state was not set' }
    $previousErrorAction = $ErrorActionPreference
    try {
        $ErrorActionPreference = 'Continue'
        $dirtyOutput = (& docker compose @composeArguments run --rm migrate up 2>&1 | Out-String)
        $dirtyExit = $LASTEXITCODE
    } finally { $ErrorActionPreference = $previousErrorAction }
    if ($dirtyExit -eq 0 -or $dirtyOutput -notmatch 'Dirty database version 4') {
        throw 'Migrator did not reject the dirty database as expected'
    }
    # No DDL was interrupted: the schema is known to be at 4. Never use force to
    # repair a real database without first inspecting and repairing its schema.
    Invoke-Compose @('run', '--rm', 'migrate', 'force', '4')
    Assert-Version4
    Invoke-Compose @('run', '--rm', 'migrate', 'up')
    Write-Output 'Migrations: clean 0 -> 4 -> 0 -> 4, repeated up and dirty-state rejection passed'

    & go test -count=1 ./...
    if ($LASTEXITCODE -ne 0) { throw 'Uncached Go unit/PostgreSQL/Redis integration suite failed' }
    Write-Output 'Uncached integration suite passed against isolated PostgreSQL and Redis'
} finally {
    try {
        if ($testProject -notmatch '^taskflow-integration-[a-f0-9]{8}$') { throw 'Unsafe cleanup project name' }
        Invoke-Compose @('down', '--volumes')
    } finally {
        foreach ($name in $previousSettings.Keys) {
            if ($null -eq $previousSettings[$name]) { Remove-Item -LiteralPath "Env:$name" -ErrorAction SilentlyContinue }
            else { [Environment]::SetEnvironmentVariable($name, $previousSettings[$name], 'Process') }
        }
        Pop-Location
    }
}
