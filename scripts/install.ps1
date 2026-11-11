# Install nodoff for the current Windows user. No admin rights needed, and none used.
#
#   powershell -ExecutionPolicy Bypass -File install.ps1 [-NoStart] [-NoStartup] [-Uninstall [-RemoveSettings]]
#
# Put this script next to nodoff.exe (both come in the release zip) and run it.
# It copies the exe to %LOCALAPPDATA%\Programs\nodoff, starts it when you sign in,
# adds a Start menu entry and starts it. Running it again upgrades in place and keeps
# your settings.

param(
  [switch]$NoStart,         # don't start nodoff after installing (used by remote installs)
  [switch]$NoStartup,       # don't add the sign-in entry
  [switch]$Uninstall,
  [switch]$RemoveSettings   # with -Uninstall: also delete settings, the log and the saved MQTT password
)
$ErrorActionPreference = "Stop"

$principal = [Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()
if ($principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator) -and -not $env:SSH_CLIENT) {
  # An elevated run would install into the admin profile's folders and registry, not yours.
  Write-Warning "This PowerShell is running as administrator. nodoff installs per user and needs no admin rights; run it from a normal PowerShell."
}

$dir  = Join-Path $env:LOCALAPPDATA "Programs\nodoff"
$data = Join-Path $env:LOCALAPPDATA "nodoff"
$exe  = Join-Path $dir "nodoff.exe"
$run  = "HKCU:\Software\Microsoft\Windows\CurrentVersion\Run"
$lnk  = Join-Path $env:APPDATA "Microsoft\Windows\Start Menu\Programs\nodoff.lnk"

function Stop-Nodoff {
  Get-Process nodoff -ErrorAction SilentlyContinue | Where-Object { $_.Path -eq $exe } | Stop-Process -Force
  Start-Sleep -Milliseconds 1500   # Windows can hold the exe for a moment after it exits
}

if ($Uninstall) {
  Stop-Nodoff
  Remove-ItemProperty -Path $run -Name nodoff -ErrorAction SilentlyContinue
  Remove-Item $lnk -ErrorAction SilentlyContinue
  Remove-Item $dir -Recurse -Force -ErrorAction SilentlyContinue
  if ($RemoveSettings) {
    Remove-Item $data -Recurse -Force -ErrorAction SilentlyContinue
    cmdkey /delete:nodoff/mqtt 2>&1 | Out-Null
    "nodoff, its settings and its saved MQTT password are removed."
  } else {
    "nodoff is uninstalled. Settings are still in $data. Run with -Uninstall -RemoveSettings to remove them and the saved MQTT password."
  }
  "Home Assistant keeps the monitors until you delete the device there, or turn off discovery in nodoff before uninstalling."
  return
}

$src = Join-Path $PSScriptRoot "nodoff.exe"
if (-not (Test-Path $src)) { throw "nodoff.exe isn't next to this script ($PSScriptRoot)." }

New-Item -ItemType Directory -Force $dir | Out-Null
Stop-Nodoff
Copy-Item $src $exe -Force
Unblock-File $exe   # downloaded files carry a mark that makes Windows ask before every start

if (-not $NoStartup) {
  # Per-user Run entry, quoted so the path can't be misread. Starts quietly in the tray.
  Set-ItemProperty -Path $run -Name nodoff -Value "`"$exe`" --background"
}

$sh = New-Object -ComObject WScript.Shell
$sc = $sh.CreateShortcut($lnk)
$sc.TargetPath = $exe
$sc.WorkingDirectory = $dir
$sc.IconLocation = "$exe,0"
$sc.Description = "nodoff settings"
$sc.Save()

$ver = (Get-Item $exe).VersionInfo.ProductVersion
if (-not $NoStart) {
  Start-Process $exe -WorkingDirectory $dir
  "nodoff $ver is installed in $dir and settings are opening."
} else {
  "nodoff $ver is installed in $dir."
}
