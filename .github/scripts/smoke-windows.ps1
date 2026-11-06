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
