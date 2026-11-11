# Changelog

## 0.2.0 (unreleased)

- New setting: lock the PC a set number of minutes after every screen has gone to sleep.
- New setting: double-tap Ctrl to wake the screens. On the lock screen, any key or mouse movement wakes them.

## 0.1.1 (unreleased)

- Opening settings while the panel is already open brings that window to the front. Before, every open started another window.

## 0.1.0 (unreleased)

First version.

- Finds monitors over DDC/CI and names them from their EDID (model, serial, left or right).
- Publishes each chosen monitor to MQTT as a Home Assistant light: on/off and brightness.
- Settings panel (General, Devices, MQTT, About), tray icon, start at sign-in.
- MQTT password kept in Windows Credential Manager.
