# nodoff

Windows tray app that exposes monitor brightness and power (DDC/CI) over MQTT, with Home Assistant discovery. Go, no cgo, built on Linux and cross-compiled. Read `README.md` for what it does and `TODO.md` for what's next.

Machine-specific notes (test PC, deploy host, broker) are in `CLAUDE.local.md`, which is gitignored. Keep host names, LAN addresses and personal names out of every tracked file: this repo may go public.

## Layout

| Path | What |
|---|---|
| `main_windows.go` | Start-up, MQTT bridge lifecycle, poller, tray |
| `monitors_windows.go` | Monitor service: scan, read, set, state for the panel and MQTT |
| `ui_windows.go`, `ui.html` | Settings panel: HTTP server on a random loopback port, one embedded page |
| `platform_windows.go` | Single instance, start at sign-in (HKCU Run), MQTT password in Credential Manager |
| `internal/ddc` | DDC/CI through dxva2, EDID and capabilities parsing (parsers are platform-neutral and tested) |
| `internal/bridge` | MQTT connection, topics, HA discovery, command parsing |
| `internal/settings` | settings.json with validation |
| `internal/logo` | The logo, drawn in code |
| `tools/mkrsrc`, `tools/logopreview`, `tools/ui-mock` | Exe resources, logo PNGs, panel mock for screenshots |
| `scripts/` | `build.sh`, `deploy-ssh.sh HOST`, `install.ps1` |

## Rules

- British English in UI strings, comments and docs. Apply the unslop skill to prose.
- Security is a feature here. Keep: no admin rights, System32-only DLLs, password only in Credential Manager, panel guard (Host, Origin, key, JSON content type), CSP with nonce, no `innerHTML` with data, validated MQTT topics and payloads. Nothing from a request may reach a command line.
- Run `./scripts/build.sh` (vet for both OSes, tests, build) before calling something done, and `GOOS=windows govulncheck ./...` after any dependency change.
- Bump `version.go` and `CHANGELOG.md` for each release. A `v*` tag builds a draft GitHub release.
- DDC/CI calls are slow (about 50 ms each) and must not overlap: everything goes through `ddc.Open()`, which holds a lock until `Close()`. Don't publish to MQTT while holding a session.
- Processes started over SSH run outside the desktop, where there's no tray and no monitors. `deploy-ssh.sh` starts nodoff through a one-off scheduled task in the user's session; do the same for any manual start.
