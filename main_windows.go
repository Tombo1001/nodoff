//go:build windows

// nodoff exposes monitor brightness and power (over DDC/CI) to MQTT, with Home
// Assistant discovery, so a presence sensor can wake and sleep a PC's screens.
package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"fyne.io/systray"
	"golang.org/x/sys/windows"

	"nodoff/internal/bridge"
	"nodoff/internal/logo"
	"nodoff/internal/settings"
)

var (
	dataDir string
	store   *settings.Store

	bridgeMu  sync.Mutex
	mqttLink  *bridge.Bridge
	mqttState bridge.Status

	pollNow = make(chan struct{}, 1)

	mStatus *systray.MenuItem
)

func main() {
	// Load DLLs from System32 only, never from the folder the exe sits in.
	_ = windows.SetDefaultDllDirectories(windows.LOAD_LIBRARY_SEARCH_SYSTEM32)

	if !claimSingleInstance() {
		return // the running copy opens Settings instead
	}
	enableDPIAwareness()

	base, err := os.UserConfigDir() // %APPDATA%; we want the local one
	if la := os.Getenv("LOCALAPPDATA"); la != "" {
		base, err = la, nil
	}
	if err != nil {
		fatal("can't find a folder for settings: %v", err)
	}
	dataDir = filepath.Join(base, "nodoff")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		fatal("can't create %s: %v", dataDir, err)
	}
	openLog()

	host, _ := os.Hostname()
	store, err = settings.Load(filepath.Join(dataDir, "settings.json"), host)
	if err != nil {
		log.Printf("settings: %v", err)
	}
	log.Printf("nodoff %s starting", version)

	mons.publish = func(d bridge.Display) {
		bridgeMu.Lock()
		b := mqttLink
		bridgeMu.Unlock()
		if b != nil {
			b.Publish(d)
		}
	}

	go func() {
		if err := settingsServer(); err != nil {
			log.Printf("settings panel: %v", err)
		}
	}()
	go func() {
		if err := mons.scan(true); err != nil {
			log.Printf("first scan: %v", err)
		}
		restartBridge(false)
		poller()
	}()
	if !launchedInBackground() || store.Get().OpenOnStart {
		go func() { time.Sleep(800 * time.Millisecond); openSettings() }()
	}
	systray.Run(onReady, func() {})
}

// poller re-reads every monitor on the poll interval, or straight away when asked.
func poller() {
	for {
		wait := time.Duration(store.Get().PollSeconds) * time.Second
		select {
		case <-time.After(wait):
		case <-pollNow:
		}
		if err := mons.scan(false); err != nil {
			log.Printf("scan: %v", err)
		}
	}
}

func requestPoll() {
	select {
	case pollNow <- struct{}{}:
	default:
	}
}

// restartBridge drops the MQTT connection and makes a new one from the current
// settings. clearOld removes the old connection's monitors from Home Assistant first,
// which matters when the base topic or discovery prefix changed.
func restartBridge(clearOld bool) {
	bridgeMu.Lock()
	old := mqttLink
	mqttLink = nil
	bridgeMu.Unlock()
	if old != nil {
		old.Stop(clearOld)
	}
	set := store.Get()
	m := set.MQTT
	if !m.Enabled || m.Host == "" {
		setMQTTStatus(bridge.Status{Error: "off"})
		return
	}
	pw, err := loadPassword()
	if err != nil {
		log.Printf("mqtt password: %v", err)
	}
	host, _ := os.Hostname()
	b := bridge.New(bridge.Config{
		Host: m.Host, Port: m.Port, TLS: m.TLS,
		Username: m.Username, Password: pw,
		ClientID:  "nodoff-" + strings.ToLower(host),
		Topics:    bridge.NewTopics(m.BaseTopic, m.DiscoveryPrefix),
		Discovery: m.Discovery,
		Info:      bridge.Info{NodeName: m.NodeName, Version: version, Model: "Windows PC"},
	}, mons, setMQTTStatus)
	b.SetDevices(mons.exposedNames())
	bridgeMu.Lock()
	mqttLink = b
	bridgeMu.Unlock()
	setMQTTStatus(bridge.Status{Error: "connecting"})
	b.Start()
}

// refreshExposed tells the bridge which monitors to publish after the list changes.
func refreshExposed() {
	bridgeMu.Lock()
	b := mqttLink
	bridgeMu.Unlock()
	if b != nil {
		b.SetDevices(mons.exposedNames())
	}
}

func setMQTTStatus(s bridge.Status) {
	bridgeMu.Lock()
	mqttState = s
	bridgeMu.Unlock()
	refreshTray()
}

func currentMQTTStatus() bridge.Status {
	bridgeMu.Lock()
	defer bridgeMu.Unlock()
	return mqttState
}

// ---------- tray ----------

func onReady() {
	systray.SetTitle("nodoff")
	mStatus = systray.AddMenuItem("", "")
	mStatus.Disable()
	systray.AddSeparator()
	mWake := systray.AddMenuItem("Wake all screens", "")
	mSleep := systray.AddMenuItem("Sleep all screens", "")
	mSettings := systray.AddMenuItem("Settings…", "")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quit nodoff", "")
	refreshTray()

	var lastTap time.Time
	var tapMu sync.Mutex
	systray.SetOnTapped(func() {
		tapMu.Lock()
		double := time.Since(lastTap) < 500*time.Millisecond
		lastTap = time.Now()
		if double {
			lastTap = time.Time{}
		}
		tapMu.Unlock()
		if double {
			openSettings()
		}
	})
	go func() {
		for {
			select {
			case <-mWake.ClickedCh:
				go mons.allPower(true)
			case <-mSleep.ClickedCh:
				go mons.allPower(false)
			case <-mSettings.ClickedCh:
				openSettings()
			case <-mQuit.ClickedCh:
				quit()
			}
		}
	}()
}

func quit() {
	log.Printf("quitting")
	bridgeMu.Lock()
	b := mqttLink
	bridgeMu.Unlock()
	if b != nil {
		b.Stop(false) // says "offline" so Home Assistant greys the monitors out
	}
	systray.Quit()
	os.Exit(0)
}

func refreshTray() {
	if mStatus == nil {
		return
	}
	s := currentMQTTStatus()
	var text string
	switch {
	case s.Connected:
		text = "Connected to MQTT"
		systray.SetIcon(logo.ICO(logo.Amber, 16, 20, 24, 32, 48))
	case s.Error == "off":
		text = "MQTT is off"
		systray.SetIcon(logo.ICO(logo.Grey, 16, 20, 24, 32, 48))
	case s.Error == "connecting":
		text = "Connecting to MQTT…"
		systray.SetIcon(logo.ICO(logo.Grey, 16, 20, 24, 32, 48))
	default:
		text = "MQTT: " + s.Error
		systray.SetIcon(logo.ICO(logo.Grey, 16, 20, 24, 32, 48))
	}
	if len(text) > 60 {
		text = text[:57] + "…"
	}
	mStatus.SetTitle(text)
	systray.SetTooltip("nodoff: " + text)
}

// ---------- log ----------

func openLog() {
	path := filepath.Join(dataDir, "nodoff.log")
	if fi, err := os.Stat(path); err == nil && fi.Size() > 1<<20 {
		_ = os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err == nil {
		log.SetOutput(f)
	}
}

func fatal(format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	log.Print(msg)
	t, _ := windows.UTF16PtrFromString("nodoff")
	m, _ := windows.UTF16PtrFromString(msg)
	windows.MessageBox(0, m, t, windows.MB_ICONERROR)
	os.Exit(1)
}

var procSetDpiAwarenessCx = windows.NewLazySystemDLL("user32.dll").NewProc("SetProcessDpiAwarenessContext")

func enableDPIAwareness() {
	if procSetDpiAwarenessCx.Find() == nil {
		procSetDpiAwarenessCx.Call(^uintptr(3)) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 (-4)
	}
}
