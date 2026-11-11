# TODO

## Status on 2026-11-11

Version 0.1.1 is built and running on the test PC (two Lenovo L28u-30). Checked so far:

- [x] Discovery finds both monitors and names them from the EDID (IDs `len65fa_v9k27xqa`, `len65fa_v9k31mtd`).
- [x] MQTT: connects, publishes status, availability, power and brightness, and the Home Assistant discovery configs (checked against a throwaway Mosquitto, not a real Home Assistant yet).
- [x] Brightness over MQTT: `60` reads back as 60 from the monitor, and a burst of nine commands settles on the last one.
- [x] Starts at sign-in through the HKCU Run entry; runs in the desktop session; the panel is reachable only through its own window.
- [ ] **Power over DDC/CI is unconfirmed.** Sending `D6=04` (standby) to the left monitor logged fine, but 5 s later the monitor read back `D6=01` (on). Either it ignored standby while it still had a picture, or it slept and reports on. Needs eyes on the screen. If standby doesn't stick, try `D6=05` ("Power off" in General), and check whether it then wakes over DDC.
- [ ] The settings panel has only been seen in a browser against `tools/ui-mock`, not in the Edge window on Windows. Check it there, and check the tray icon and menu.
- [ ] Left and right may be the wrong way round: positions come from each display's desktop X coordinate. Confirm against the real desk.

## Next

- [ ] Point it at the real Home Assistant broker and check the entities, names and availability there.
- [ ] Decide what to do when a monitor ignores `D6` standby. Options: try `D6=05`; fall back to the Windows "turn displays off" message (`SC_MONITORPOWER`, all monitors at once, waking needs fake input); or set brightness to 0 as a dim-only mode. Make it a per-monitor choice if monitors differ.
- [ ] Reading state while a monitor sleeps: some monitors stop answering DDC/CI in standby. Today nodoff keeps the last known state. Check what the L28u-30 does, and whether a DisplayPort monitor in deep sleep drops off the bus altogether (Windows then removes the display, and nodoff can't wake it). Lenovo's menu may have a "DP deep sleep" option to turn off.
- [ ] Rescan when the display layout changes (`WM_DISPLAYCHANGE`) instead of waiting for the next poll.
- [ ] Input source as a Home Assistant `select` (VCP `60`; the L28u-30 lists DP-1 `0F` and HDMI-1 `11`).
- [ ] Connector type (HDMI or DisplayPort) in the device list, from `QueryDisplayConfig`.
- [ ] TLS with a private CA or self-signed broker certificate: a "trust this certificate" file picker. Today only certificates Windows trusts work.
- [ ] A "Test connection" button on the MQTT tab that tries the form values before saving.
- [ ] Tests for the Windows-only code: the HTTP guard (host, origin, key, content type), and the settings API. They can run on a Windows CI runner.

## Before going public

- [ ] Pick a licence. VoiceLoop used MIT; the dependencies allow MIT (paho under its EDL option).
- [ ] Check the name: GitHub has a few small `nodoff` repos (a Chrome sleep-timer extension, an Android app), none about monitors. Check domains if a site is wanted.
- [ ] Module path: `nodoff` today. Change to `github.com/<owner>/nodoff` when the repo exists.
- [ ] Code signing. No certificate yet, so SmartScreen warns on first run. Options: Azure Trusted Signing, or SignPath's free tier for open source.
- [ ] Screenshots of the real panel for the README.
- [ ] Test on another GPU (NVIDIA or AMD) and another monitor brand. DDC/CI quirks differ by driver.
- [ ] Remove the `.syso` and `dist/` from any commit (both are gitignored) and make sure no LAN addresses or host names get into the public tree.
