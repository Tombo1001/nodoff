//go:build windows

package main

// Windows plumbing: one copy at a time, start at sign-in, and the MQTT password in
// Credential Manager.

import (
	"errors"
	"log"
	"os"
	"strings"

	"github.com/danieljoos/wincred"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// ---------- single instance ----------

const (
	mutexName = `Local\nodoffSingleInstance`
	eventName = `Local\nodoffShowSettings`
)

// claimSingleInstance returns false if nodoff is already running in this session,
// after asking that copy to open Settings.
func claimSingleInstance() bool {
	mn, _ := windows.UTF16PtrFromString(mutexName)
	en, _ := windows.UTF16PtrFromString(eventName)
	_, err := windows.CreateMutex(nil, false, mn)
	if err == windows.ERROR_ALREADY_EXISTS {
		if ev, err := windows.OpenEvent(windows.EVENT_MODIFY_STATE, false, en); err == nil {
			windows.SetEvent(ev)
			windows.CloseHandle(ev)
		}
		return false
	}
	ev, err := windows.CreateEvent(nil, 0, 0, en) // auto-reset
	if err != nil {
		log.Printf("single instance event: %v", err)
		return true
	}
	go func() {
		for {
			if r, _ := windows.WaitForSingleObject(ev, windows.INFINITE); r == windows.WAIT_OBJECT_0 {
				openSettings()
			}
		}
	}()
	return true
}

// launchedInBackground is true when Windows starts us at sign-in (the Run entry passes --background).
func launchedInBackground() bool {
	for _, a := range os.Args[1:] {
		if a == "--background" {
			return true
		}
	}
	return false
}

// ---------- start at sign-in ----------

const (
	runKey   = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValue = "nodoff"
)

// startupCommand is the Run entry for this exe. Quoted, so a path with spaces can't
// be read as a different program.
func startupCommand() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return `"` + exe + `" --background`, nil
}

// startupState reports whether nodoff starts at sign-in, and whether the entry points
// at this exe (it won't after the exe moves).
func startupState() (on bool, thisExe bool) {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false, false
	}
	defer k.Close()
	v, _, err := k.GetStringValue(runValue)
	if err != nil {
		return false, false
	}
	want, _ := startupCommand()
	return true, strings.EqualFold(v, want)
}

// setStartup adds or removes the per-user Run entry. Per-user means no admin rights,
// and nothing runs for other accounts.
func setStartup(on bool) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if !on {
		if err := k.DeleteValue(runValue); err != nil && !errors.Is(err, registry.ErrNotExist) {
			return err
		}
		return nil
	}
	cmd, err := startupCommand()
	if err != nil {
		return err
	}
	return k.SetStringValue(runValue, cmd)
}

// ---------- MQTT password ----------

// The password lives in Windows Credential Manager as a generic credential, encrypted
// by Windows for this user on this PC. It never goes in settings.json, the log or
// back to the settings panel.
const credTarget = "nodoff/mqtt"

func loadPassword() (string, error) {
	c, err := wincred.GetGenericCredential(credTarget)
	if errors.Is(err, wincred.ErrElementNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return string(c.CredentialBlob), nil
}

func passwordSet() bool {
	_, err := wincred.GetGenericCredential(credTarget)
	return err == nil
}

func savePassword(user, pw string) error {
	if pw == "" {
		return clearPassword()
	}
	c := wincred.NewGenericCredential(credTarget)
	c.UserName = user
	c.CredentialBlob = []byte(pw)
	c.Persist = wincred.PersistLocalMachine // this PC only, never roams with the profile
	c.Comment = "MQTT broker password for nodoff"
	return c.Write()
}

func clearPassword() error {
	c, err := wincred.GetGenericCredential(credTarget)
	if errors.Is(err, wincred.ErrElementNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return c.Delete()
}
