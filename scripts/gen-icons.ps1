# HushDrop brand icon generator.
# Draws the HushDrop mark (ring + falling arrow + drop + baseline) and renders
# every size needed: PWA icons, Tauri source icon, Windows ICO size set and
# Android launcher mipmaps (legacy, round and adaptive foreground).
# Deterministic: fully scripted so every release can rebuild identical assets.

$ErrorActionPreference = "Stop"
Add-Type -AssemblyName System.Drawing

$rootDir = Resolve-Path "$PSScriptRoot\.."
$brandDir = Join-Path $rootDir "assets\brand"
$icoDir = Join-Path $brandDir "ico"
$launcherDir = Join-Path $rootDir "mobile\assets\launcher"
$frontendPublic = Join-Path $rootDir "frontend\public"

foreach ($d in @($brandDir, $icoDir, $launcherDir, $frontendPublic)) {
    if (!(Test-Path $d)) { New-Item -ItemType Directory -Path $d -Force | Out-Null }
}

function New-Canvas([int]$w, [int]$h) {
    $bmp = New-Object System.Drawing.Bitmap($w, $h, [System.Drawing.Imaging.PixelFormat]::Format32bppArgb)
    $g = [System.Drawing.Graphics]::FromImage($bmp)
    $g.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::AntiAlias
    $g.InterpolationMode = [System.Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
    $g.PixelOffsetMode = [System.Drawing.Drawing2D.PixelOffsetMode]::HighQuality
    return @{ Bitmap = $bmp; Graphics = $g }
}

function New-RoundedPath([single]$x, [single]$y, [single]$w, [single]$h, [single]$r) {
    $p = New-Object System.Drawing.Drawing2D.GraphicsPath
    $p.AddArc($x, $y, 2 * $r, 2 * $r, 180, 90)
    $p.AddArc($x + $w - 2 * $r, $y, 2 * $r, 2 * $r, 270, 90)
    $p.AddArc($x + $w - 2 * $r, $y + $h - 2 * $r, 2 * $r, 2 * $r, 0, 90)
    $p.AddArc($x, $y + $h - 2 * $r, 2 * $r, 2 * $r, 90, 90)
    $p.CloseFigure()
    return $p
}

function New-VGradient([single]$x, [single]$y, [single]$w, [single]$h) {
    $c1 = [System.Drawing.Color]::FromArgb(255, 0x34, 0xD3, 0x99)   # #34D399
    $c2 = [System.Drawing.Color]::FromArgb(255, 0x06, 0x5F, 0x46)   # #065F46
    $b = New-Object System.Drawing.Drawing2D.LinearGradientBrush(
        (New-Object System.Drawing.PointF($x, $y)),
        (New-Object System.Drawing.PointF($x, ($y + $h))),
        $c1, $c2)
    return $b
}

# mode: launcher (bg rounded square), round (bg circle), fg (no bg, scaled for adaptive icon)
function Draw-Icon([System.Drawing.Graphics]$g, [int]$size, [string]$mode) {
    $u = $size / 100.0

    if ($mode -eq "fg") {
        # Adaptive icon foreground: content must fit the 66-unit safe circle.
        $g.TranslateTransform($size / 2.0, $size / 2.0)
        $g.ScaleTransform(0.70, 0.70)
        $g.TranslateTransform(-$size / 2.0, -$size / 2.0)
    }

    # Background tile
    if ($mode -ne "fg") {
        $bgBrush = New-Object System.Drawing.SolidBrush([System.Drawing.Color]::FromArgb(255, 0x0B, 0x0F, 0x19))
        if ($mode -eq "round") {
            $bgPath = New-Object System.Drawing.Drawing2D.GraphicsPath
            $bgPath.AddEllipse(0, 0, $size, $size)
        } else {
            $r = 24 * $u
            $bgPath = New-RoundedPath 0 0 $size $size $r
        }
        $g.FillPath($bgBrush, $bgPath)
        $bgPath.Dispose()
        $bgBrush.Dispose()

        # Subtle center glow
        $glow = New-Object System.Drawing.SolidBrush([System.Drawing.Color]::FromArgb(22, 0x10, 0xB9, 0x81))
        $g.FillEllipse($glow, (22 * $u), (18 * $u), (56 * $u), (56 * $u))
        $glow.Dispose()
    }

    # Ring (inactive track)
    $ringPen = New-Object System.Drawing.Pen([System.Drawing.Color]::FromArgb(255, 0x1E, 0x29, 0x3B), (2.2 * $u))
    $ringPen.StartCap = [System.Drawing.Drawing2D.LineCap]::Round
    $ringPen.EndCap = [System.Drawing.Drawing2D.LineCap]::Round
    $g.DrawEllipse($ringPen, (12 * $u), (8 * $u), (76 * $u), (76 * $u))
    $ringPen.Dispose()

    # Arrow: shaft + chevron head (vertical gradient)
    $arrowBrush = New-VGradient (36 * $u) (20 * $u) (28 * $u) (32 * $u)
    $arrowPen = New-Object System.Drawing.Pen($arrowBrush, (7 * $u))
    $arrowPen.StartCap = [System.Drawing.Drawing2D.LineCap]::Round
    $arrowPen.EndCap = [System.Drawing.Drawing2D.LineCap]::Round
    $arrowPen.LineJoin = [System.Drawing.Drawing2D.LineJoin]::Round

    $shaft = @( (New-Object System.Drawing.PointF((50 * $u), (20 * $u))),
                (New-Object System.Drawing.PointF((50 * $u), (52 * $u))) )
    $g.DrawLines($arrowPen, $shaft)
    $head = @( (New-Object System.Drawing.PointF((36 * $u), (38 * $u))),
               (New-Object System.Drawing.PointF((50 * $u), (52 * $u))),
               (New-Object System.Drawing.PointF((64 * $u), (38 * $u))) )
    $g.DrawLines($arrowPen, $head)
    $arrowPen.Dispose()
    $arrowBrush.Dispose()

    # The HushDrop: teardrop under the arrowhead
    $dropBrush = New-VGradient (42.8 * $u) (55.5 * $u) (14.4 * $u) (17.2 * $u)
    $dropPath = New-Object System.Drawing.Drawing2D.GraphicsPath
    $tipX = 50 * $u; $tipY = 55.5 * $u
    $rx = 57.2 * $u; $by = 65.5 * $u; $r = 7.2 * $u; $lx = 42.8 * $u
    $dropPath.AddBezier($tipX, $tipY, (55.0 * $u), (60.5 * $u), $rx, (62.5 * $u), $rx, $by)
    $dropPath.AddArc(($lx), ($by - $r), (2 * $r), (2 * $r), 0, 180)
    $dropPath.AddBezier($lx, $by, $lx, (62.5 * $u), (45.0 * $u), (60.5 * $u), $tipX, $tipY)
    $dropPath.CloseFigure()
    $g.FillPath($dropBrush, $dropPath)
    $dropPath.Dispose()
    $dropBrush.Dispose()

    # Highlight on the drop
    $hl = New-Object System.Drawing.SolidBrush([System.Drawing.Color]::FromArgb(235, 0xA7, 0xF3, 0xD0))
    $g.FillEllipse($hl, (46.3 * $u), (62.3 * $u), (3.0 * $u), (4.0 * $u))
    $hl.Dispose()

    # Baseline (the coaster the drop lands on) — ends kiss the ring exactly
    $barBrush = New-Object System.Drawing.SolidBrush([System.Drawing.Color]::FromArgb(255, 0x10, 0xB9, 0x81))
    $barPen = New-Object System.Drawing.Pen($barBrush, (7 * $u))
    $barPen.StartCap = [System.Drawing.Drawing2D.LineCap]::Round
    $barPen.EndCap = [System.Drawing.Drawing2D.LineCap]::Round
    $g.DrawLine($barPen, (34 * $u), (78 * $u), (66 * $u), (78 * $u))
    $barPen.Dispose()
    $barBrush.Dispose()

    if ($mode -eq "fg") {
        $g.ResetTransform()
    }
}

function Save-Render([string]$mode, [int]$size, [string]$outPath) {
    $c = New-Canvas $size $size
    Draw-Icon $c.Graphics $size $mode
    $c.Bitmap.Save($outPath, [System.Drawing.Imaging.ImageFormat]::Png)
    $c.Graphics.Dispose()
    $c.Bitmap.Dispose()
}

function Resize-Master([System.Drawing.Bitmap]$master, [int]$size, [string]$outPath) {
    $bmp = New-Object System.Drawing.Bitmap($size, $size, [System.Drawing.Imaging.PixelFormat]::Format32bppArgb)
    $g = [System.Drawing.Graphics]::FromImage($bmp)
    $g.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::AntiAlias
    $g.InterpolationMode = [System.Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
    $g.PixelOffsetMode = [System.Drawing.Drawing2D.PixelOffsetMode]::HighQuality
    $g.DrawImage($master, 0, 0, $size, $size)
    $bmp.Save($outPath, [System.Drawing.Imaging.ImageFormat]::Png)
    $g.Dispose()
    $bmp.Dispose()
}

Write-Host "===> Rendering brand icon sources..." -ForegroundColor Cyan

# Master (Tauri source + PWA resize master)
$masterPath = Join-Path $brandDir "icon-1024.png"
$masterC = New-Canvas 1024 1024
Draw-Icon $masterC.Graphics 1024 "launcher"
$masterC.Bitmap.Save($masterPath, [System.Drawing.Imaging.ImageFormat]::Png)

Write-Host "   -> Master icon rendered."

# PWA set
Resize-Master $masterC.Bitmap 512 (Join-Path $frontendPublic "icon-512.png")
Resize-Master $masterC.Bitmap 192 (Join-Path $frontendPublic "icon-192.png")
Resize-Master $masterC.Bitmap 180 (Join-Path $frontendPublic "apple-touch-icon.png")
Resize-Master $masterC.Bitmap 64 (Join-Path $frontendPublic "favicon.png")

# Windows ICO size set (consumed by go-winres during build)
foreach ($s in @(16, 24, 32, 48, 64, 128, 256)) {
    Resize-Master $masterC.Bitmap $s (Join-Path $icoDir ("{0}.png" -f $s))
}

# Android launcher mipmaps (source of truth; deploy via apply-android-custom.js)
$densities = @{
    "mdpi"    = 48
    "hdpi"    = 72
    "xhdpi"   = 96
    "xxhdpi"  = 144
    "xxxhdpi" = 192
}
foreach ($k in $densities.Keys) {
    $dir = Join-Path $launcherDir ("mipmap-" + $k)
    if (!(Test-Path $dir)) { New-Item -ItemType Directory -Path $dir -Force | Out-Null }
    $s = $densities[$k]
    Resize-Master $masterC.Bitmap $s (Join-Path $dir "ic_launcher.png")
    Resize-Master $masterC.Bitmap $s (Join-Path $dir "ic_launcher_round.png")
    # Adaptive foreground capacitor-style: 108dp canvas (2.25x the launcher size), alpha-only
    $fgSize = [int]($s * 2.25)
    $fgc = New-Canvas $fgSize $fgSize
    Draw-Icon $fgc.Graphics $fgSize "fg"
    $fgc.Bitmap.Save((Join-Path $dir "ic_launcher_foreground.png"), [System.Drawing.Imaging.ImageFormat]::Png)
    $fgc.Graphics.Dispose()
    $fgc.Bitmap.Dispose()
}

# Refresh the legacy source assets under mobile/assets with the current art too
Resize-Master $masterC.Bitmap 1024 (Join-Path $rootDir "mobile\assets\icon.png")
Resize-Master $masterC.Bitmap 1024 (Join-Path $rootDir "mobile\assets\icon-only.png")

$masterC.Graphics.Dispose()
$masterC.Bitmap.Dispose()

Write-Host "===> Icons written:"
Write-Host "      assets/brand/icon-1024.png, assets/brand/ico/*.png" 
Write-Host "      frontend/public/{icon-512,icon-192,apple-touch-icon,favicon}.png"
Write-Host "      mobile/assets/launcher/mipmap-*/..."
