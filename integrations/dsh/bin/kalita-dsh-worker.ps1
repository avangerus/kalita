# kalita-dsh-worker.ps1 — supervisor loop: one DSH headless run per task.
#
# Prereqs: `dsh` on PATH (`npx @deepseek-ai/dsh`), a kalita node running, and either
#   KALITA_TOKEN (agent bearer token), or
#   KALITA_BOOTSTRAP_SECRET + KALITA_WORKER_ID + KALITA_WORKER_ROLE (self-register).
# Optional env: KALITA_URL (default http://127.0.0.1:8080), KALITA_ROLE (prompt label).

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot   # integrations/dsh
$overlay = Join-Path $root 'cordis.yml'

$node = $env:KALITA_URL
if (-not $node) { $node = 'http://127.0.0.1:8080' }

if (-not $env:KALITA_TOKEN) {
    if (-not $env:KALITA_BOOTSTRAP_SECRET) { throw 'set KALITA_TOKEN or KALITA_BOOTSTRAP_SECRET' }
    $body = @{ secret = $env:KALITA_BOOTSTRAP_SECRET; id = $env:KALITA_WORKER_ID; role = $env:KALITA_WORKER_ROLE } | ConvertTo-Json -Compress
    $resp = Invoke-RestMethod -Method Post -Uri "$node/api/bootstrap" -ContentType 'application/json' -Body $body
    $env:KALITA_TOKEN = $resp.token
    Write-Host "bootstrapped agent $env:KALITA_WORKER_ID as $env:KALITA_WORKER_ROLE"
}

$role = $env:KALITA_ROLE
if (-not $role) { $role = $env:KALITA_WORKER_ROLE }
if (-not $role) { $role = 'agent' }

$task = "You are a kalita worker (role: $role). Load the kalita-worker skill and follow it. Poll your kalita task pool ONCE with mcp__kalita__wait_for_task. If a task arrives, take it, do the work, report and complete or fail it. If the poll times out with no task, reply with just 'no task'. Touch nothing outside mcp__kalita__* tools."

while ($true) {
    dsh --profile headless --patch $overlay $task
    if ($LASTEXITCODE -ne 0) { Write-Warning "dsh exited with code $LASTEXITCODE" }
    Start-Sleep -Seconds 2
}
