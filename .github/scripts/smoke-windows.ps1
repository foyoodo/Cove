# Launch Cove.exe from the preview zip, wait for the main window, take a
# screenshot, and close the app. A missing window, a hang, or a non-zero
# exit fails the job.
#
# Usage: smoke-windows.ps1 -Archive Cove-<ver>-windows-amd64.zip -Screenshot windows-smoke.png

param(
    [Parameter(Mandatory = $true)][string]$Archive,
    [string]$Screenshot = "windows-smoke.png"
)

$ErrorActionPreference = "Stop"

if (-not (Test-Path -LiteralPath $Archive)) {
    throw "missing archive $Archive"
}

Add-Type @"
using System;
using System.Runtime.InteropServices;
public static class CoveNative {
  public struct RECT { public int Left; public int Top; public int Right; public int Bottom; }
  [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr hWnd, out RECT lpRect);
  [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr hWnd);
  [DllImport("user32.dll")] public static extern bool SystemParametersInfo(uint uiAction, uint uiParam, ref RECT pvParam, uint fWinIni);
  public const uint SPI_GETWORKAREA = 0x0030;
}
"@

Add-Type -AssemblyName System.Drawing
Add-Type -AssemblyName System.Windows.Forms

$work = Join-Path ([System.IO.Path]::GetTempPath()) ("cove-smoke-" + [guid]::NewGuid().ToString("n"))
New-Item -ItemType Directory -Path $work | Out-Null
try {
    Expand-Archive -LiteralPath $Archive -DestinationPath $work -Force
    $exe = Join-Path $work "Cove.exe"
    if (-not (Test-Path -LiteralPath $exe)) {
        throw "archive has no Cove.exe"
    }
    $icns = Get-ChildItem -LiteralPath $work -Recurse -Filter *.icns -ErrorAction SilentlyContinue
    if ($icns) {
        throw "windows zip ships $($icns.Name); Windows does not use .icns (embedded PNG + cove.ico)"
    }

    $proc = Start-Process -FilePath $exe -PassThru -WorkingDirectory $work
    $hwnd = [IntPtr]::Zero
    $deadline = (Get-Date).AddSeconds(45)
    while ((Get-Date) -lt $deadline) {
        if ($proc.HasExited) {
            throw "Cove.exe exited before a window appeared (code $($proc.ExitCode))"
        }
        $proc.Refresh()
        if ($proc.MainWindowHandle -ne [IntPtr]::Zero) {
            $hwnd = $proc.MainWindowHandle
            break
        }
        Start-Sleep -Milliseconds 400
    }
    if ($hwnd -eq [IntPtr]::Zero) {
        throw "no Cove window after 45s (title='$($proc.MainWindowTitle)')"
    }

    [void][CoveNative]::SetForegroundWindow($hwnd)
    Start-Sleep -Seconds 1

    $rect = New-Object CoveNative+RECT
    if (-not [CoveNative]::GetWindowRect($hwnd, [ref]$rect)) {
        throw "GetWindowRect failed"
    }
    $w = [Math]::Max(1, $rect.Right - $rect.Left)
    $h = [Math]::Max(1, $rect.Bottom - $rect.Top)
    $bmp = New-Object System.Drawing.Bitmap $w, $h
    $g = [System.Drawing.Graphics]::FromImage($bmp)
    try {
        $g.CopyFromScreen($rect.Left, $rect.Top, 0, 0, (New-Object System.Drawing.Size $w, $h))
        $dir = Split-Path -Parent $Screenshot
        if ($dir -and -not (Test-Path -LiteralPath $dir)) {
            New-Item -ItemType Directory -Path $dir | Out-Null
        }
        $bmp.Save($Screenshot, [System.Drawing.Imaging.ImageFormat]::Png)
    }
    finally {
        $g.Dispose()
        $bmp.Dispose()
    }

    $spi = New-Object CoveNative+RECT
    if (-not [CoveNative]::SystemParametersInfo([CoveNative]::SPI_GETWORKAREA, 0, [ref]$spi, 0)) {
        throw "SPI_GETWORKAREA failed"
    }
    $working = [System.Windows.Forms.Screen]::PrimaryScreen.WorkingArea
    Write-Host ("windows window rect L={0} T={1} R={2} B={3} ({4}x{5})" -f $rect.Left, $rect.Top, $rect.Right, $rect.Bottom, $w, $h)
    Write-Host ("SPI_GETWORKAREA     L={0} T={1} R={2} B={3} ({4}x{5})" -f $spi.Left, $spi.Top, $spi.Right, $spi.Bottom, ($spi.Right - $spi.Left), ($spi.Bottom - $spi.Top))
    Write-Host ("Screen.WorkingArea  L={0} T={1} R={2} B={3} ({4}x{5})" -f $working.Left, $working.Top, $working.Right, $working.Bottom, $working.Width, $working.Height)
    if ($rect.Left -lt $spi.Left -or $rect.Top -lt $spi.Top -or $rect.Right -gt $spi.Right -or $rect.Bottom -gt $spi.Bottom) {
        throw "window rect is outside SPI_GETWORKAREA"
    }
    if ($rect.Left -lt $working.Left -or $rect.Top -lt $working.Top -or $rect.Right -gt $working.Right -or $rect.Bottom -gt $working.Bottom) {
        throw "window rect is outside Screen.WorkingArea"
    }

    if (-not $proc.CloseMainWindow()) {
        throw "CloseMainWindow failed"
    }
    if (-not $proc.WaitForExit(20000)) {
        throw "Cove.exe did not exit after CloseMainWindow"
    }
    if ($proc.ExitCode -ne 0) {
        throw "Cove.exe exited $($proc.ExitCode)"
    }
    Write-Host "windows smoke ok; screenshot $Screenshot"
}
finally {
    if ($proc -and -not $proc.HasExited) {
        try { $proc.Kill() } catch { }
    }
    Remove-Item -LiteralPath $work -Recurse -Force -ErrorAction SilentlyContinue
}
