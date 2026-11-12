# How it works

nodoff is one Windows exe, written in Go with no cgo. It has four parts.

```
                 DDC/CI (dxva2.dll)            MQTT
  monitors  <----------------------  nodoff  <------>  broker  <-->  Home Assistant
                                       |
                              tray icon, settings panel
                              (127.0.0.1, Edge app window)
```

## Monitors

DDC/CI is a control channel in HDMI, DisplayPort and DVI cables. Windows exposes it through `dxva2.dll`. nodoff uses two VCP codes from the MCCS standard:

| Code | Meaning | Values nodoff sends |
|---|---|---|
| `10` | Brightness | 0 up to the monitor's own maximum |
| `D6` | Power mode | `01` on, `04` standby, `05` off |

Each call takes about 50 ms, and monitors garble messages that overlap. So all traffic goes through one session at a time (`internal/ddc`), with the pause the standard asks for after a write.

Windows gives a DDC/CI handle for each display but no stable name. nodoff matches each handle to its device with `EnumDisplayDevices`, reads the EDID from the registry and builds an ID from the maker, product code and serial, such as `len65fa_v9k27xqa`. The ID survives reboots and cable swaps, and two monitors of the same model get different IDs.

A poller re-reads every monitor on the interval from the General tab, so changes made with the monitor's buttons reach MQTT.

## MQTT

`internal/bridge` holds the connection (Eclipse Paho). It publishes retained state, subscribes to two command topics per PC and drops anything that isn't a well-formed command for a published monitor. A run of brightness commands for one monitor, as a dragged slider sends, collapses to the last one.

The last will marks the PC offline if it loses power or the network. With discovery on, nodoff publishes one Home Assistant light config per monitor and repeats it when Home Assistant restarts.

## Settings panel

The panel is one HTML page embedded in the exe. nodoff serves it on a random `127.0.0.1` port and opens it in an Edge app window. Requests need the right `Host` header, the right origin and a random key that changes every start. The page runs under a Content Security Policy with a per-load nonce and builds its DOM without `innerHTML`.

Settings are in `%LOCALAPPDATA%\nodoff\settings.json`. Every value is checked before it's saved (`internal/settings`). The MQTT password is the exception: it goes to Windows Credential Manager and the panel can't read it back.

## Lock and wake

Both features follow the power commands nodoff has sent, because a sleeping monitor often doesn't answer when asked for its state.

- **Lock.** When the last connected monitor goes to sleep, a timer starts. If no monitor has been woken through nodoff when it ends, nodoff calls `LockWorkStation`.
- **Double-tap Ctrl.** A low-level keyboard hook, installed only while the setting is on, times Ctrl presses. On the lock screen no app can see keys, so nodoff watches the time of the last input (`GetLastInputInfo`) and wakes the screens when it changes.

## Files

| Path | What |
|---|---|
| `main_windows.go` | Start-up, MQTT bridge lifecycle, poller, tray |
| `monitors_windows.go` | Scan, read and set monitors; state for the panel and MQTT |
| `ui_windows.go`, `ui.html` | The settings panel and its API |
| `wake_windows.go`, `ctrltap.go` | Lock after sleep, double-tap Ctrl |
| `platform_windows.go` | Single instance, start at sign-in, Credential Manager |
| `internal/ddc` | DDC/CI, EDID and capabilities parsing |
| `internal/bridge` | MQTT topics, discovery, command parsing |
| `internal/settings` | `settings.json` and its checks |
| `internal/logo` | The logo, drawn in code |
| `tools/` | Exe resources, logo PNGs, a mock of the panel's API |
