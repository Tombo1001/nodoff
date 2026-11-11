#!/usr/bin/env bash
# Build nodoff and install it on a Windows PC over OpenSSH, then start it in the
# signed-in user's desktop session.
#
#   scripts/deploy-ssh.sh <ssh-host>
#
# Needs: Windows OpenSSH server with PowerShell as the default shell, and the SSH
# user signed in at the console (the tray and DDC/CI only work in that session).
set -euo pipefail
host=${1:?usage: deploy-ssh.sh <ssh-host>}
cd "$(dirname "$0")/.."
./scripts/build.sh
quiet() { grep -v -e 'post-quantum' -e 'store now' -e 'upgraded' -e 'pq.html' || true; }

ssh "$host" 'New-Item -ItemType Directory -Force "$env:TEMP\nodoff-deploy" | Out-Null' 2>&1 | quiet
scp -q dist/nodoff.exe scripts/install.ps1 "$host:AppData/Local/Temp/nodoff-deploy/" 2>&1 | quiet
# Install without starting (a process started over SSH would run outside the desktop),
# then start it through a one-off scheduled task in the user's interactive session.
ssh "$host" '
  $d = "$env:TEMP\nodoff-deploy"
  powershell -NoProfile -ExecutionPolicy Bypass -File "$d\install.ps1" -NoStart
  $exe = Join-Path $env:LOCALAPPDATA "Programs\nodoff\nodoff.exe"
  $a = New-ScheduledTaskAction -Execute $exe -Argument "--background"
  $p = New-ScheduledTaskPrincipal -UserId $env:USERNAME -LogonType Interactive -RunLevel Limited
  Register-ScheduledTask -TaskName "nodoff-deploy-start" -Action $a -Principal $p -Force | Out-Null
  Start-ScheduledTask -TaskName "nodoff-deploy-start"
  Start-Sleep 3
  Unregister-ScheduledTask -TaskName "nodoff-deploy-start" -Confirm:$false
  Remove-Item $d -Recurse -Force
  $log = Join-Path $env:LOCALAPPDATA "nodoff\nodoff.log"
  if (Test-Path $log) { Get-Content $log -Tail 5 }
' 2>&1 | quiet
