# Changelog

## 0.2.1 (2026-11-12)

First public release.

- New setting: lock the PC a set number of minutes after every screen has gone to sleep.
- New setting: double-tap Ctrl to wake the screens. On the lock screen, any key or mouse movement wakes them. Both settings are off by default and have had little testing so far.
- Opening settings while the panel is already open brings that window to the front.
- Edge, Explorer and the settings folder are now found through the Windows API and not through environment variables.
- Docs: Home Assistant set-up, troubleshooting, how it works, a security policy and an MIT licence.

## 0.2.0, 0.1.1 and 0.1.0

Development versions, never published.

- Finds monitors over DDC/CI and names them from their EDID (model, serial, left or right).
- Publishes each chosen monitor to MQTT as a Home Assistant light: on/off and brightness.
- Settings panel (General, Devices, MQTT, About), tray icon, start at sign-in.
- MQTT password kept in Windows Credential Manager.
