param(
    [ValidateRange(1024, 65535)][int]$ApiPort = 18081,
    [ValidateRange(1024, 65535)][int]$PostgresPort = 15434
)

$ErrorActionPreference = 'Stop'
$repositoryRoot = Split-Path -Parent $PSScriptRoot
$smokeProject = 'taskflow-auth-smoke-' + [Guid]::NewGuid().ToString('N').Substring(0, 8)
$composeArguments = @('--env-file', '.env.example', '-p', $smokeProject)
$exampleSettings = @{
    API_PORT = "$ApiPort"
    POSTGRES_PORT = "$PostgresPort"
    POSTGRES_DB = 'taskflow'
    POSTGRES_USER = 'taskflow'
    POSTGRES_PASSWORD = 'local_example_only'
    DATABASE_URL = 'postgres://taskflow:local_example_only@postgres:5432/taskflow?sslmode=disable'
    DATABASE_TIMEOUT = '3s'
    JWT_SECRET = 'local_example_only_change_me_32_bytes_minimum'
    JWT_ACCESS_TTL = '15m'
    HTTP_READ_HEADER_TIMEOUT = '5s'
    HTTP_READ_TIMEOUT = '10s'
    HTTP_WRITE_TIMEOUT = '10s'
    HTTP_IDLE_TIMEOUT = '60s'
    HTTP_SHUTDOWN_TIMEOUT = '10s'
    API_STOP_GRACE_PERIOD = '30s'
}
$previousSettings = @{}
$httpClient = $null

function Invoke-Compose([string[]]$Arguments) {
    & docker compose @composeArguments @Arguments
    if ($LASTEXITCODE -ne 0) { throw "Docker Compose failed with exit code $LASTEXITCODE" }
}

function Send-Request([string]$Method, [string]$Path, [string]$Body = '', [string]$Authorization = '') {
    $request = [System.Net.Http.HttpRequestMessage]::new([System.Net.Http.HttpMethod]::new($Method), "http://127.0.0.1:$ApiPort$Path")
    try {
        if ($Body -ne '') { $request.Content = [System.Net.Http.StringContent]::new($Body, [Text.Encoding]::UTF8, 'application/json') }
        if ($Authorization -ne '') { [void]$request.Headers.TryAddWithoutValidation('Authorization', $Authorization) }
        $response = $httpClient.SendAsync($request).GetAwaiter().GetResult()
        try {
            $content = $response.Content.ReadAsStringAsync().GetAwaiter().GetResult()
            return [PSCustomObject]@{ Status = [int]$response.StatusCode; Body = $content }
        } finally { $response.Dispose() }
    } finally { $request.Dispose() }
}

function Assert-Status($Response, [int]$Expected, [string]$Label) {
    if ($Response.Status -ne $Expected) { throw "$Label returned $($Response.Status), expected $Expected" }
    Write-Output "${Label}: $Expected"
}

function ConvertTo-Base64Url([byte[]]$Bytes) {
    return [Convert]::ToBase64String($Bytes).TrimEnd('=').Replace('+', '-').Replace('/', '_')
}

function New-TestToken([long]$UserId, [string]$Secret, [bool]$Expired) {
    $now = [DateTimeOffset]::UtcNow.ToUnixTimeSeconds()
    $expires = $now + 60
    if ($Expired) { $expires = $now - 60 }
    $claims = @{ sub = "$UserId"; iat = ($now - 120); exp = $expires } | ConvertTo-Json -Compress
    $header = ConvertTo-Base64Url ([Text.Encoding]::UTF8.GetBytes('{"alg":"HS256","typ":"JWT"}'))
    $payload = ConvertTo-Base64Url ([Text.Encoding]::UTF8.GetBytes($claims))
    $unsigned = "$header.$payload"
    $hmac = [System.Security.Cryptography.HMACSHA256]::new([Text.Encoding]::UTF8.GetBytes($Secret))
    try { $signature = ConvertTo-Base64Url ($hmac.ComputeHash([Text.Encoding]::UTF8.GetBytes($unsigned))) }
    finally { $hmac.Dispose() }
    return "$unsigned.$signature"
}

Push-Location -LiteralPath $repositoryRoot
try {
    foreach ($name in $exampleSettings.Keys) {
        $previousSettings[$name] = [Environment]::GetEnvironmentVariable($name, 'Process')
    }
    foreach ($name in $exampleSettings.Keys) { [Environment]::SetEnvironmentVariable($name, $exampleSettings[$name], 'Process') }
    # The explicit example URL targets only this disposable project's service.
    Invoke-Compose -Arguments @('config', '--quiet')
    Invoke-Compose -Arguments @('up', '-d', '--wait', 'postgres')
    Invoke-Compose -Arguments @('run', '--rm', 'migrate', 'up')
    Invoke-Compose -Arguments @('run', '--rm', 'migrate', 'up')
    Invoke-Compose -Arguments @('run', '--rm', 'migrate', 'down', '1')
    Invoke-Compose -Arguments @('run', '--rm', 'migrate', 'up')
    Invoke-Compose -Arguments @('up', '-d', '--build', '--wait', 'api')
    Add-Type -AssemblyName System.Net.Http
    $httpClient = [System.Net.Http.HttpClient]::new()
    $httpClient.Timeout = [TimeSpan]::FromSeconds(10)
    Assert-Status (Send-Request 'GET' '/health') 200 'Liveness'
    Assert-Status (Send-Request 'GET' '/ready') 200 'Readiness'
    $registration = '{"email":" USER@Example.com ","password":"strong-password-123","display_name":" User "}'
    $created = Send-Request 'POST' '/api/v1/auth/register' $registration
    Assert-Status $created 201 'Registration'
    $user = ($created.Body | ConvertFrom-Json).data
    if ($user.email -ne 'user@example.com' -or $user.display_name -ne 'User' -or $created.Body.Contains('password')) { throw 'Incorrect public user response' }
    Assert-Status (Send-Request 'POST' '/api/v1/auth/register' $registration) 409 'Duplicate email'
    Assert-Status (Send-Request 'POST' '/api/v1/auth/register' '{"unknown":true}') 400 'Invalid registration'
    $login = Send-Request 'POST' '/api/v1/auth/login' '{"email":"user@example.com","password":"strong-password-123"}'
    Assert-Status $login 200 'Login'
    $access = ($login.Body | ConvertFrom-Json).data.access_token
    $me = Send-Request 'GET' '/api/v1/users/me' '' "Bearer $access"
    Assert-Status $me 200 'Authenticated current user'
    if (($me.Body | ConvertFrom-Json).data.id -ne $user.id -or $me.Body.Contains('password')) { throw 'Incorrect current user response' }
    $wrong = Send-Request 'POST' '/api/v1/auth/login' '{"email":"user@example.com","password":"wrong-password"}'
    $unknown = Send-Request 'POST' '/api/v1/auth/login' '{"email":"unknown@example.com","password":"strong-password-123"}'
    Assert-Status $wrong 401 'Incorrect password'
    Assert-Status $unknown 401 'Unknown email'
    if ($wrong.Body -ne $unknown.Body) { throw 'Login disclosed account existence' }
    Assert-Status (Send-Request 'GET' '/api/v1/users/me') 401 'Missing credentials'
    Assert-Status (Send-Request 'GET' '/api/v1/users/me' '' 'Bearer invalid') 401 'Malformed credentials'
    $badSignature = New-TestToken $user.id 'another-secret-at-least-32-bytes-long' $false
    $expired = New-TestToken $user.id $exampleSettings.JWT_SECRET $true
    Assert-Status (Send-Request 'GET' '/api/v1/users/me' '' "Bearer $badSignature") 401 'Invalid JWT signature'
    Assert-Status (Send-Request 'GET' '/api/v1/users/me' '' "Bearer $expired") 401 'Expired JWT'
    Invoke-Compose -Arguments @('stop', 'postgres')
    Assert-Status (Send-Request 'GET' '/health') 200 'Liveness without PostgreSQL'
    $unavailable = Send-Request 'GET' '/ready'
    Assert-Status $unavailable 503 'Readiness without PostgreSQL'
    if (($unavailable.Body | ConvertFrom-Json).error.code -ne 'not_ready') { throw 'Unsafe readiness response' }
    Invoke-Compose -Arguments @('up', '-d', '--wait', 'postgres')
    Assert-Status (Send-Request 'GET' '/ready') 200 'Readiness after recovery'
    $logs = (Invoke-Compose -Arguments @('logs', '--no-color', 'api')) -join "`n"
    foreach ($sensitive in @($access, 'strong-password-123', '$2a$', 'password_hash')) {
        if ($logs.Contains($sensitive)) { throw 'Authentication secret found in logs' }
    }
    Invoke-Compose -Arguments @('stop', 'api')
    $state = (Invoke-Compose -Arguments @('ps', '-a', '--format', 'json', 'api')) | ConvertFrom-Json
    if ($state.ExitCode -ne 0 -or $state.State -ne 'exited') { throw 'API shutdown was not clean' }
    Write-Output 'Authentication Docker smoke checks passed; API shutdown exit code: 0'
} finally {
    try {
        if ($httpClient) { $httpClient.Dispose() }
        if ($smokeProject -notmatch '^taskflow-auth-smoke-[a-f0-9]{8}$') { throw 'Unsafe cleanup project name' }
        # Only this script's fresh project and test volume are removed.
        Invoke-Compose -Arguments @('down', '--volumes')
    } finally {
        foreach ($name in $previousSettings.Keys) {
            if ($null -eq $previousSettings[$name]) { Remove-Item -LiteralPath "Env:$name" -ErrorAction SilentlyContinue }
            else { [Environment]::SetEnvironmentVariable($name, $previousSettings[$name], 'Process') }
        }
        Pop-Location
    }
}
