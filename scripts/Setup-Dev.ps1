param([switch]$Demo)
$ErrorActionPreference = 'Stop'
$projectRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$envPath = Join-Path $projectRoot '.env'
if (Test-Path -LiteralPath $envPath) { throw '.env already exists; edit it directly to preserve local configuration.' }
$randomBytes = New-Object byte[] 48
$random = [System.Security.Cryptography.RandomNumberGenerator]::Create()
try { $random.GetBytes($randomBytes) } finally { $random.Dispose() }
$appSecret = [Convert]::ToBase64String($randomBytes)
$content = [System.IO.File]::ReadAllText((Join-Path $projectRoot '.env.example'))
$content = $content.Replace('replace-with-a-random-secret-of-at-least-32-characters', $appSecret)
if ($Demo) { $content = $content.Replace('DEMO_MODE=false', 'DEMO_MODE=true') }
[System.IO.File]::WriteAllText($envPath, $content, (New-Object System.Text.UTF8Encoding($false)))
Write-Output 'Created local .env. Use scripts/Use-Env.ps1 before starting the backend.'
