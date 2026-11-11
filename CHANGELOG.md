# Changelog

## 0.1.0 (unreleased)

First version.

- Finds monitors over DDC/CI and names them from their EDID (model, serial, left or right).
- Publishes each chosen monitor to MQTT as a Home Assistant light: on/off and brightness.
- Settings panel (General, Devices, MQTT, About), tray icon, start at sign-in.
- MQTT password kept in Windows Credential Manager.
