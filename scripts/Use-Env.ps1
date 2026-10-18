param([string]$Path = (Join-Path $PSScriptRoot '..\.env'))
$ErrorActionPreference = 'Stop'
$resolvedEnv = (Resolve-Path -LiteralPath $Path).Path
foreach ($line in [System.IO.File]::ReadAllLines($resolvedEnv, [System.Text.Encoding]::UTF8)) {
    $valueLine = $line.Trim()
    if ($valueLine.Length -eq 0 -or $valueLine.StartsWith('#')) { continue }
    if ($valueLine -notmatch '^([A-Z][A-Z0-9_]*)=(.*)$') { throw 'Invalid environment file line (expected KEY=value)' }
    $key = $Matches[1]
    $value = $Matches[2]
    if ($value.Length -ge 2 -and (($value.StartsWith('"') -and $value.EndsWith('"')) -or ($value.StartsWith("'") -and $value.EndsWith("'")))) { $value = $value.Substring(1, $value.Length - 2) }
    [Environment]::SetEnvironmentVariable($key, $value, 'Process')
}
