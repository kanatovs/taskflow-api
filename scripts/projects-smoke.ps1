param(
    [ValidateRange(1024, 65535)][int]$ApiPort = 18082,
    [ValidateRange(1024, 65535)][int]$PostgresPort = 15436
)

$ErrorActionPreference = 'Stop'
$repositoryRoot = Split-Path -Parent $PSScriptRoot
$smokeProject = 'taskflow-project-smoke-' + [Guid]::NewGuid().ToString('N').Substring(0, 8)
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
    HTTP_REQUEST_TIMEOUT = '5s'
    HTTP_WRITE_TIMEOUT = '10s'
    HTTP_IDLE_TIMEOUT = '60s'
    HTTP_SHUTDOWN_TIMEOUT = '10s'
    API_STOP_GRACE_PERIOD = '30s'
}
$previousSettings = @{}
$httpClient = $null
$accessTokens = [Collections.Generic.List[string]]::new()

function Invoke-Compose([string[]]$Arguments) {
    & docker compose @composeArguments @Arguments
    if ($LASTEXITCODE -ne 0) { throw "Docker Compose failed with exit code $LASTEXITCODE" }
}

function Assert-Schema([string]$File) {
    Get-Content -Raw -LiteralPath $File | & docker compose @composeArguments exec -T postgres sh -c 'exec psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1'
    if ($LASTEXITCODE -ne 0) { throw "Schema verification failed: $File" }
}

function Send-Request([string]$Method, [string]$Path, $Body, [string]$Authorization, [int]$Expected) {
    $request = [Net.Http.HttpRequestMessage]::new([Net.Http.HttpMethod]::new($Method), "http://127.0.0.1:$ApiPort$Path")
    try {
        if ($null -ne $Body) {
            $json = $Body | ConvertTo-Json -Compress
            $request.Content = [Net.Http.StringContent]::new($json, [Text.Encoding]::UTF8, 'application/json')
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
            foreach ($sensitive in @('password_hash', 'private-test-hash', 'strong-password-123', '$2a$', '$2b$', '$2y$')) {
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
    Invoke-Compose @('up', '-d', '--wait', 'postgres')
    Invoke-Compose @('run', '--rm', 'migrate', 'up')
    Assert-Schema './scripts/check-projects-schema.sql'
    Invoke-Compose @('run', '--rm', 'migrate', 'up')
    # This fresh project's tables are empty; rollback never touches development data.
    Invoke-Compose @('run', '--rm', 'migrate', 'goto', '1')
    Assert-Schema './scripts/check-users-schema.sql'
    Invoke-Compose @('run', '--rm', 'migrate', 'up')
    Assert-Schema './scripts/check-projects-schema.sql'
    Write-Output 'Project migration up/repeated-up/rollback-to-users/up checks passed'
    Invoke-Compose @('up', '-d', '--build', '--wait', 'api')
    Add-Type -AssemblyName System.Net.Http
    $httpClient = [Net.Http.HttpClient]::new()
    $httpClient.Timeout = [TimeSpan]::FromSeconds(10)
    $owner = New-SmokeUser 'owner'
    $admin = New-SmokeUser 'admin'
    $member = New-SmokeUser 'member'
    $outsider = New-SmokeUser 'outsider'
    $target = New-SmokeUser 'target'
    $null = Send-Request 'POST' '/api/v1/projects' @{name='No authentication'} '' 401
    $null = Send-Request 'POST' '/api/v1/projects' @{name='Bad token'} 'Bearer invalid' 401
    $null = Send-Request 'POST' '/api/v1/projects' @{name=' '} $owner.Authorization 400
    $created = Send-Request 'POST' '/api/v1/projects' @{name=' Portfolio ';description='Initial description'} $owner.Authorization 201
    if ($created.data.owner_id -ne $owner.ID -or $created.data.name -ne 'Portfolio') { throw 'Incorrect project owner or name' }
    $projectPath = '/api/v1/projects/' + $created.data.id
    $membersPath = $projectPath + '/members'
    $ownerMembership = Send-Request 'GET' $membersPath $null $owner.Authorization 200
    if ($ownerMembership.data.Count -ne 1 -or $ownerMembership.data[0].role -ne 'owner') { throw 'Owner membership missing' }
    $null = Send-Request 'POST' $membersPath @{user_id=$admin.ID;role='admin'} $owner.Authorization 201
    $null = Send-Request 'POST' $membersPath @{user_id=$member.ID;role='member'} $owner.Authorization 201
    $null = Send-Request 'POST' $membersPath @{user_id=$member.ID;role='member'} $owner.Authorization 409
    $null = Send-Request 'POST' $membersPath @{user_id=999999;role='member'} $owner.Authorization 404
    $null = Send-Request 'POST' $membersPath @{user_id=$target.ID;role='owner'} $owner.Authorization 400
    $null = Send-Request 'GET' $projectPath $null $member.Authorization 200
    $null = Send-Request 'GET' $projectPath $null $outsider.Authorization 403
    $null = Send-Request 'PATCH' $projectPath @{name='Forbidden'} $member.Authorization 403
    $null = Send-Request 'PATCH' $projectPath @{description='Admin description'} $admin.Authorization 200
    $updated = Send-Request 'PATCH' $projectPath @{name='Updated'} $owner.Authorization 200
    if ($updated.data.description -ne 'Admin description') { throw 'PATCH replaced an omitted field' }
    $other = Send-Request 'POST' '/api/v1/projects' @{name='Other project'} $outsider.Authorization 201
    $listed = Send-Request 'GET' '/api/v1/projects?page=1&page_size=1' $null $member.Authorization 200
    if ($listed.meta.total -ne 1 -or $listed.data[0].id -ne $created.data.id) { throw 'Project listing leaked another project' }
    $null = Send-Request 'DELETE' $projectPath $null $admin.Authorization 403
    $null = Send-Request 'DELETE' $projectPath $null $member.Authorization 403
    Write-Output 'Authenticated project CRUD and member-only listing checks passed'
    $null = Send-Request 'GET' $membersPath $null $member.Authorization 200
    $null = Send-Request 'GET' $membersPath $null $outsider.Authorization 403
    $null = Send-Request 'POST' $membersPath @{user_id=$target.ID;role='member'} $member.Authorization 403
    $null = Send-Request 'POST' $membersPath @{user_id=$target.ID;role='admin'} $admin.Authorization 403
    $null = Send-Request 'POST' $membersPath @{user_id=$target.ID;role='member'} $admin.Authorization 201
    $targetPath = $membersPath + '/' + $target.ID
    $null = Send-Request 'PATCH' $targetPath @{role='admin'} $admin.Authorization 403
    $null = Send-Request 'PATCH' ($membersPath + '/' + $admin.ID) @{role='member'} $admin.Authorization 403
    $null = Send-Request 'DELETE' ($membersPath + '/' + $admin.ID) $null $admin.Authorization 403
    $null = Send-Request 'PATCH' ($membersPath + '/' + $owner.ID) @{role='member'} $owner.Authorization 403
    $null = Send-Request 'DELETE' ($membersPath + '/' + $owner.ID) $null $owner.Authorization 403
    $null = Send-Request 'DELETE' ($membersPath + '/' + $owner.ID) $null $admin.Authorization 403
    $null = Send-Request 'DELETE' $targetPath $null $member.Authorization 403
    $null = Send-Request 'DELETE' $targetPath $null $admin.Authorization 204
    $null = Send-Request 'POST' $membersPath @{user_id=$target.ID;role='member'} $owner.Authorization 201
    $null = Send-Request 'PATCH' $targetPath @{role='admin'} $owner.Authorization 200
    $null = Send-Request 'DELETE' $targetPath $null $admin.Authorization 403
    $null = Send-Request 'PATCH' $targetPath @{role='member'} $owner.Authorization 200
    $null = Send-Request 'DELETE' $targetPath $null $owner.Authorization 204
    $otherPath = '/api/v1/projects/' + $other.data.id
    $null = Send-Request 'POST' ($otherPath + '/members') @{user_id=$target.ID;role='member'} $outsider.Authorization 201
    $null = Send-Request 'DELETE' $targetPath $null $owner.Authorization 404
    $null = Send-Request 'DELETE' ($otherPath + '/members/' + $target.ID) $null $owner.Authorization 403
    $null = Send-Request 'GET' ($otherPath + '/members') $null $target.Authorization 200
    Write-Output 'Membership authorization, owner protection and cross-project checks passed'
    $null = Send-Request 'DELETE' $projectPath $null $owner.Authorization 204
    $null = Send-Request 'GET' $projectPath $null $owner.Authorization 404
    $empty = Send-Request 'GET' '/api/v1/projects' $null $member.Authorization 200
    if ($empty.meta.total -ne 0 -or $empty.data.Count -ne 0) { throw 'Project deletion did not remove membership' }
    $logs = (Invoke-Compose @('logs', '--no-color', 'api')) -join "`n"
    foreach ($sensitive in @('strong-password-123', 'password_hash', '$2a$') + $accessTokens.ToArray()) {
        if ($logs.Contains($sensitive)) { throw 'Secret found in API logs' }
    }
    Invoke-Compose @('stop', 'api')
    $state = (Invoke-Compose @('ps', '-a', '--format', 'json', 'api')) | ConvertFrom-Json
    if ($state.ExitCode -ne 0 -or $state.State -ne 'exited') { throw 'API did not shut down cleanly' }
    Write-Output 'Project and membership Docker smoke checks passed; API shutdown exit code: 0'
} finally {
    try {
        if ($httpClient) { $httpClient.Dispose() }
        if ($smokeProject -notmatch '^taskflow-project-smoke-[a-f0-9]{8}$') { throw 'Unsafe cleanup project name' }
        Invoke-Compose @('down', '--volumes')
    } finally {
        foreach ($name in $previousSettings.Keys) {
            if ($null -eq $previousSettings[$name]) { Remove-Item -LiteralPath "Env:$name" -ErrorAction SilentlyContinue }
            else { [Environment]::SetEnvironmentVariable($name, $previousSettings[$name], 'Process') }
        }
        Pop-Location
    }
}
