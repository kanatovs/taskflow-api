param(
    [ValidateRange(1024, 65535)][int]$ApiPort = 18084,
    [ValidateRange(1024, 65535)][int]$PostgresPort = 15440,
    [ValidateRange(1024, 65535)][int]$RedisPort = 16380
)

$ErrorActionPreference = 'Stop'
$repositoryRoot = Split-Path -Parent $PSScriptRoot
$smokeProject = 'taskflow-cache-smoke-' + [Guid]::NewGuid().ToString('N').Substring(0, 8)
$composeArguments = @('--env-file', '.env.example', '-p', $smokeProject, '-f', 'compose.yaml', '-f', 'scripts/compose.cache-test.yaml')
$exampleSettings = @{
    API_PORT = "$ApiPort"; POSTGRES_PORT = "$PostgresPort"; REDIS_TEST_PORT = "$RedisPort"
    POSTGRES_DB = 'taskflow'; POSTGRES_USER = 'taskflow'; POSTGRES_PASSWORD = 'local_example_only'
    DATABASE_URL = 'postgres://taskflow:local_example_only@postgres:5432/taskflow?sslmode=disable'
    DATABASE_TIMEOUT = '3s'; JWT_SECRET = 'local_example_only_change_me_32_bytes_minimum'; JWT_ACCESS_TTL = '15m'
    HTTP_READ_HEADER_TIMEOUT = '5s'; HTTP_READ_TIMEOUT = '10s'; HTTP_WRITE_TIMEOUT = '10s'
    HTTP_IDLE_TIMEOUT = '60s'; HTTP_SHUTDOWN_TIMEOUT = '10s'; API_STOP_GRACE_PERIOD = '30s'
    REDIS_ADDR = 'redis:6379'; REDIS_PASSWORD = ''; REDIS_DB = '0'; CACHE_TTL = '1m'
    REDIS_DIAL_TIMEOUT = '200ms'; REDIS_OPERATION_TIMEOUT = '100ms'
    TEST_POSTGRES_URL = "postgres://taskflow:local_example_only@127.0.0.1:$PostgresPort/taskflow?sslmode=disable"
    TEST_REDIS_ADDR = "127.0.0.1:$RedisPort"
}
$previousSettings = @{}
$httpClient = $null
$accessTokens = [Collections.Generic.List[string]]::new()

function Invoke-Compose([string[]]$Arguments) {
    & docker compose @composeArguments @Arguments
    if ($LASTEXITCODE -ne 0) { throw "Docker Compose failed with exit code $LASTEXITCODE" }
}

function Read-Redis([string[]]$Arguments) {
    & docker compose @composeArguments exec -T redis redis-cli --raw @Arguments
    if ($LASTEXITCODE -ne 0) { throw 'Redis inspection failed' }
}

function Assert-Absent([string]$Key) {
    if ((Read-Redis @('EXISTS', $Key)) -ne '0') { throw 'Invalidated or negative entry is cached' }
}

function Send-Request([string]$Method, [string]$Path, $Body, [string]$Authorization, [int]$Expected) {
    $request = [Net.Http.HttpRequestMessage]::new([Net.Http.HttpMethod]::new($Method), "http://127.0.0.1:$ApiPort$Path")
    try {
        if ($null -ne $Body) {
            $request.Content = [Net.Http.StringContent]::new(($Body | ConvertTo-Json -Compress), [Text.Encoding]::UTF8, 'application/json')
        }
        if ($Authorization) { [void]$request.Headers.TryAddWithoutValidation('Authorization', $Authorization) }
        $response = $httpClient.SendAsync($request).GetAwaiter().GetResult()
        try {
            $content = $response.Content.ReadAsStringAsync().GetAwaiter().GetResult()
            if ([int]$response.StatusCode -ne $Expected) { throw "$Method $Path returned $([int]$response.StatusCode), expected $Expected" }
            if ($Expected -eq 204) {
                if ($content.Length -ne 0) { throw '204 response contains a body' }
                return
            }
            if ($response.Content.Headers.ContentType.MediaType -ne 'application/json') { throw 'Response is not JSON' }
            foreach ($sensitive in @('password_hash', 'strong-password-123', '$2a$', '$2b$')) {
                if ($content.Contains($sensitive)) { throw 'Password or hash found in response' }
            }
            return $content | ConvertFrom-Json
        } finally { $response.Dispose() }
    } finally { $request.Dispose() }
}

function New-SmokeUser([string]$Name) {
    $created = Send-Request 'POST' '/api/v1/auth/register' @{email="$Name@example.com";password='strong-password-123';display_name=$Name} '' 201
    $login = Send-Request 'POST' '/api/v1/auth/login' @{email="$Name@example.com";password='strong-password-123'} '' 200
    $accessTokens.Add($login.data.access_token)
    return [PSCustomObject]@{ ID = $created.data.id; Authorization = 'Bearer ' + $login.data.access_token }
}

Push-Location -LiteralPath $repositoryRoot
try {
    foreach ($name in $exampleSettings.Keys) { $previousSettings[$name] = [Environment]::GetEnvironmentVariable($name, 'Process') }
    foreach ($name in $exampleSettings.Keys) { [Environment]::SetEnvironmentVariable($name, $exampleSettings[$name], 'Process') }
    Invoke-Compose @('config', '--quiet')
    Invoke-Compose @('up', '-d', '--wait', 'postgres', 'redis')
    Invoke-Compose @('run', '--rm', 'migrate', 'up')
    Invoke-Compose @('run', '--rm', 'migrate', 'up')
    & go test -count=1 ./...
    if ($LASTEXITCODE -ne 0) { throw 'Go unit/PostgreSQL/Redis integration tests failed' }
    Write-Output 'Full Go suite with isolated PostgreSQL and Redis: passed'
    Invoke-Compose @('up', '-d', '--build', '--wait', 'api')
    Add-Type -AssemblyName System.Net.Http
    $httpClient = [Net.Http.HttpClient]::new()
    $httpClient.Timeout = [TimeSpan]::FromSeconds(5)
    $owner = New-SmokeUser 'owner'
    $member = New-SmokeUser 'member'
    $outsider = New-SmokeUser 'outsider'
    $project = Send-Request 'POST' '/api/v1/projects' @{name='Cache project'} $owner.Authorization 201
    $path = '/api/v1/projects/' + $project.data.id
    $null = Send-Request 'POST' ($path + '/members') @{user_id=$member.ID;role='member'} $owner.Authorization 201
    $task = Send-Request 'POST' ($path + '/tasks') @{title='Cache task'} $owner.Authorization 201
    $taskPath = $path + '/tasks/' + $task.data.id
    $projectKey = 'taskflow:v1:project:' + $project.data.id
    $taskKey = $projectKey + ':task:' + $task.data.id
    foreach ($detail in @($path, $taskPath)) {
        $null = Send-Request 'GET' $detail $null $member.Authorization 200
        $null = Send-Request 'GET' $detail $null $member.Authorization 200
        $null = Send-Request 'GET' $detail $null $outsider.Authorization 403
        $null = Send-Request 'GET' $detail $null '' 401
    }
    foreach ($key in @($projectKey, $taskKey)) {
        $ttl = [long](Read-Redis @('PTTL', $key))
        if ($ttl -le 0 -or $ttl -gt 60000) { throw 'Cache TTL missing or incorrect' }
        $cached = (Read-Redis @('GET', $key)) -join "`n"
        foreach ($sensitive in @('password', 'Authorization', 'access_token', '"data"', '"role"')) {
            if ($cached.Contains($sensitive)) { throw 'Non-domain or secret data cached' }
        }
    }
    $null = Read-Redis @('SET', $projectKey, 'malformed-cache-sentinel', 'PX', '60000')
    $null = Read-Redis @('SET', $taskKey, 'malformed-cache-sentinel', 'PX', '60000')
    $null = Send-Request 'GET' $path $null $owner.Authorization 200
    $null = Send-Request 'GET' $taskPath $null $owner.Authorization 200
    $null = Send-Request 'PATCH' $path @{name='Updated project'} $owner.Authorization 200
    Assert-Absent $projectKey
    $fresh = Send-Request 'GET' $path $null $owner.Authorization 200
    if ($fresh.data.name -ne 'Updated project') { throw 'Stale project after update' }
    foreach ($change in @(@{title='Updated task'}, @{status='in_progress'}, @{priority='high'}, @{assignee_id=$member.ID}, @{due_date='2026-10-20T10:00:00Z'}, @{assignee_id=$null;due_date=$null})) {
        $null = Send-Request 'PATCH' $taskPath $change $owner.Authorization 200
        Assert-Absent $taskKey
        $fresh = Send-Request 'GET' $taskPath $null $owner.Authorization 200
        foreach ($field in $change.Keys) { if ($fresh.data.$field -ne $change[$field]) { throw "Stale task field: $field" } }
    }
    $null = Send-Request 'POST' ($taskPath + '/comments') @{body='Comment does not change task details'} $member.Authorization 201
    if ((Read-Redis @('EXISTS', $taskKey)) -ne '1') { throw 'Comment invalidated task detail' }
    $null = Send-Request 'GET' '/api/v1/projects' $null $owner.Authorization 200
    $null = Send-Request 'GET' ($path + '/tasks?search=Updated') $null $owner.Authorization 200
    $cacheKeys = @(Read-Redis @('--scan', '--pattern', 'taskflow:v1:*'))
    if ($cacheKeys.Count -ne 2) { throw 'Unexpected list/search/membership cache entries' }
    $other = Send-Request 'POST' '/api/v1/projects' @{name='Other'} $owner.Authorization 201
    $null = Send-Request 'GET' ('/api/v1/projects/' + $other.data.id + '/tasks/' + $task.data.id) $null $owner.Authorization 404
    $null = Send-Request 'DELETE' ($path + '/members/' + $member.ID) $null $owner.Authorization 204
    $null = Send-Request 'GET' $path $null $member.Authorization 403
    $null = Send-Request 'GET' $taskPath $null $member.Authorization 403
    Write-Output 'Project/task cache smoke: hit/miss, TTL, malformed values, permissions, removal, scoped keys and mutation invalidation passed'

    Invoke-Compose @('stop', 'redis')
    $null = Send-Request 'GET' '/ready' $null '' 200
    $null = Send-Request 'GET' $path $null $owner.Authorization 200
    $null = Send-Request 'GET' $taskPath $null $owner.Authorization 200
    $null = Send-Request 'PATCH' $path @{name='Offline update'} $owner.Authorization 200
    $null = Send-Request 'PATCH' $taskPath @{title='Offline task'} $owner.Authorization 200
    $null = New-SmokeUser 'offline-user'
    $null = Send-Request 'GET' '/api/v1/users/me' $null $owner.Authorization 200
    $beforeRestartLogs = (Invoke-Compose @('logs', '--no-color', 'api')) -join "`n"
    if (-not $beforeRestartLogs.Contains('Redis invalidation failed') -or -not $beforeRestartLogs.Contains('Malformed cached value')) {
        throw 'Invalidation or malformed cache warning missing'
    }
    Invoke-Compose @('up', '-d', '--force-recreate', '--wait', 'api')
    $null = Send-Request 'GET' '/ready' $null '' 200
    $fresh = Send-Request 'GET' $path $null $owner.Authorization 200
    if ($fresh.data.name -ne 'Offline update') { throw 'PostgreSQL mutation lost during Redis outage' }
    Write-Output 'Redis-unavailable smoke: readiness, detail reads, writes, login, registration and API startup passed'
    $logs = $beforeRestartLogs + ((Invoke-Compose @('logs', '--no-color', 'api')) -join "`n")
    if (-not $logs.Contains('Redis read unavailable')) { throw 'Redis failure warning missing' }
    Invoke-Compose @('start', 'redis')
    $fresh = Send-Request 'GET' $taskPath $null $owner.Authorization 200
    if ($fresh.data.title -ne 'Offline task') { throw 'Stale task after Redis recovery' }
    $null = Send-Request 'DELETE' $taskPath $null $owner.Authorization 204
    Assert-Absent $taskKey
    $null = Send-Request 'GET' $taskPath $null $owner.Authorization 404
    Assert-Absent $taskKey
    $null = Send-Request 'DELETE' $path $null $owner.Authorization 204
    Assert-Absent $projectKey
    $null = Send-Request 'GET' $path $null $owner.Authorization 404
    Assert-Absent $projectKey
    $logs += (Invoke-Compose @('logs', '--no-color', 'api')) -join "`n"
    foreach ($sensitive in @('strong-password-123', 'password_hash', 'malformed-cache-sentinel') + $accessTokens.ToArray()) {
        if ($logs.Contains($sensitive)) { throw 'Secret or cache payload in API logs' }
    }
    $env:REDIS_ADDR = ''
    Invoke-Compose @('up', '-d', '--force-recreate', '--wait', 'api')
    $null = Send-Request 'GET' '/ready' $null '' 200
    $null = Send-Request 'GET' ('/api/v1/projects/' + $other.data.id) $null $owner.Authorization 200
    Invoke-Compose @('stop', 'api')
    $state = (Invoke-Compose @('ps', '-a', '--format', 'json', 'api')) | ConvertFrom-Json
    if ($state.ExitCode -ne 0 -or $state.State -ne 'exited') { throw 'API did not shut down cleanly' }
    Write-Output 'Cache deletion, recovery, disabled-cache mode and graceful shutdown: passed'
} finally {
    try {
        if ($httpClient) { $httpClient.Dispose() }
        if ($smokeProject -notmatch '^taskflow-cache-smoke-[a-f0-9]{8}$') { throw 'Unsafe cleanup project name' }
        Invoke-Compose @('down', '--volumes')
    } finally {
        foreach ($name in $previousSettings.Keys) {
            if ($null -eq $previousSettings[$name]) { Remove-Item -LiteralPath "Env:$name" -ErrorAction SilentlyContinue }
            else { [Environment]::SetEnvironmentVariable($name, $previousSettings[$name], 'Process') }
        }
        Pop-Location
    }
}
