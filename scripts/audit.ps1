<#
.SYNOPSIS
    HushDrop release-audit harness (READ-ONLY).

.DESCRIPTION
    Verifies the 12 release gates for HushDrop in a single run and prints a
    PASS / FAIL / WARN / SKIP table plus an exit code.

    Gates:
      01 git-base          HEAD, main == public-lite, tag vs code versions
      02 no-secret-tracked no .pem/.key/.env/.exe/.log, no dist/ or data/ in git
      03 versions          main.go / build.ps1 / package.json x3 / tauri.conf / Cargo vs tag
      04 go-vet-test       go vet ./... and go test ./... -count=1
      05 frontend-build    npm build (tsc && vite build)
      06 checksums         recompute SHA-256 and compare with checksums.txt
      07 apk-exported      HostForegroundService must NOT be exported in every APK
      08 apk-version       versionName == latest tag, versionCode bumped
      09 apk-permissions   no READ_MEDIA_*/READ_EXTERNAL_STORAGE, cleartext bounded
      10 apk-freshness     APK must be newer than the HEAD commit (no stale artifacts)
      11 release-keys      no .key/.pem/.crt/.log in dist/ and dist-desktop/ staging
      12 code-hygiene      PIN not logged, fetch() only in api/client.ts, stop() w/o bare startService

    The script NEVER edits tracked files, never stages and never commits.
    `npm run build` rewrites the gitignored frontend/dist/ directory; pass
    -SkipBuild for a strictly non-writing run.

.PARAMETER RepoRoot
    Repository root. Defaults to the parent directory of this script.

.PARAMETER ApkPath
    Audit only this APK. Defaults to every APK found in the release staging
    directories and in the Capacitor gradle output tree.

.PARAMETER SkipBuild
    Skip gate 05 (npm build).

.PARAMETER SkipTests
    Skip gate 04 (go vet / go test).

.EXAMPLE
    pwsh -File scripts/audit.ps1

.EXAMPLE
    pwsh -File scripts/audit.ps1 -SkipBuild -SkipTests

.NOTES
    Exit codes: 0 = no FAIL, 1 = at least one FAIL, 2 = usage/environment error.
#>
[CmdletBinding()]
param(
    [string]$RepoRoot,
    [string]$ApkPath,
    [switch]$SkipBuild,
    [switch]$SkipTests
)

$ErrorActionPreference = 'Continue'
$script:Results = New-Object System.Collections.ArrayList

# Resolve the script directory without relying on a $PSScriptRoot parameter
# default: under `powershell -File` that automatic variable is not yet populated
# while parameter defaults are evaluated.
if (-not $RepoRoot) {
    $scriptDir = $PSScriptRoot
    if (-not $scriptDir -and $MyInvocation.MyCommand.Path) {
        $scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
    }
    if (-not $scriptDir) { $scriptDir = (Get-Location).Path }
    $RepoRoot = Split-Path -Parent $scriptDir
}

# ---------------------------------------------------------------- helpers ---

function Add-Result {
    param(
        [string]$Id,
        [string]$Check,
        [string]$Status,
        [string]$Detail
    )
    [void]$script:Results.Add([pscustomobject]@{
        Id     = $Id
        Check  = $Check
        Status = $Status
        Detail = $Detail
    })
}

function Invoke-External {
    param([string]$Exe, [string[]]$Arguments, [string]$WorkDir)
    $out = $null
    if ($WorkDir) { Push-Location $WorkDir }
    try {
        $out = & $Exe @Arguments 2>&1
    } finally {
        if ($WorkDir) { Pop-Location }
    }
    return [pscustomobject]@{
        Text = (($out | Out-String).Trim())
        Code = $LASTEXITCODE
    }
}

function Get-Tail {
    param([string]$Text, [int]$Lines = 3)
    if (-not $Text) { return '' }
    $split = $Text -split "`r?`n"
    $take = $split.Count
    if ($take -gt $Lines) { $take = $Lines }
    return (($split | Select-Object -Last $take) -join ' | ')
}

function Get-Token {
    param([string]$Path, [string]$Pattern)
    if (-not (Test-Path $Path)) { return $null }
    $hit = Select-String -Path $Path -Pattern $Pattern -List -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($hit -and $hit.Matches.Count -gt 0) { return $hit.Matches[0].Groups[1].Value }
    return $null
}

function Get-JsonVersion {
    param([string]$Path)
    if (-not (Test-Path $Path)) { return $null }
    try {
        $obj = Get-Content -Path $Path -Raw -ErrorAction Stop | ConvertFrom-Json
        return $obj.version
    } catch {
        return $null
    }
}

function Find-Aapt {
    $candidates = @()
    foreach ($base in @($env:ANDROID_HOME, $env:ANDROID_SDK_ROOT)) {
        if ($base -and (Test-Path $base)) {
            $bt = Join-Path $base 'build-tools'
            if (Test-Path $bt) {
                $dirs = Get-ChildItem -Path $bt -Directory -ErrorAction SilentlyContinue | Sort-Object Name -Descending
                foreach ($d in $dirs) {
                    $candidates += (Join-Path $d.FullName 'aapt.exe')
                    $candidates += (Join-Path $d.FullName 'aapt')
                }
            }
        }
    }
    foreach ($c in $candidates) { if (Test-Path $c) { return $c } }
    $cmd = Get-Command aapt -ErrorAction SilentlyContinue
    if ($cmd) { return $cmd.Source }
    return $null
}

function Get-ExportedAfter {
    # Walks the aapt xmltree text forward from a component name to its exported flag.
    param([string[]]$Tree, [string]$ComponentName)
    $start = -1
    for ($i = 0; $i -lt $Tree.Count; $i++) {
        if ($Tree[$i] -match [regex]::Escape($ComponentName)) { $start = $i; break }
    }
    if ($start -lt 0) { return $null }
    $limit = $start + 6
    if ($limit -gt $Tree.Count) { $limit = $Tree.Count }
    for ($j = $start; $j -lt $limit; $j++) {
        if ($Tree[$j] -match 'android:exported\(0x01010010\)=\(type 0x12\)(0x[0-9a-fA-F]+)') {
            if ($Matches[1] -eq '0x0') { return 'false' }
            return 'true'
        }
    }
    return $null
}

# ------------------------------------------------------------------- setup ---

if (-not (Test-Path $RepoRoot)) {
    Write-Host "RepoRoot not found: $RepoRoot" -ForegroundColor Red
    exit 2
}
$RepoRoot = (Resolve-Path $RepoRoot).Path

if (-not (Get-Command git -ErrorAction SilentlyContinue)) {
    Write-Host 'git is required' -ForegroundColor Red
    exit 2
}

Push-Location $RepoRoot

Write-Host ''
Write-Host '=============================================================' -ForegroundColor Cyan
Write-Host '  HushDrop release audit (read-only)' -ForegroundColor Cyan
Write-Host "  repo: $RepoRoot" -ForegroundColor Cyan
Write-Host '=============================================================' -ForegroundColor Cyan

# ------------------------------------------------------- gate 01 git-base ---

$head = (Invoke-External git @('rev-parse','HEAD')).Text
$mainRef = (Invoke-External git @('rev-parse','main')).Text
$liteRef = (Invoke-External git @('rev-parse','public-lite')).Text

$tagsAtHead = (Invoke-External git @('tag','--points-at','HEAD')).Text
$latestTag = ((Invoke-External git @('tag','--sort=-v:refname')).Text -split "`r?`n" | Where-Object { $_ } | Select-Object -First 1)

if ($mainRef -and $liteRef -and ($mainRef -eq $liteRef)) {
    Add-Result '01' 'main == public-lite' 'PASS' "$($mainRef.Substring(0,7))"
} else {
    Add-Result '01' 'main == public-lite' 'FAIL' "main=$mainRef public-lite=$liteRef"
}

$branchDiff = (Invoke-External git @('diff','main...public-lite','--stat')).Text
if ($branchDiff) {
    Add-Result '01b' 'diff main...public-lite empty' 'FAIL' (Get-Tail $branchDiff 2)
} else {
    Add-Result '01b' 'diff main...public-lite empty' 'PASS' '-'
}

if ($tagsAtHead) {
    Add-Result '01c' 'tag points at HEAD' 'PASS' $tagsAtHead
} else {
    Add-Result '01c' 'tag points at HEAD' 'WARN' "HEAD untagged; latest tag $latestTag (no release cut for this commit)"
}

$headCommitDateTxt = (Invoke-External git @('log','-1','--format=%cI')).Text
$headCommitDate = $null
if ($headCommitDateTxt) {
    try { $headCommitDate = [datetime]::Parse($headCommitDateTxt) } catch { $headCommitDate = $null }
}

# --------------------------------------------- gate 02 no-secret-tracked ---

$tracked = (Invoke-External git @('ls-files')).Text -split "`r?`n" | Where-Object { $_ }
$secretPattern = '\.(pem|key|env|exe|log)$|^dist/|^data/|\.p12$|\.jks$'
$secretHits = $tracked | Where-Object { $_ -match $secretPattern }
if ($secretHits) {
    Add-Result '02' 'no secrets/build output tracked' 'FAIL' (($secretHits | Select-Object -First 5) -join ', ')
} else {
    Add-Result '02' 'no secrets/build output tracked' 'PASS' "$($tracked.Count) tracked files clean"
}

# ------------------------------------------------------- gate 03 versions ---

$versionSources = [ordered]@{
    'cmd/hushdrop/main.go'                  = (Get-Token 'cmd/hushdrop/main.go' 'var\s+version\s*=\s*"([^"]+)"')
    'scripts/build.ps1'                     = (Get-Token 'scripts/build.ps1' '\$version\s*=\s*"([^"]+)"')
    'frontend/package.json'                 = (Get-JsonVersion 'frontend/package.json')
    'mobile/package.json'                   = (Get-JsonVersion 'mobile/package.json')
    'desktop/package.json'                  = (Get-JsonVersion 'desktop/package.json')
    'desktop/src-tauri/tauri.conf.json'     = (Get-JsonVersion 'desktop/src-tauri/tauri.conf.json')
    'desktop/src-tauri/Cargo.toml'          = (Get-Token 'desktop/src-tauri/Cargo.toml' '^version\s*=\s*"([^"]+)"')
}

$found = @($versionSources.Values | Where-Object { $_ })
$distinct = @($found | Sort-Object -Unique)
$summary = ($versionSources.GetEnumerator() | ForEach-Object { "$($_.Key)=$($_.Value)" }) -join ' '

if ($distinct.Count -eq 1 -and $found.Count -eq $versionSources.Count) {
    $v = $distinct[0]
    $tagVersion = $null
    if ($latestTag) { $tagVersion = $latestTag.TrimStart('v') }
    if ($tagVersion -eq $v) {
        Add-Result '03' 'versions consistent + match tag' 'PASS' "$v (tag $latestTag)"
    } else {
        Add-Result '03' 'versions consistent + match tag' 'FAIL' "code=$v latestTag=$latestTag"
    }
} else {
    Add-Result '03' 'versions consistent + match tag' 'FAIL' $summary
}

# ------------------------------------------------------ gate 04 go/vet test ---

if ($SkipTests) {
    Add-Result '04' 'go vet ./...' 'SKIP' '-SkipTests'
    Add-Result '04b' 'go test ./...' 'SKIP' '-SkipTests'
} else {
    $vet = Invoke-External go @('vet','./...')
    if ($vet.Code -eq 0) { Add-Result '04' 'go vet ./...' 'PASS' 'exit 0' }
    else { Add-Result '04' 'go vet ./...' 'FAIL' "exit $($vet.Code): $(Get-Tail $vet.Text 2)" }

    $test = Invoke-External go @('test','./...','-count=1')
    if ($test.Code -eq 0) { Add-Result '04b' 'go test ./...' 'PASS' (Get-Tail $test.Text 2) }
    else { Add-Result '04b' 'go test ./...' 'FAIL' "exit $($test.Code): $(Get-Tail $test.Text 3)" }
}

# ---------------------------------------------------- gate 05 frontend build ---

if ($SkipBuild) {
    Add-Result '05' 'frontend build (tsc+vite)' 'SKIP' '-SkipBuild'
} else {
    $npm = 'npm'
    if ($env:OS -eq 'Windows_NT') { $npm = 'npm.cmd' }
    $build = Invoke-External $npm @('run','build') (Join-Path $RepoRoot 'frontend')
    if ($build.Code -eq 0) { Add-Result '05' 'frontend build (tsc+vite)' 'PASS' (Get-Tail $build.Text 2) }
    else { Add-Result '05' 'frontend build (tsc+vite)' 'FAIL' "exit $($build.Code): $(Get-Tail $build.Text 3)" }
}

# --------------------------------------------------------- gate 06 checksums ---

foreach ($stage in @('dist','dist-desktop')) {
    $stagePath = Join-Path $RepoRoot $stage
    $sumFile = Join-Path $stagePath 'checksums.txt'
    if (-not (Test-Path $sumFile)) {
        Add-Result '06' "checksums $stage" 'SKIP' 'no checksums.txt'
        continue
    }
    $expected = @{}
    foreach ($line in (Get-Content $sumFile)) {
        if ($line -match '^([0-9a-fA-F]{64})\s+(.+?)\s*$') {
            $expected[$Matches[2]] = $Matches[1].ToLower()
        }
    }
    $bad = @()
    $checked = 0
    foreach ($f in (Get-ChildItem -Path $stagePath -File | Where-Object { $_.Name -ne 'checksums.txt' })) {
        $actual = (Get-FileHash -Path $f.FullName -Algorithm SHA256).Hash.ToLower()
        $checked++
        if ($expected.ContainsKey($f.Name)) {
            if ($expected[$f.Name] -ne $actual) { $bad += "$($f.Name)(mismatch)" }
        } else {
            $bad += "$($f.Name)(unlisted)"
        }
    }
    foreach ($name in $expected.Keys) {
        if (-not (Test-Path (Join-Path $stagePath $name))) { $bad += "$name(missing)" }
    }
    if ($bad.Count -eq 0) { Add-Result '06' "checksums $stage" 'PASS' "$checked files verified" }
    else { Add-Result '06' "checksums $stage" 'FAIL' ($bad -join ', ') }
}

# ------------------------------------------------------------- APK discovery ---

$aapt = Find-Aapt

$apkList = New-Object System.Collections.ArrayList
if ($ApkPath) {
    if (Test-Path $ApkPath) { [void]$apkList.Add((Resolve-Path $ApkPath).Path) }
} else {
    $gradleOut = Join-Path $RepoRoot 'mobile/android/app/build/outputs/apk'
    if (Test-Path $gradleOut) {
        Get-ChildItem -Path $gradleOut -Recurse -Filter '*.apk' -ErrorAction SilentlyContinue |
            ForEach-Object { [void]$apkList.Add($_.FullName) }
    }
    foreach ($p in @('dist-desktop/HushDrop.apk','dist/HushDrop.apk')) {
        $full = Join-Path $RepoRoot $p
        if (Test-Path $full) { [void]$apkList.Add($full) }
    }
}

if ($apkList.Count -eq 0) {
    foreach ($g in @('07','08','09','10')) {
        Add-Result $g 'APK gates' 'SKIP' 'no APK found (build the Android app first)'
    }
} elseif (-not $aapt) {
    foreach ($g in @('07','08','09','10')) {
        Add-Result $g 'APK gates' 'SKIP' 'aapt not found (set ANDROID_HOME or add build-tools to PATH)'
    }
} else {
    $tagVersion = $null
    if ($latestTag) { $tagVersion = $latestTag.TrimStart('v') }

    foreach ($apk in $apkList) {
        $apkItem = Get-Item $apk
        $label = $apkItem.Name
        $rel = $apk.Replace($RepoRoot, '').TrimStart('\','/')

        $badging = & $aapt dump badging $apk 2>&1
        $tree = & $aapt dump xmltree $apk AndroidManifest.xml 2>&1

        # --- 07 exported ---
        $svcExported = Get-ExportedAfter -Tree $tree -ComponentName 'HostForegroundService'
        if ($null -eq $svcExported) {
            Add-Result '07' "exported=false [$label]" 'FAIL' 'HostForegroundService not found in manifest'
        } elseif ($svcExported -eq 'false') {
            Add-Result '07' "exported=false [$label]" 'PASS' 'service not exported'
        } else {
            Add-Result '07' "exported=false [$label]" 'FAIL' "HostForegroundService exported=$svcExported ($rel)"
        }

        # --- 08 version ---
        $vName = $null; $vCode = $null
        foreach ($line in $badging) {
            if ($line -match "versionCode='(\d+)'") { $vCode = $Matches[1] }
            if ($line -match "versionName='([^']*)'") { $vName = $Matches[1] }
        }
        if ($vName -eq $tagVersion -and [int]$vCode -ge 2) {
            Add-Result '08' "apk version [$label]" 'PASS' "versionName=$vName versionCode=$vCode (tag $latestTag)"
        } else {
            Add-Result '08' "apk version [$label]" 'FAIL' "versionName=$vName versionCode=$vCode expected versionName=$tagVersion and versionCode>=2 ($rel)"
        }

        # --- 09 permissions / cleartext ---
        $perms = @()
        foreach ($line in $badging) {
            if ($line -match "uses-permission: name='([^']+)'") { $perms += $Matches[1] }
        }
        $broad = @($perms | Where-Object { $_ -match 'READ_MEDIA_|READ_EXTERNAL_STORAGE|WRITE_EXTERNAL_STORAGE|MANAGE_EXTERNAL_STORAGE' })
        $hasNsc = $false
        foreach ($line in $tree) { if ($line -match 'networkSecurityConfig') { $hasNsc = $true; break } }
        if ($broad.Count -gt 0) {
            Add-Result '09' "minimal permissions [$label]" 'FAIL' "broad storage perms: $($broad -join ', ')"
        } elseif ($hasNsc) {
            Add-Result '09' "minimal permissions [$label]" 'PASS' "$($perms.Count) perms, networkSecurityConfig present"
        } else {
            Add-Result '09' "minimal permissions [$label]" 'WARN' "$($perms.Count) perms, no networkSecurityConfig"
        }

        # --- 10 freshness (advisory) ---
        # mtime is unreliable: `git checkout` stamps every file with the checkout
        # time, so an APK built just before the commit can look "older" than its
        # own sources. Content gates 07/08/09 are authoritative; this gate only
        # warns so a stale artifact is never silently accepted.
        if ($headCommitDate) {
            if ($apkItem.LastWriteTime -lt $headCommitDate) {
                Add-Result '10' "apk freshness [$label]" 'WARN' "APK built $($apkItem.LastWriteTime.ToString('yyyy-MM-dd HH:mm:ss')) predates HEAD commit $($headCommitDate.ToString('yyyy-MM-dd HH:mm:ss')); mtime is checkout-dependent - trust content gates 07-09 ($rel)"
            } else {
                Add-Result '10' "apk freshness [$label]" 'PASS' "built $($apkItem.LastWriteTime.ToString('yyyy-MM-dd HH:mm:ss'))"
            }
        } else {
            Add-Result '10' "apk freshness [$label]" 'SKIP' 'HEAD commit date unavailable'
        }
    }
}

# -------------------------------------------------------- gate 11 release keys ---

$stagingFindings = @()
foreach ($stage in @('dist','dist-desktop')) {
    $stagePath = Join-Path $RepoRoot $stage
    if (-not (Test-Path $stagePath)) { continue }
    $leaked = Get-ChildItem -Path $stagePath -Recurse -File -Include '*.key','*.pem','*.crt','*.log','.env*' -ErrorAction SilentlyContinue
    foreach ($l in $leaked) { $stagingFindings += $l.FullName.Replace($RepoRoot,'').TrimStart('\','/') }
}
if ($stagingFindings.Count -eq 0) {
    Add-Result '11' 'no key/log material in release staging' 'PASS' '-'
} else {
    Add-Result '11' 'no key/log material in release staging' 'FAIL' (($stagingFindings | Select-Object -First 6) -join ', ')
}

# ---------------------------------------------------------- gate 12 hygiene ---

# 12a PIN must not be logged
$pinHits = @()
$ktFiles = Get-ChildItem -Path (Join-Path $RepoRoot 'mobile/android-host/src') -Recurse -File -Filter '*.kt' -ErrorAction SilentlyContinue
foreach ($f in $ktFiles) {
    $ln = 0
    foreach ($line in (Get-Content $f.FullName -ErrorAction SilentlyContinue)) {
        $ln++
        if ($line -match 'Log\.[a-z]+\(' -and $line -match '\$pin\b|\$\{pin\}') {
            $pinHits += "$($f.Name):$ln"
        }
    }
}
if ($pinHits.Count -eq 0) { Add-Result '12' 'PIN not written to log' 'PASS' 'no Log.* $pin' }
else { Add-Result '12' 'PIN not written to log' 'FAIL' ($pinHits -join ', ') }

# 12b fetch() only in api/client.ts
$fetchHits = @()
$tsFiles = Get-ChildItem -Path (Join-Path $RepoRoot 'frontend/src') -Recurse -File -Include '*.ts','*.tsx' -ErrorAction SilentlyContinue
foreach ($f in $tsFiles) {
    $ln = 0
    foreach ($line in (Get-Content $f.FullName -ErrorAction SilentlyContinue)) {
        $ln++
        if ($line -match '\bfetch\(' -and $f.Name -ne 'client.ts') {
            $fetchHits += "$($f.Name):$ln"
        }
    }
}
if ($fetchHits.Count -eq 0) { Add-Result '12b' 'fetch() only in api/client.ts' 'PASS' '-' }
else { Add-Result '12b' 'fetch() only in api/client.ts' 'FAIL' ($fetchHits -join ', ') }

# 12c stop() must not use a bare startService
$svcFile = Join-Path $RepoRoot 'mobile/android-host/src/main/kotlin/app/hushdrop/host/HostForegroundService.kt'
if (Test-Path $svcFile) {
    $svcText = Get-Content $svcFile -Raw
    $stopIdx = $svcText.IndexOf('fun stop(context: Context)')
    if ($stopIdx -ge 0) {
        $window = $svcText.Substring($stopIdx)
        $endIdx = $window.IndexOf('override fun onBind')
        if ($endIdx -gt 0) { $window = $window.Substring(0, $endIdx) }
        if ($window -match 'startForegroundService') {
            Add-Result '12c' 'stop() without bare startService' 'PASS' 'uses startForegroundService on API 26+'
        } elseif ($window -match '\bstartService\(') {
            Add-Result '12c' 'stop() without bare startService' 'FAIL' 'stop() still calls context.startService()'
        } else {
            Add-Result '12c' 'stop() without bare startService' 'PASS' 'no startService in stop()'
        }
    } else {
        Add-Result '12c' 'stop() without bare startService' 'SKIP' 'stop() not found'
    }
} else {
    Add-Result '12c' 'stop() without bare startService' 'SKIP' 'service file missing'
}

# 12d curated android tracking is not enforceable by ignore rules alone
$curated = @(
    'mobile/android/app/build.gradle',
    'mobile/android/app/src/main/AndroidManifest.xml',
    'mobile/android/app/src/main/java/app/hushdrop/MainActivity.java',
    'mobile/android/app/src/main/res/xml/network_security_config.xml'
)
$trackedCurated = @($tracked | Where-Object { $_ -like 'mobile/android/*' })
$rootIgnore = Join-Path $RepoRoot '.gitignore'
$parentExcluded = $false
if (Test-Path $rootIgnore) {
    foreach ($line in (Get-Content $rootIgnore)) {
        if ($line.Trim() -eq 'mobile/android/') { $parentExcluded = $true; break }
    }
}
if ($parentExcluded) {
    Add-Result '12d' 'android tracking enforceable' 'WARN' "root .gitignore excludes 'mobile/android/' (a directory), so negations in mobile/.gitignore cannot re-include files; the $($trackedCurated.Count) curated file(s) survive only via 'git add -f'"
} else {
    Add-Result '12d' 'android tracking enforceable' 'PASS' "$($trackedCurated.Count) curated file(s) tracked"
}

# ----------------------------------------------------------------- summary ---

Pop-Location

$fails = @($script:Results | Where-Object { $_.Status -eq 'FAIL' })
$warns = @($script:Results | Where-Object { $_.Status -eq 'WARN' })
$skips = @($script:Results | Where-Object { $_.Status -eq 'SKIP' })

Write-Host ''
$script:Results | Sort-Object Id | Format-Table -AutoSize -Property Id, Check, Status, Detail

$verdict = 'PASS'
if ($fails.Count -gt 0) { $verdict = 'FAIL' }
elseif ($warns.Count -gt 0) { $verdict = 'PASS WITH NOTES' }

$color = 'Green'
if ($verdict -eq 'FAIL') { $color = 'Red' }
elseif ($verdict -eq 'PASS WITH NOTES') { $color = 'Yellow' }

Write-Host ''
Write-Host "VERDICT: $verdict   (FAIL=$($fails.Count) WARN=$($warns.Count) SKIP=$($skips.Count))" -ForegroundColor $color
Write-Host "  HEAD: $head" -ForegroundColor Gray
Write-Host "  latest tag: $latestTag" -ForegroundColor Gray
Write-Host ''

if ($fails.Count -gt 0) { exit 1 }
exit 0
