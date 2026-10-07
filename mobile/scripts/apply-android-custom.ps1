<#
.SYNOPSIS
    HushDrop Android customization applier (PowerShell wrapper for Windows).
#>
[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path

if (-not (Get-Command node -ErrorAction SilentlyContinue)) {
    Write-Error "[apply-android-custom.ps1] Error: node is required to run apply-android-custom.js"
    exit 1
}

$jsPath = Join-Path $scriptDir 'apply-android-custom.js'
& node $jsPath @args
if ($LASTEXITCODE -ne 0) {
    exit $LASTEXITCODE
}
