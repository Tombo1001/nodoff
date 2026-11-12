# Security

## Reporting a problem

Please don't open a public issue for a security problem. Use GitHub's private report form: the **Security** tab of this repository, then **Report a vulnerability**. Say what you found, how to reproduce it and which version you ran. Reports are read and answered as soon as possible.

Only the latest release gets fixes.

## What nodoff is trusted with

nodoff runs as you, with your rights and nothing more. It has no service, no driver and no elevated part. It can change monitor brightness and power, lock the PC if you turn that on, and talk to one MQTT broker.

The README's [Security section](README.md#security) lists the measures in the code. The short version:

- no admin rights, per-user install, per-user start-up entry;
- Windows DLLs and helper programs load from paths Windows reports, never from the app folder or an environment variable;
- the MQTT password is in Windows Credential Manager and nowhere else;
- the settings panel listens on loopback only and accepts requests only from its own window;
- MQTT input is checked against the topics and monitors you published.

## What it doesn't protect against

- **Another program running as you.** It can read and change nodoff's settings, replace the exe in your profile, or read the saved password out of Credential Manager. Every per-user app has this limit.
- **An MQTT broker without TLS.** The username and password cross the network in the clear, and anyone on that network can read them. Turn on **Use TLS** if the broker isn't on a network you trust.
- **Anyone who can publish to your broker.** They can turn your published monitors on and off and set their brightness. They can't do anything else to the PC through nodoff. Give nodoff its own broker user and limit it to its base topic with an ACL.
- **Unsigned releases.** The exe isn't code-signed yet. Check the SHA-256 in the release notes against the file you downloaded.

## The keyboard hook

"Double-tap Ctrl to wake the screens" is off by default. Turning it on installs a low-level keyboard hook, and Windows passes every key press to such a hook. nodoff's hook looks at whether the key is Ctrl, keeps two timestamps, and passes the event on. It stores and logs no keys. Turning the setting off removes the hook. The code is in `wake_windows.go` and `ctrltap.go`, about 100 lines in all.
