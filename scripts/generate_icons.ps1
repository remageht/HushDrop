$cs = @"
using System;
using System.Drawing;
using System.Drawing.Drawing2D;
using System.Drawing.Imaging;
using System.IO;

public static class IconRenderer {
    public static void Render(int size, string path, bool transparent, float contentScale) {
        using (var bmp = new Bitmap(size, size, PixelFormat.Format32bppArgb)) {
            using (var g = Graphics.FromImage(bmp)) {
                g.SmoothingMode = SmoothingMode.AntiAlias;
                g.InterpolationMode = InterpolationMode.HighQualityBicubic;
                g.PixelOffsetMode = PixelOffsetMode.HighQuality;

                Color bgColor = Color.FromArgb(255, 11, 15, 25);      // #0b0f19
                Color circleColor = Color.FromArgb(255, 30, 41, 59);  // #1e293b
                Color gradStart = Color.FromArgb(255, 16, 185, 129);  // #10b981
                Color gradEnd = Color.FromArgb(255, 6, 95, 70);      // #065f46

                if (!transparent) {
                    using (var brush = new SolidBrush(bgColor)) {
                        g.FillRectangle(brush, 0, 0, size, size);
                    }
                } else {
                    g.Clear(Color.Transparent);
                }

                float scale = (size / 100.0f) * contentScale;
                float offsetX = (size - (100.0f * scale)) / 2.0f;
                float offsetY = (size - (100.0f * scale)) / 2.0f;

                // Outer decorative ring
                using (var circlePen = new Pen(circleColor, Math.Max(2.0f, 2.0f * scale))) {
                    g.DrawEllipse(circlePen, offsetX + (12.0f * scale), offsetY + (12.0f * scale), 76.0f * scale, 76.0f * scale);
                }

                float strokeW = Math.Max(3.0f, 7.0f * scale);

                // Emerald arrow with gradient
                float arrowTop = offsetY + (24.0f * scale);
                float arrowBottom = offsetY + (62.0f * scale);
                var gradRect = new RectangleF(offsetX, arrowTop, 100.0f * scale, Math.Max(1.0f, arrowBottom - arrowTop));
                using (var gradBrush = new LinearGradientBrush(gradRect, gradStart, gradEnd, LinearGradientMode.Vertical)) {
                    using (var arrowPen = new Pen(gradBrush, strokeW)) {
                        arrowPen.StartCap = LineCap.Round;
                        arrowPen.EndCap = LineCap.Round;
                        arrowPen.LineJoin = LineJoin.Round;

                        // Vertical stem
                        g.DrawLine(arrowPen, (50.0f * scale) + offsetX, (24.0f * scale) + offsetY, (50.0f * scale) + offsetX, (62.0f * scale) + offsetY);

                        // Chevron head
                        var points = new PointF[] {
                            new PointF((34.0f * scale) + offsetX, (46.0f * scale) + offsetY),
                            new PointF((50.0f * scale) + offsetX, (62.0f * scale) + offsetY),
                            new PointF((66.0f * scale) + offsetX, (46.0f * scale) + offsetY)
                        };
                        g.DrawLines(arrowPen, points);
                    }
                }

                // Bottom horizontal baseline
                using (var lineBrush = new SolidBrush(gradStart)) {
                    using (var bottomPen = new Pen(lineBrush, strokeW)) {
                        bottomPen.StartCap = LineCap.Round;
                        bottomPen.EndCap = LineCap.Round;
                        g.DrawLine(bottomPen, (30.0f * scale) + offsetX, (74.0f * scale) + offsetY, (70.0f * scale) + offsetX, (74.0f * scale) + offsetY);
                    }
                }
            }

            string dir = Path.GetDirectoryName(path);
            if (!string.IsNullOrEmpty(dir) && !Directory.Exists(dir)) {
                Directory.CreateDirectory(dir);
            }
            bmp.Save(path, ImageFormat.Png);
        }
    }

    public static void RenderSolidBackground(int size, string path) {
        using (var bmp = new Bitmap(size, size, PixelFormat.Format32bppArgb)) {
            using (var g = Graphics.FromImage(bmp)) {
                Color bgColor = Color.FromArgb(255, 11, 15, 25);
                using (var brush = new SolidBrush(bgColor)) {
                    g.FillRectangle(brush, 0, 0, size, size);
                }
            }
            string dir = Path.GetDirectoryName(path);
            if (!string.IsNullOrEmpty(dir) && !Directory.Exists(dir)) {
                Directory.CreateDirectory(dir);
            }
            bmp.Save(path, ImageFormat.Png);
        }
    }
}
"@

if (-not ([System.Management.Automation.PSTypeName]'IconRenderer').Type) {
    Add-Type -TypeDefinition $cs -ReferencedAssemblies System.Drawing
}

Write-Host "=== 1. Generating Mobile Assets in mobile/assets/ ===" -ForegroundColor Cyan
[IconRenderer]::Render(1024, "mobile/assets/icon.png", $false, 0.90)
[IconRenderer]::Render(1024, "mobile/assets/icon-only.png", $true, 0.90)
# Adaptive foreground with safe-zone 25% (content scale 0.70 ensures arrow is within 72dp circle of 108dp)
[IconRenderer]::Render(1024, "mobile/assets/icon-foreground.png", $true, 0.70)
[IconRenderer]::RenderSolidBackground(1024, "mobile/assets/icon-background.png")

# Splash screens
[IconRenderer]::Render(2732, "mobile/assets/splash.png", $false, 0.35)
[IconRenderer]::Render(2732, "mobile/assets/splash-dark.png", $false, 0.35)

Write-Host "=== 2. Generating PWA Icons in frontend/public/ ===" -ForegroundColor Cyan
[IconRenderer]::Render(512, "frontend/public/icon-512.png", $false, 0.90)
[IconRenderer]::Render(192, "frontend/public/icon-192.png", $false, 0.90)
[IconRenderer]::Render(180, "frontend/public/apple-touch-icon.png", $false, 0.90)
[IconRenderer]::Render(64, "frontend/public/favicon.png", $false, 0.90)

Write-Host "=== All icon assets rendered successfully! ===" -ForegroundColor Green
