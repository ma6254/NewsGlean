$ErrorActionPreference = "Stop"
$Root = $PSScriptRoot
& (Join-Path $Root "release\app.exe") -d (Join-Path $Root "release")
