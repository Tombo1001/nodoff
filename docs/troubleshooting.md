# Troubleshooting

Start with the log. The **About** tab has **Show log**, or open `%LOCALAPPDATA%\nodoff\nodoff.log`. nodoff logs every power command, every MQTT connection change and every start.

## No monitors found, or a monitor "didn't answer over DDC/CI"

- Turn on DDC/CI in the monitor's own on-screen menu. Some monitors ship with it off, and a factory reset can turn it off again.
- Laptop screens don't have DDC/CI. nodoff can't control them.
- Docks, KVM switches and some USB-C or HDMI adaptors drop the DDC/CI channel. Plug the monitor straight into the PC to check.
- Some TVs used as monitors ignore DDC/CI altogether.
- Press **Discover** again after changing anything. nodoff reads what a monitor supports only then and at start.

## A monitor sleeps but doesn't wake

Some monitors stop listening on DDC/CI once they're asleep, so the wake command never arrives. Things to try, in order:

1. In **General**, keep **"Off" puts a monitor in** on **Standby**. Power off is deeper and fewer monitors wake from it.
2. Look in the monitor's menu for a deep sleep or power-saving option and turn it off. On DisplayPort, a monitor in deep sleep can drop off the bus, and Windows then treats it as unplugged.
3. Try the other cable type if the monitor has both HDMI and DisplayPort.

If none of that works, the monitor can't be woken over DDC/CI. Brightness control still works. Open an issue with the model, so it can go on a list.

## The monitor ignores sleep

If the light in Home Assistant flips back to on a few seconds after you turn it off, the monitor refused the command or woke again straight away. Try **Power off** instead of **Standby** in General. A monitor with no signal-loss sleep of its own may never stay off.

## Left and right are the wrong way round

nodoff names monitors from their position on the Windows desktop (**Settings > System > Display**). If the layout there doesn't match the desk, fix it there, or type your own names in the Devices tab. The names you type always win.

## The PC doesn't lock after the screens sleep

The lock timer starts only when nodoff itself has put every connected monitor to sleep: from Home Assistant, the tray menu or the panel. Turning a monitor off with its own button doesn't count, and neither does Windows turning the displays off. Waking any monitor through nodoff cancels the timer. The log says when the timer starts, when it's cancelled and when the PC locks.

## Double-tap Ctrl doesn't wake the screens

- Tap twice quickly: each tap shorter than 0.3 s and the second within 0.4 s of the first. Holding Ctrl or pressing another key in between doesn't count.
- On the lock screen, Windows hides key presses from every app. With the setting on, any key press or mouse movement there wakes the screens. This is new in 0.2 and has had little testing. If it doesn't work for you, please open an issue.
- Keys pressed while an elevated (administrator) window has focus aren't visible to nodoff, because nodoff doesn't run as administrator.

## MQTT won't connect

The footer of the panel and the tray tooltip show the reason.

- **"the broker refused the username or password"**: re-enter the password on the MQTT tab and save. The panel can't show the saved one.
- **"no answer"**: check the address and port, and that the PC's firewall and the broker's allow the connection. The Mosquitto add-on listens on 1883, and 8883 with TLS.
- **TLS errors**: nodoff accepts only certificates Windows already trusts. A self-signed broker certificate needs its CA added to the Windows certificate store for now.

## The lights are "unavailable" in Home Assistant

That's correct when the PC is off, asleep or nodoff has quit. If the PC is on, check the tray icon: grey means MQTT is disconnected. A single monitor showing unavailable means Windows no longer lists it, usually because it's unplugged or in a deep sleep.

## Old lights left behind in Home Assistant

Each light's ID comes from the base topic and the monitor's EDID. If you changed the base topic while nodoff couldn't reach the broker, the old entries stay. Delete the old device in Home Assistant under **Settings > Devices & services > MQTT**.

## SmartScreen warns about the exe

The exe isn't code-signed yet. Compare its SHA-256 with the one in the release notes:

```powershell
Get-FileHash nodoff.exe -Algorithm SHA256
```

## The settings window is blank or won't open

The panel opens in Microsoft Edge as an app window, or in your default browser if Edge isn't installed. It's served from `127.0.0.1` on a random port. Security software that blocks loopback connections will break it. Quit nodoff from the tray, start it again and check the log for "settings panel".
