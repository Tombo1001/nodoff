# Contributing

Bug reports are the most useful thing you can send, above all for monitors and graphics cards nodoff hasn't met. DDC/CI behaves differently on every monitor. When you report one, include the monitor model, the connection (HDMI, DisplayPort, a dock), the graphics card, and the lines from `nodoff.log` around the problem. The About tab opens the log.

## Building

You need Go (the version in `go.mod` or later) on any OS. There's no cgo and no C compiler.

```bash
./scripts/build.sh                   # vet for Windows and the host, tests, then dist/nodoff.exe
python3 tools/ui-mock/mock.py 8791   # the settings panel with a fake API, at http://127.0.0.1:8791/?k=x
./scripts/deploy-ssh.sh HOST         # build, install on a Windows PC over OpenSSH and start it there
```

The parsers (EDID, capabilities strings, MQTT topics, settings, the double-tap timing) are platform-neutral and have tests that run anywhere. The Windows-only code has none yet.

## Rules for changes

- Keep the security properties in [SECURITY.md](SECURITY.md). No admin rights, nothing from a request on a command line, no `innerHTML` with data in the panel, the password only in Credential Manager.
- All DDC/CI traffic goes through `ddc.Open()`, which holds a lock until `Close()`. Monitors garble messages that overlap. Don't publish to MQTT while holding a session.
- British English in UI text, comments and docs.
- Run `./scripts/build.sh` before you open a pull request. Run `GOOS=windows govulncheck ./...` if you changed a dependency.
- A release bumps `version.go` and `CHANGELOG.md`. Pushing a `v*` tag builds a draft release.

`CLAUDE.md` holds the same notes in the form AI coding agents read.
