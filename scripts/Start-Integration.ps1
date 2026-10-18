param([string]$EnvPath = (Join-Path $PSScriptRoot '..\.runtime\.env.integration'))
$ErrorActionPreference = 'Stop'
$projectRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
if (-not (Test-Path -LiteralPath $EnvPath)) { throw 'Missing integration environment file. Configure .runtime/.env.integration first.' }
. (Join-Path $PSScriptRoot 'Use-Env.ps1') -Path $EnvPath
if ($env:DEMO_MODE -ne 'false') { throw 'The integration environment must set DEMO_MODE=false.' }
$serverPath = Join-Path $projectRoot '.runtime\studio-integration.exe'
Push-Location -LiteralPath (Join-Path $projectRoot 'backend')
try {
    go build -o $serverPath ./cmd/server
    if ($LASTEXITCODE -ne 0) { throw 'Server build failed. Stop any existing integration server before rebuilding.' }
    & $serverPath
} finally { Pop-Location }
