param(
    [switch]$SkipSwag
)
$ErrorActionPreference = "Stop"
$Root = $PSScriptRoot
$Package = "github.com/ma6254/news-glean"

# go build needs the build cache inside the workspace in sandbox/offline envs
$env:GOCACHE = Join-Path $Root ".gocache"

# Version: prefer git tag, fall back to default. With no tag, git prints
# "fatal" to stderr, so relax error handling to avoid aborting the script.
$BuildVersion = "0.1.0"
$eap = $ErrorActionPreference
$ErrorActionPreference = "Continue"
$tag = git -C $Root describe --tags --abbrev=0 2>$null
$code = $LASTEXITCODE
$ErrorActionPreference = $eap
if ($code -eq 0 -and $tag) {
    $BuildVersion = $tag.Trim()
}

$BuildTime = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
$LdFlags = "-s -w " +
    "-X ${Package}/internal/build.BuildTime=$BuildTime " +
    "-X ${Package}/internal/build.BuildVersion=$BuildVersion"

if (-not $SkipSwag) {
    Write-Host "swag init ..."
    swag init --parseDependency --parseInternal
}

New-Item -ItemType Directory -Force -Path (Join-Path $Root "release") | Out-Null

# 把前端构建产物嵌入（若已构建）；未构建时保留占位页，前端仍可用 proxy 模式提供
$webDist = Join-Path $Root "..\NewsGlean-web\dist"
$embedDist = Join-Path $Root "internal\webui\dist"
if (Test-Path (Join-Path $webDist "index.html")) {
    Remove-Item (Join-Path $embedDist "*") -Recurse -Force -ErrorAction SilentlyContinue
    Copy-Item (Join-Path $webDist "*") $embedDist -Recurse -Force
    Write-Host "embedded frontend dist from $webDist"
}

Write-Host "building $Package @ $BuildVersion ..."
go build -tags sqlite_fts5 -ldflags $LdFlags -o (Join-Path $Root "release\app.exe") .

# copy default config to release/ on first build
$targetCfg = Join-Path $Root "release\config.yml"
if (-not (Test-Path $targetCfg)) {
    Copy-Item (Join-Path $Root "docs\default.yml") $targetCfg
    Write-Host "copied default config to release/config.yml"
}
Write-Host "done."
