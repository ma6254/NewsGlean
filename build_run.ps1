param(
    [switch]$SkipSwag
)
$ErrorActionPreference = "Stop"
$Root = $PSScriptRoot
$env:GOCACHE = Join-Path $Root ".gocache"

Write-Host "go mod tidy ..."
go mod tidy
Write-Host "go mod vendor ..."
go mod vendor

$args = @()
if ($SkipSwag) { $args += "-SkipSwag" }
& (Join-Path $Root "build.ps1") @args

# clean old data to verify from a clean state
$data = Join-Path $Root "release\data.db"
if (Test-Path $data) { Remove-Item $data -Force }

& (Join-Path $Root "run.ps1")
