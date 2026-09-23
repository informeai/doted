# Builds doted.exe and packs it into an .msi installer.
#
# Usage: packaging/windows/build-msi.ps1 -Version 1.2.3 [-OutDir dist]
# Needs the WiX Toolset 5 .NET tool:
#   dotnet tool install --global wix --version 5.0.2
param(
    [Parameter(Mandatory)][string]$Version,
    [string]$OutDir = "dist"
)
$ErrorActionPreference = "Stop"

# MSI versions are numeric: major.minor.build, each within its limits.
if ($Version -notmatch '^\d{1,3}\.\d{1,3}\.\d{1,5}$') {
    throw "MSI versions must look like 1.2.3, got '$Version'"
}

$root = Resolve-Path (Join-Path $PSScriptRoot "../..")
$work = Join-Path ([System.IO.Path]::GetTempPath()) ("doted-msi-" + [guid]::NewGuid())
New-Item -ItemType Directory -Force -Path $work, $OutDir | Out-Null

try {
    # -H=windowsgui: no console window next to doted's own window.
    $env:CGO_ENABLED = "0"
    $env:GOOS = "windows"
    $env:GOARCH = "amd64"
    Push-Location $root
    go build -trimpath -ldflags "-s -w -H=windowsgui -X main.version=$Version" -o (Join-Path $work "doted.exe") .
    if ($LASTEXITCODE -ne 0) { throw "go build failed" }
    Pop-Location

    $msi = Join-Path (Resolve-Path $OutDir) "doted-$Version-windows-x64.msi"
    wix build -arch x64 -d "Version=$Version" -d "BinDir=$work" (Join-Path $root "packaging/windows/doted.wxs") -o $msi
    if ($LASTEXITCODE -ne 0) { throw "wix build failed" }
    Write-Output $msi
}
finally {
    Remove-Item -Recurse -Force $work -ErrorAction SilentlyContinue
}
