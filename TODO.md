# TODO

## Checked so far

On one PC with two Lenovo L28u-30 monitors (HDMI and DisplayPort) and an Intel UHD 630:

- [x] Discovery finds both monitors and names them from the EDID.
- [x] MQTT connects and publishes status, availability, power, brightness and the Home Assistant discovery configs.
- [x] Brightness over MQTT reads back from the monitor, and a burst of commands settles on the last one.
- [x] Starts at sign-in through the per-user Run entry and runs in the desktop session.
- [x] Both monitors appear as lights in a real Home Assistant.

## Not yet confirmed

- [ ] Power over DDC/CI. One test sent `D6=04` (standby) and the monitor read back `D6=01` (on) five seconds later. Either it ignored standby while it had a picture, or it slept and reported on. If standby doesn't stick, try `D6=05` ("Power off" in General) and check it still wakes.
- [ ] Lock after screens-off and double-tap Ctrl (new in 0.2). Check the lock fires after the set minutes, double Ctrl wakes the screens when unlocked, and a key press wakes them on the lock screen. The last one depends on Windows updating the last-input time there.
- [ ] Opening settings twice brings the first window forward, including when it's minimised.
- [ ] Left and right come from each display's desktop X coordinate. Confirm against a real desk.

## Next

- [ ] Decide what to do when a monitor ignores `D6` standby. Options: try `D6=05`; fall back to the Windows "turn displays off" message (`SC_MONITORPOWER`, all monitors at once, waking needs fake input); or set brightness to 0 as a dim-only mode. Make it a per-monitor choice if monitors differ.
- [ ] Reading state while a monitor sleeps: some monitors stop answering DDC/CI in standby. Today nodoff keeps the last known state. Check whether a DisplayPort monitor in deep sleep drops off the bus altogether (Windows then removes the display, and nodoff can't wake it).
- [ ] Rescan when the display layout changes (`WM_DISPLAYCHANGE`) instead of waiting for the next poll.
- [ ] Input source as a Home Assistant `select` (VCP `60`).
- [ ] Connector type (HDMI or DisplayPort) in the device list, from `QueryDisplayConfig`.
- [ ] TLS with a private CA or self-signed broker certificate: a "trust this certificate" file picker. Today only certificates Windows trusts work.
- [ ] A "Test connection" button on the MQTT tab that tries the form values before saving.
- [ ] Tests for the Windows-only code: the HTTP guard (host, origin, key, content type) and the settings API. They can run on a Windows CI runner.
- [ ] Code signing. No certificate yet, so SmartScreen warns on first run. Options: Azure Trusted Signing, or SignPath's free tier for open source.
- [ ] Test on another GPU (NVIDIA or AMD) and another monitor brand. DDC/CI quirks differ by driver.
