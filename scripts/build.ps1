# Cross-platform release compilation script for HushDrop (PowerShell)
$ErrorActionPreference = "Stop"

$rootDir = Resolve-Path "$PSScriptRoot\.."
Set-Location -Path $rootDir

Write-Host "===> 1. Building React PWA frontend..." -ForegroundColor Cyan
Set-Location -Path "$rootDir\frontend"
cmd.exe /c "npm run build"
Set-Location -Path $rootDir

Write-Host "===> 2. Syncing embedded assets into internal/server/web..." -ForegroundColor Cyan
if (!(Test-Path "$rootDir\internal\server\web")) {
    New-Item -ItemType Directory -Path "$rootDir\internal\server\web" -Force | Out-Null
}
Copy-Item -Path "$rootDir\frontend\dist\*" -Destination "$rootDir\internal\server\web\" -Recurse -Force

Write-Host "===> 2b. Generating Windows resource (brand icon + version info)..." -ForegroundColor Cyan
$version = "0.5.0"
$brandDir = "$rootDir\assets\brand"
if (Test-Path "$brandDir\icon-1024.png") {
    $winres = [ordered]@{
        "RT_GROUP_ICON" = [ordered]@{
            "APP" = [ordered]@{
                "0000" = @("ico/16.png", "ico/24.png", "ico/32.png", "ico/48.png", "ico/64.png", "ico/128.png", "ico/256.png")
            }
        }
        "RT_VERSION" = [ordered]@{
            "1" = [ordered]@{
                "0000" = [ordered]@{
                    "fixed" = [ordered]@{
                        "file_version"   = "$version.0"
                        "product_version" = "$version.0"
                    }
                    "info" = [ordered]@{
                        "0409" = [ordered]@{
                            "ProductName"      = "HushDrop"
                            "FileDescription"  = "HushDrop P2P LAN transfer"
                            "FileVersion"      = $version
                            "ProductVersion"   = $version
                            "OriginalFilename" = "HushDrop.exe"
                        }
                    }
                }
            }
        }
    }
    $winres | ConvertTo-Json -Depth 10 | Out-String | ForEach-Object { [System.IO.File]::WriteAllText("$brandDir\winres.json", $_, [System.Text.UTF8Encoding]::new($false)) }
    Push-Location $brandDir
    try {
        go run github.com/tc-hib/go-winres@v0.3.3 make --in winres.json --arch amd64 --out "$rootDir\cmd\hushdrop\rsrc"
        if ($LASTEXITCODE -ne 0) { throw "go-winres make failed" }
        Write-Host "   -> rsrc_windows_amd64.syso written (icon + version resource)" -ForegroundColor Green
    } finally {
        Pop-Location
    }
} else {
    Write-Host "   -> Skipped: assets/brand/icon-1024.png not found" -ForegroundColor Yellow
}

Write-Host "===> 3. Compiling Go binaries into ./dist/..." -ForegroundColor Cyan
$distDir = "$rootDir\dist"
if (!(Test-Path $distDir)) {
    New-Item -ItemType Directory -Path $distDir -Force | Out-Null
}

$env:CGO_ENABLED = "0"
$version = "0.5.0"
$ldflags = "-s -w -X main.version=$version -X main.openBrowserDefault=false"

# Windows amd64 (console + GUI builds from the same code)
Write-Host "   -> Building dist/HushDrop-console.exe (Windows amd64, console)..." -ForegroundColor Yellow
$env:GOOS = "windows"
$env:GOARCH = "amd64"
go build -ldflags $ldflags -o "$distDir\HushDrop-console.exe" .\cmd\hushdrop
Copy-Item "$distDir\HushDrop-console.exe" "$rootDir\HushDrop-console.exe" -Force

Write-Host "   -> Building dist/HushDrop.exe (Windows amd64, GUI)..." -ForegroundColor Yellow
$ldflagsNoConsole = "-s -w -X main.version=$version -X main.openBrowserDefault=true -H=windowsgui"
go build -ldflags $ldflagsNoConsole -o "$distDir\HushDrop.exe" .\cmd\hushdrop
Copy-Item "$distDir\HushDrop.exe" "$rootDir\HushDrop.exe" -Force

# Linux amd64
Write-Host "   -> Building dist/hushdrop-linux-amd64 (Linux amd64)..." -ForegroundColor Yellow
$env:GOOS = "linux"
$env:GOARCH = "amd64"
go build -ldflags $ldflags -o "$distDir\hushdrop-linux-amd64" .\cmd\hushdrop

# macOS arm64
Write-Host "   -> Building dist/hushdrop-darwin-arm64 (macOS arm64)..." -ForegroundColor Yellow
$env:GOOS = "darwin"
$env:GOARCH = "arm64"
go build -ldflags $ldflags -o "$distDir\hushdrop-darwin-arm64" .\cmd\hushdrop

# macOS amd64
Write-Host "   -> Building dist/hushdrop-darwin-amd64 (macOS amd64)..." -ForegroundColor Yellow
$env:GOOS = "darwin"
$env:GOARCH = "amd64"
go build -ldflags $ldflags -o "$distDir\hushdrop-darwin-amd64" .\cmd\hushdrop

Write-Host "===> 4. Calculating SHA-256 checksums..." -ForegroundColor Cyan
$checksumFile = "$distDir\checksums.txt"
if (Test-Path $checksumFile) { Remove-Item $checksumFile -Force }

Get-ChildItem -Path $distDir -File | Where-Object { $_.Name -ne "checksums.txt" } | ForEach-Object {
    $hash = (Get-FileHash -Path $_.FullName -Algorithm SHA256).Hash.ToLower()
    "$hash  $($_.Name)" | Out-File -FilePath $checksumFile -Append -Encoding ascii
}

Write-Host "===> 5. Verifying artifact security (no keys / certs / logs / env)..." -ForegroundColor Cyan
$leakedFiles = Get-ChildItem -Path $distDir -Recurse -File -Include "*.key", "*.pem", "*.crt", "*.log", ".env*"
if ($leakedFiles.Count -gt 0) {
    Write-Error "CRITICAL: Private keys/logs detected in dist: $($leakedFiles -join ', ')"
    exit 1
}
Write-Host "   -> Artifact security check passed: no secrets or logs found." -ForegroundColor Green

Write-Host "===> Build finished successfully in ./dist/:" -ForegroundColor Green
Get-Content $checksumFile
