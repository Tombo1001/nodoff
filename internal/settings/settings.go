// Package settings holds nodoff's saved settings and checks every value that comes in
// from the settings panel before it's stored or used.
package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
)

// Settings is everything in settings.json. The MQTT password is not here: it lives in
// Windows Credential Manager.
type Settings struct {
	OpenOnStart bool   `json:"open_settings_on_start"`
	PollSeconds int    `json:"poll_seconds"`
	OffMode     string `json:"off_mode"` // "standby" (VCP D6=04) or "off" (D6=05)
	// Lock Windows once every screen has been asleep for LockAfterMinutes.
	LockAfterOff     bool `json:"lock_after_off"`
	LockAfterMinutes int  `json:"lock_after_minutes"`
	// Wake the screens on a double tap of Ctrl (any input when Windows is locked).
	WakeDoubleCtrl bool `json:"wake_double_ctrl"`
	MQTT           MQTT `json:"mqtt"`
	// Devices remembers every monitor nodoff has seen, so the list still shows one
	// that's unplugged, and its choices survive.
	Devices map[string]Device `json:"devices"`
}

type MQTT struct {
	Enabled         bool   `json:"enabled"`
	Host            string `json:"host"`
	Port            int    `json:"port"`
	TLS             bool   `json:"tls"`
	Username        string `json:"username"`
	NodeName        string `json:"node_name"`  // shown in Home Assistant as the device name
	BaseTopic       string `json:"base_topic"` // e.g. nodoff/desk-pc
	Discovery       bool   `json:"discovery"`  // publish Home Assistant MQTT discovery
	DiscoveryPrefix string `json:"discovery_prefix"`
}

type Device struct {
	Exposed  bool      `json:"exposed"`
	Name     string    `json:"name,omitempty"` // the user's name for it; empty means the EDID name
	Model    string    `json:"model,omitempty"`
	Serial   string    `json:"serial,omitempty"`
	LastSeen time.Time `json:"last_seen"`
}

// Defaults for a new install. host is the computer name.
func Defaults(host string) Settings {
	slug := topicSlug(host)
	if slug == "" {
		slug = "pc"
	}
	return Settings{
		PollSeconds:      30,
		OffMode:          "standby",
		LockAfterMinutes: 5,
		MQTT: MQTT{
			Port:            1883,
			NodeName:        host,
			BaseTopic:       "nodoff/" + slug,
			Discovery:       true,
			DiscoveryPrefix: "homeassistant",
		},
		Devices: map[string]Device{},
	}
}

// Normalise trims strings and checks every field. It returns an error a person can act on.
func (s *Settings) Normalise() error {
	if s.PollSeconds == 0 {
		s.PollSeconds = 30
	}
	if s.PollSeconds < 5 || s.PollSeconds > 3600 {
		return errors.New("poll every 5 to 3600 seconds")
	}
	switch s.OffMode {
	case "":
		s.OffMode = "standby"
	case "standby", "off":
	default:
		return errors.New(`"off" must mean standby or off`)
	}
	if s.LockAfterMinutes == 0 {
		s.LockAfterMinutes = 5
	}
	if s.LockAfterMinutes < 1 || s.LockAfterMinutes > 240 {
		return errors.New("lock after 1 to 240 minutes")
	}
	m := &s.MQTT
	m.Host = strings.TrimSpace(m.Host)
	m.Username = strings.TrimSpace(m.Username)
	m.NodeName = strings.TrimSpace(m.NodeName)
	m.BaseTopic = strings.Trim(strings.TrimSpace(m.BaseTopic), "/")
	m.DiscoveryPrefix = strings.Trim(strings.TrimSpace(m.DiscoveryPrefix), "/")
	if m.Port == 0 {
		m.Port = 1883
	}
	if m.Port < 1 || m.Port > 65535 {
		return errors.New("the MQTT port must be 1 to 65535")
	}
	if err := checkHost(m.Host); err != nil {
		return err
	}
	if len(m.Username) > 256 || strings.ContainsFunc(m.Username, unicode.IsControl) {
		return errors.New("the MQTT username is too long or has control characters")
	}
	if m.NodeName == "" {
		return errors.New("give this PC a name for Home Assistant")
	}
	if len(m.NodeName) > 64 || strings.ContainsFunc(m.NodeName, unicode.IsControl) {
		return errors.New("the PC name must be 64 characters or fewer, with no control characters")
	}
	if err := CheckTopic(m.BaseTopic, "base topic"); err != nil {
		return err
	}
	if m.DiscoveryPrefix == "" {
		m.DiscoveryPrefix = "homeassistant"
	}
	if err := CheckTopic(m.DiscoveryPrefix, "discovery prefix"); err != nil {
		return err
	}
	if m.Enabled && m.Host == "" {
		return errors.New("enter the MQTT broker's address")
	}
	if s.Devices == nil {
		s.Devices = map[string]Device{}
	}
	for id, d := range s.Devices {
		if !validID(id) {
			delete(s.Devices, id)
			continue
		}
		d.Name = strings.TrimSpace(d.Name)
		if len(d.Name) > 64 || strings.ContainsFunc(d.Name, unicode.IsControl) {
			return fmt.Errorf("monitor names must be 64 characters or fewer")
		}
		s.Devices[id] = d
	}
	return nil
}

// CheckTopic rejects MQTT wildcards, empty levels and odd characters, so a topic from
// settings can't subscribe to more than nodoff's own messages.
func CheckTopic(t, what string) error {
	if t == "" {
		return fmt.Errorf("the %s can't be empty", what)
	}
	if len(t) > 200 {
		return fmt.Errorf("the %s is too long", what)
	}
	for _, level := range strings.Split(t, "/") {
		if level == "" {
			return fmt.Errorf("the %s has an empty level (two slashes in a row)", what)
		}
	}
	for _, r := range t {
		if r == '+' || r == '#' || r == '$' || unicode.IsControl(r) || unicode.IsSpace(r) {
			return fmt.Errorf("the %s can't contain spaces, +, # or $", what)
		}
	}
	return nil
}

func checkHost(h string) error {
	if h == "" {
		return nil
	}
	if len(h) > 253 {
		return errors.New("the MQTT broker address is too long")
	}
	for _, r := range h {
		ok := r == '.' || r == '-' || r == ':' || r == '[' || r == ']' || r == '_' ||
			r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
		if !ok {
			return errors.New("enter the broker as a host name or IP address only, without mqtt:// or a port")
		}
	}
	return nil
}

func validID(id string) bool {
	if id == "" || len(id) > 120 {
		return false
	}
	for _, r := range id {
		if !(r == '_' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// topicSlug makes a host name safe as one topic level.
func topicSlug(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		case r == ' ' || r == '.':
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

// Store is the settings file, safe for concurrent use.
type Store struct {
	mu   sync.Mutex
	path string
	s    Settings
}

// Load reads path, or starts from defaults if it doesn't exist. A file that won't parse
// is kept as path.broken-<time> and defaults are used, so a bad edit can't stop nodoff starting.
func Load(path, host string) (*Store, error) {
	st := &Store{path: path, s: Defaults(host)}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	b = []byte(strings.TrimPrefix(string(b), "\ufeff"))
	s := Defaults(host)
	if err := json.Unmarshal(b, &s); err == nil {
		err = s.Normalise()
		if err == nil {
			st.s = s
			return st, nil
		}
	}
	broken := path + ".broken-" + time.Now().Format("20060102-150405")
	_ = os.Rename(path, broken)
	return st, fmt.Errorf("settings.json couldn't be read, so defaults are in use; the old file is %s", filepath.Base(broken))
}

// Get returns a copy.
func (st *Store) Get() Settings {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.s.clone()
}

// Update changes the settings through fn, checks them and saves. Nothing changes if
// fn or the check fails.
func (st *Store) Update(fn func(*Settings) error) (Settings, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	next := st.s.clone()
	if err := fn(&next); err != nil {
		return st.s.clone(), err
	}
	if err := next.Normalise(); err != nil {
		return st.s.clone(), err
	}
	if err := writeAtomic(st.path, next); err != nil {
		return st.s.clone(), err
	}
	st.s = next
	return next.clone(), nil
}

func (s Settings) clone() Settings {
	c := s
	c.Devices = make(map[string]Device, len(s.Devices))
	for k, v := range s.Devices {
		c.Devices[k] = v
	}
	return c
}

func writeAtomic(path string, s Settings) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
