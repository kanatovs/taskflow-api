param(
    [ValidateRange(1024, 65535)][int]$ApiPort = 18083,
    [ValidateRange(1024, 65535)][int]$PostgresPort = 15438
)

$ErrorActionPreference = 'Stop'
$repositoryRoot = Split-Path -Parent $PSScriptRoot
$smokeProject = 'taskflow-task-smoke-' + [Guid]::NewGuid().ToString('N').Substring(0, 8)
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

function Assert-MigrationState([string]$Expected) {
    $query = "SELECT version, dirty, to_regclass('public.tasks') IS NOT NULL, to_regclass('public.comments') IS NOT NULL FROM public.schema_migrations;"
    $state = $query | & docker compose @composeArguments exec -T postgres sh -ec 'exec psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -At -v ON_ERROR_STOP=1'
    if ($LASTEXITCODE -ne 0) { throw 'Could not read migration state' }
    if (($state -join "`n").Trim() -ne $Expected) { throw "Unexpected migration state; expected $Expected" }
}

Push-Location -LiteralPath $repositoryRoot
try {
    foreach ($name in $exampleSettings.Keys) { $previousSettings[$name] = [Environment]::GetEnvironmentVariable($name, 'Process') }
    foreach ($name in $exampleSettings.Keys) { [Environment]::SetEnvironmentVariable($name, $exampleSettings[$name], 'Process') }
    Invoke-Compose @('config', '--quiet')
    Invoke-Compose @('up', '-d', '--wait', 'postgres')
    Invoke-Compose @('run', '--rm', 'migrate', 'up')
    Assert-Schema './scripts/check-tasks-schema.sql'
    Assert-MigrationState '4|f|t|t'
    Invoke-Compose @('run', '--rm', 'migrate', 'up')
    Invoke-Compose @('run', '--rm', 'migrate', 'down', '1')
    Assert-MigrationState '3|f|t|f'
    Invoke-Compose @('run', '--rm', 'migrate', 'down', '1')
    Assert-MigrationState '2|f|f|f'
    Assert-Schema './scripts/check-projects-schema.sql'
    Invoke-Compose @('run', '--rm', 'migrate', 'up')
    Assert-Schema './scripts/check-tasks-schema.sql'
    Assert-MigrationState '4|f|t|t'
    Write-Output 'Task/comment migration up/repeated-up/down/down/up checks passed'
    Invoke-Compose @('up', '-d', '--build', '--wait', 'api')
    Add-Type -AssemblyName System.Net.Http
    $httpClient = [Net.Http.HttpClient]::new()
    $httpClient.Timeout = [TimeSpan]::FromSeconds(10)
    $owner = New-SmokeUser 'owner'
    $admin = New-SmokeUser 'admin'
    $member = New-SmokeUser 'member'
    $outsider = New-SmokeUser 'outsider'
    $project = Send-Request 'POST' '/api/v1/projects' @{name='Task smoke'} $owner.Authorization 201
    $projectPath = '/api/v1/projects/' + $project.data.id
    $null = Send-Request 'POST' ($projectPath + '/members') @{user_id=$admin.ID;role='admin'} $owner.Authorization 201
    $null = Send-Request 'POST' ($projectPath + '/members') @{user_id=$member.ID;role='member'} $owner.Authorization 201
    $tasks = $projectPath + '/tasks'
    $null = Send-Request 'POST' $tasks @{title='Anonymous'} '' 401
    $null = Send-Request 'GET' $tasks $null 'Bearer invalid' 401
    $null = Send-Request 'POST' $tasks @{title='Outside'} $outsider.Authorization 403
    $null = Send-Request 'POST' $tasks @{title=' '} $owner.Authorization 400
    $null = Send-Request 'POST' $tasks @{title='Wrong assignment';assignee_id=$outsider.ID} $owner.Authorization 400
    $created = Send-Request 'POST' $tasks @{title=' First task ';description='Needle';priority='high';assignee_id=$member.ID;due_date='2026-10-20T15:00:00+05:00'} $owner.Authorization 201
    if ($created.data.title -ne 'First task' -or $created.data.creator_id -ne $owner.ID -or $created.data.status -ne 'todo') { throw 'Incorrect task creation' }
    $item = $tasks + '/' + $created.data.id
    $null = Send-Request 'GET' $item $null $member.Authorization 200
    $null = Send-Request 'PATCH' $item @{title='Forbidden'} $member.Authorization 403
    $null = Send-Request 'PATCH' $item @{status='done'} $member.Authorization 409
    $null = Send-Request 'PATCH' $item @{status='in_progress'} $member.Authorization 200
    $null = Send-Request 'PATCH' $item @{status='done'} $member.Authorization 200
    $null = Send-Request 'PATCH' $item @{status='in_progress'} $admin.Authorization 200
    $second = Send-Request 'POST' $tasks @{title='Second';priority='high'} $member.Authorization 201
    $filtered = Send-Request 'GET' ($tasks + '?status=in_progress&priority=high&search=needle&sort=due_date&order=asc&page_size=1') $null $owner.Authorization 200
    if ($filtered.meta.total -ne 1 -or $filtered.data.Count -ne 1 -or $filtered.data[0].id -ne $created.data.id) { throw 'Combined filtering failed' }
    $page = Send-Request 'GET' ($tasks + '?sort=priority&order=desc&page_size=1&page=2') $null $owner.Authorization 200
    if ($page.meta.total -ne 2 -or $page.data.Count -ne 1 -or $page.data[0].id -ne $created.data.id) { throw 'Stable pagination failed' }
    $empty = Send-Request 'GET' ($tasks + '?page=99') $null $owner.Authorization 200
    if ($empty.meta.total -ne 2 -or $empty.data.Count -ne 0) { throw 'Empty page lost total' }
    $null = Send-Request 'GET' ($tasks + '?sort=unknown') $null $owner.Authorization 400
    $cleared = Send-Request 'PATCH' $item @{assignee_id=$null;due_date=$null} $owner.Authorization 200
    if ($null -ne $cleared.data.assignee_id -or $null -ne $cleared.data.due_date -or $cleared.data.description -ne 'Needle') { throw 'Nullable PATCH failed' }
    $other = Send-Request 'POST' '/api/v1/projects' @{name='Other'} $owner.Authorization 201
    $wrongItem = '/api/v1/projects/' + $other.data.id + '/tasks/' + $created.data.id
    $null = Send-Request 'GET' $wrongItem $null $owner.Authorization 404
    $null = Send-Request 'PATCH' $wrongItem @{title='Cross-project'} $owner.Authorization 404
    $null = Send-Request 'DELETE' $wrongItem $null $owner.Authorization 404
    $comments = $item + '/comments'
    $null = Send-Request 'POST' $comments @{body=' '} $member.Authorization 400
    $comment = Send-Request 'POST' $comments @{body=' First comment '} $member.Authorization 201
    if ($comment.data.author_id -ne $member.ID -or $comment.data.body -ne 'First comment') { throw 'Incorrect comment creation' }
    $commentPath = $comments + '/' + $comment.data.id
    $null = Send-Request 'PATCH' $commentPath @{body='Author edit'} $member.Authorization 200
    $ownerComment = Send-Request 'POST' $comments @{body='Owner comment'} $owner.Authorization 201
    $ownerCommentPath = $comments + '/' + $ownerComment.data.id
    $null = Send-Request 'PATCH' $ownerCommentPath @{body='Forbidden'} $member.Authorization 403
    $null = Send-Request 'PATCH' $ownerCommentPath @{body='Admin edit'} $admin.Authorization 200
    $null = Send-Request 'GET' $comments $null $outsider.Authorization 403
    $wrongComment = $tasks + '/' + $second.data.id + '/comments/' + $comment.data.id
    $null = Send-Request 'DELETE' $wrongComment $null $owner.Authorization 404
    $listed = Send-Request 'GET' ($comments + '?page_size=1') $null $member.Authorization 200
    if ($listed.meta.total -ne 2 -or $listed.data.Count -ne 1) { throw 'Comment pagination failed' }
    $null = Send-Request 'DELETE' $commentPath $null $member.Authorization 204
    $null = Send-Request 'DELETE' ($projectPath + '/members/' + $member.ID) $null $owner.Authorization 204
    $null = Send-Request 'GET' $comments $null $member.Authorization 403
    $null = Send-Request 'DELETE' $item $null $admin.Authorization 204
    $null = Send-Request 'GET' $comments $null $owner.Authorization 404
    $null = Send-Request 'DELETE' $projectPath $null $owner.Authorization 204
    Write-Output 'Task/comment CRUD, permissions, filtering, pagination and cascading deletion passed'
    $logs = (Invoke-Compose @('logs', '--no-color', 'api')) -join "`n"
    foreach ($sensitive in @('strong-password-123', 'password_hash', '$2a$') + $accessTokens.ToArray()) {
        if ($logs.Contains($sensitive)) { throw 'Secret found in API logs' }
    }
    Invoke-Compose @('stop', 'api')
    $state = (Invoke-Compose @('ps', '-a', '--format', 'json', 'api')) | ConvertFrom-Json
    if ($state.ExitCode -ne 0 -or $state.State -ne 'exited') { throw 'API did not shut down cleanly' }
    Write-Output 'Task and comment Docker smoke checks passed; API shutdown exit code: 0'
} finally {
    try {
        if ($httpClient) { $httpClient.Dispose() }
        if ($smokeProject -notmatch '^taskflow-task-smoke-[a-f0-9]{8}$') { throw 'Unsafe cleanup project name' }
        Invoke-Compose @('down', '--volumes')
    } finally {
        foreach ($name in $previousSettings.Keys) {
            if ($null -eq $previousSettings[$name]) { Remove-Item -LiteralPath "Env:$name" -ErrorAction SilentlyContinue }
            else { [Environment]::SetEnvironmentVariable($name, $previousSettings[$name], 'Process') }
        }
        Pop-Location
    }
}
