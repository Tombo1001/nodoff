//go:build windows

package main

// The settings panel: one HTML page served on a random loopback port and opened in an
// Edge app window (no tabs or address bar).
//
// Who can talk to it: only this user's Edge window. The server listens on 127.0.0.1
// only, checks the Host header (so a web page can't reach it through DNS rebinding),
// rejects cross-origin requests, and needs a random key that changes on every start.
// The key reaches the page in the URL that nodoff opens and the page strips it from
// the address straight away.

import (
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"nodoff/internal/logo"
	"nodoff/internal/settings"
)

//go:embed ui.html
var uiHTML string

var (
	uiKey  = randomHex(16)
	uiAddr string // host:port, set once the listener is up
	uiOnce sync.Once
	uiUp   = make(chan struct{})

	// uiPages counts open panel pages: each one holds an /api/events stream.
	uiPages atomic.Int32
)

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand doesn't fail on supported Windows versions
	}
	return hex.EncodeToString(b)
}

func settingsServer() error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		close(uiUp)
		return err
	}
	uiAddr = ln.Addr().String()
	close(uiUp)

	mux := http.NewServeMux()
	icon := logo.ICO(logo.Amber, 16, 24, 32, 48, 64, 256)
	mark := logo.PNG(128, logo.Amber)

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		if !sameHost(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		// A fresh nonce per load, so only nodoff's own inline script runs.
		nonce := randomHex(16)
		h := w.Header()
		h.Set("Content-Type", "text/html; charset=utf-8")
		h.Set("Cache-Control", "no-store")
		h.Set("Content-Security-Policy", "default-src 'none'; img-src 'self' data:; style-src 'unsafe-inline'; "+
			"script-src 'nonce-"+nonce+"'; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		io.WriteString(w, strings.Replace(uiHTML, "<script>", `<script nonce="`+nonce+`">`, 1))
	})
	mux.HandleFunc("GET /favicon.ico", static("image/x-icon", icon))
	mux.HandleFunc("GET /logo.png", static("image/png", mark))

	// The page keeps this stream open. When nodoff quits the connection drops and the
	// page closes its own window. EventSource can't send headers, so the key is in the query.
	mux.HandleFunc("GET /api/events", func(w http.ResponseWriter, r *http.Request) {
		if !sameHost(r) || !keyOK(r.URL.Query().Get("k")) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		fl, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "no streaming", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-store")
		uiPages.Add(1)
		defer uiPages.Add(-1)
		fmt.Fprintf(w, "event: hello\ndata: %s\n\n", version)
		fl.Flush()
		t := time.NewTicker(15 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-t.C:
				fmt.Fprint(w, ": ping\n\n")
				fl.Flush()
			}
		}
	})

	api := func(pattern string, h func(w http.ResponseWriter, r *http.Request) (any, error)) {
		mux.HandleFunc(pattern, guard(func(w http.ResponseWriter, r *http.Request) {
			out, err := h(w, r)
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": err.Error()})
				return
			}
			if out == nil {
				out = stateJSON()
			}
			json.NewEncoder(w).Encode(out)
		}))
	}

	api("GET /api/state", func(w http.ResponseWriter, r *http.Request) (any, error) { return nil, nil })

	api("POST /api/general", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var b struct {
			OpenOnStart  *bool   `json:"open_settings_on_start"`
			PollSeconds  *int    `json:"poll_seconds"`
			OffMode      *string `json:"off_mode"`
			StartAtLogin *bool   `json:"start_at_login"`
		}
		if err := decode(r, &b, 4<<10); err != nil {
			return nil, err
		}
		if b.StartAtLogin != nil {
			if err := setStartup(*b.StartAtLogin); err != nil {
				return nil, fmt.Errorf("couldn't change the start-up setting: %v", err)
			}
		}
		_, err := store.Update(func(s *settings.Settings) error {
			if b.OpenOnStart != nil {
				s.OpenOnStart = *b.OpenOnStart
			}
			if b.PollSeconds != nil {
				s.PollSeconds = *b.PollSeconds
			}
			if b.OffMode != nil {
				s.OffMode = *b.OffMode
			}
			return nil
		})
		return nil, err
	})

	api("POST /api/mqtt", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var b struct {
			settings.MQTT
			Password      string `json:"password"`       // empty: leave as it is
			ClearPassword bool   `json:"clear_password"` // remove the saved password
		}
		if err := decode(r, &b, 16<<10); err != nil {
			return nil, err
		}
		if len(b.Password) > 1024 {
			return nil, errors.New("that password is too long")
		}
		before := store.Get().MQTT
		if _, err := store.Update(func(s *settings.Settings) error { s.MQTT = b.MQTT; return nil }); err != nil {
			return nil, err
		}
		after := store.Get().MQTT
		switch {
		case b.ClearPassword:
			if err := clearPassword(); err != nil {
				return nil, fmt.Errorf("couldn't remove the saved password: %v", err)
			}
		case b.Password != "":
			if err := savePassword(after.Username, b.Password); err != nil {
				return nil, fmt.Errorf("couldn't save the password in Credential Manager: %v", err)
			}
		}
		// Moving to a new topic or prefix, or turning discovery or MQTT off, removes
		// this PC's monitors from Home Assistant under the old names first.
		clearOld := before.BaseTopic != after.BaseTopic || before.DiscoveryPrefix != after.DiscoveryPrefix ||
			before.Discovery && !after.Discovery || before.Enabled && !after.Enabled
		log.Printf("mqtt settings saved (enabled %v, %s:%d, tls %v, discovery %v)", after.Enabled, after.Host, after.Port, after.TLS, after.Discovery)
		restartBridge(clearOld)
		return nil, nil
	})

	api("POST /api/discover", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := mons.scan(true); err != nil {
			return nil, err
		}
		refreshExposed()
		return nil, nil
	})

	api("POST /api/device", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var b struct {
			ID      string  `json:"id"`
			Exposed *bool   `json:"exposed"`
			Name    *string `json:"name"`
			Forget  bool    `json:"forget"`
		}
		if err := decode(r, &b, 4<<10); err != nil {
			return nil, err
		}
		_, err := store.Update(func(s *settings.Settings) error {
			d, ok := s.Devices[b.ID]
			if !ok {
				return errors.New("unknown monitor; press Discover first")
			}
			if b.Forget {
				delete(s.Devices, b.ID)
				return nil
			}
			if b.Exposed != nil {
				d.Exposed = *b.Exposed
			}
			if b.Name != nil {
				d.Name = *b.Name
			}
			s.Devices[b.ID] = d
			return nil
		})
		if err != nil {
			return nil, err
		}
		if b.Forget {
			mons.forget(b.ID)
		}
		refreshExposed()
		return nil, nil
	})

	api("POST /api/device/brightness", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var b struct {
			ID    string `json:"id"`
			Value int    `json:"value"`
		}
		if err := decode(r, &b, 1<<10); err != nil {
			return nil, err
		}
		if err := mons.SetBrightness(b.ID, b.Value); err != nil {
			return nil, err
		}
		mons.Refresh(b.ID)
		return map[string]any{"ok": true}, nil
	})

	api("POST /api/device/power", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var b struct {
			ID string `json:"id"`
			On bool   `json:"on"`
		}
		if err := decode(r, &b, 1<<10); err != nil {
			return nil, err
		}
		if err := mons.SetPower(b.ID, b.On); err != nil {
			return nil, err
		}
		mons.Refresh(b.ID)
		return map[string]any{"ok": true}, nil
	})

	// Sleep a monitor for a few seconds and wake it again, to prove DDC/CI can wake
	// it before anyone relies on it from an automation.
	api("POST /api/device/test", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var b struct {
			ID string `json:"id"`
		}
		if err := decode(r, &b, 1<<10); err != nil {
			return nil, err
		}
		return mons.sleepWakeTest(b.ID)
	})

	api("POST /api/open", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var b struct {
			What string `json:"what"`
		}
		if err := decode(r, &b, 1<<10); err != nil {
			return nil, err
		}
		// Fixed targets only: nothing from the request reaches the command line.
		var args []string
		switch b.What {
		case "folder":
			args = []string{dataDir}
		case "log":
			args = []string{"/select,", filepath.Join(dataDir, "nodoff.log")}
		default:
			return nil, errors.New("unknown target")
		}
		explorer := filepath.Join(os.Getenv("SystemRoot"), "explorer.exe")
		if os.Getenv("SystemRoot") == "" {
			explorer = `C:\Windows\explorer.exe`
		}
		_ = exec.Command(explorer, args...).Start()
		return map[string]any{"ok": true}, nil
	})

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    16 << 10,
	}
	return srv.Serve(ln)
}

func static(ctype string, body []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !sameHost(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", ctype)
		w.Write(body)
	}
}

// guard lets a request through only from nodoff's own page.
func guard(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !sameHost(r) || !keyOK(r.Header.Get("X-Nodoff-Key")) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if o := r.Header.Get("Origin"); o != "" && o != "http://"+uiAddr {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if r.Method == http.MethodPost && !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			http.Error(w, "json only", http.StatusUnsupportedMediaType)
			return
		}
		h(w, r)
	}
}

func sameHost(r *http.Request) bool { return uiAddr != "" && r.Host == uiAddr }

func keyOK(k string) bool { return subtle.ConstantTimeCompare([]byte(k), []byte(uiKey)) == 1 }

func decode(r *http.Request, v any, limit int64) error {
	d := json.NewDecoder(io.LimitReader(r.Body, limit))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return fmt.Errorf("bad request: %v", err)
	}
	return nil
}

func stateJSON() map[string]any {
	set := store.Get()
	on, thisExe := startupState()
	host, _ := os.Hostname()
	return map[string]any{
		"version":  version,
		"go":       runtime.Version(),
		"host":     host,
		"data_dir": dataDir,
		"general": map[string]any{
			"open_settings_on_start": set.OpenOnStart,
			"poll_seconds":           set.PollSeconds,
			"off_mode":               set.OffMode,
			"start_at_login":         on,
			"start_entry_stale":      on && !thisExe,
		},
		"mqtt":         set.MQTT,
		"password_set": passwordSet(),
		"mqtt_status":  currentMQTTStatus(),
		"monitors":     mons.views(),
	}
}

// openSettings opens the panel in an Edge app window, or the default browser without Edge.
var (
	openMu   sync.Mutex
	lastOpen time.Time
)

func openSettings() {
	<-uiUp
	if uiAddr == "" {
		return
	}
	openMu.Lock()
	if time.Since(lastOpen) < 2*time.Second {
		openMu.Unlock()
		return
	}
	lastOpen = time.Now()
	openMu.Unlock()
	// One panel window at a time: if a page is already open, bring its window forward.
	if uiPages.Load() > 0 && focusPanelWindow() {
		return
	}
	url := "http://" + uiAddr + "/?k=" + uiKey
	for _, base := range []string{os.Getenv("ProgramFiles(x86)"), os.Getenv("ProgramFiles")} {
		if base == "" {
			continue
		}
		edge := filepath.Join(base, "Microsoft", "Edge", "Application", "msedge.exe")
		if _, err := os.Stat(edge); err == nil {
			cmd := exec.Command(edge, "--app="+url, "--window-size=1000,820", "--no-first-run")
			cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
			if cmd.Start() == nil {
				return
			}
		}
	}
	rundll := filepath.Join(os.Getenv("SystemRoot"), "System32", "rundll32.exe")
	cmd := exec.Command(rundll, "url.dll,FileProtocolHandler", url)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	_ = cmd.Start()
}

var (
	user32                  = windows.NewLazySystemDLL("user32.dll")
	procEnumWindows         = user32.NewProc("EnumWindows")
	procGetClassName        = user32.NewProc("GetClassNameW")
	procGetWindowText       = user32.NewProc("GetWindowTextW")
	procIsWindowVisible     = user32.NewProc("IsWindowVisible")
	procIsIconic            = user32.NewProc("IsIconic")
	procShowWindow          = user32.NewProc("ShowWindow")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
)

// focusPanelWindow finds the open panel window and brings it to the front, restoring
// it if minimised. The panel is a Chromium app window whose title is the page title.
// It reports false when there's no such window, for example when the panel is a tab
// in another browser.
func focusPanelWindow() bool {
	var found uintptr
	cb := syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
		if v, _, _ := procIsWindowVisible.Call(hwnd); v == 0 {
			return 1
		}
		var buf [64]uint16
		procGetClassName.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		if windows.UTF16ToString(buf[:]) != "Chrome_WidgetWin_1" {
			return 1
		}
		buf = [64]uint16{}
		procGetWindowText.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		if windows.UTF16ToString(buf[:]) != "nodoff" {
			return 1
		}
		found = hwnd
		return 0 // stop
	})
	procEnumWindows.Call(cb, 0)
	if found == 0 {
		return false
	}
	if min, _, _ := procIsIconic.Call(found); min != 0 {
		procShowWindow.Call(found, 9) // SW_RESTORE
	}
	procSetForegroundWindow.Call(found)
	return true
}
