//go:build windows

package main

// The monitor service: finds monitors, reads and changes them over DDC/CI, and keeps
// the latest state for the settings panel, the tray and MQTT.

import (
	"errors"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"nodoff/internal/bridge"
	"nodoff/internal/ddc"
	"nodoff/internal/settings"
)

// MonitorView is one monitor as the settings panel shows it.
type MonitorView struct {
	ID            string `json:"id"`
	Model         string `json:"model"`
	Serial        string `json:"serial,omitempty"`
	Year          int    `json:"year,omitempty"`
	Position      string `json:"position,omitempty"` // left, right, middle, only, or a number
	Resolution    string `json:"resolution,omitempty"`
	Primary       bool   `json:"primary"`
	Present       bool   `json:"present"`
	CanBrightness bool   `json:"can_brightness"`
	CanPower      bool   `json:"can_power"`
	Brightness    int    `json:"brightness"` // 0-100, -1 unknown
	Power         string `json:"power"`      // ON, OFF or ""
	PowerCode     int    `json:"power_code"` // raw VCP D6 value, 0 unknown
	Error         string `json:"error,omitempty"`
	MCCS          string `json:"mccs,omitempty"`
	Exposed       bool   `json:"exposed"`
	Name          string `json:"name"`         // the user's name, may be empty
	DefaultName   string `json:"default_name"` // what's used when Name is empty
	LastSeen      string `json:"last_seen,omitempty"`
}

type monState struct {
	MonitorView
	capsRead bool
	caps     ddc.Caps
	brMax    uint32
}

type monitorService struct {
	mu   sync.Mutex
	byID map[string]*monState
	// publish is called with fresh state for each monitor after a read.
	publish func(bridge.Display)
}

var mons = &monitorService{byID: map[string]*monState{}}

// scan enumerates monitors and reads their state. withCaps also asks each monitor for
// its capabilities, which can take a second or two per monitor.
func (s *monitorService) scan(withCaps bool) error {
	if err := s.scanDDC(withCaps); err != nil {
		return err
	}
	// Outside the DDC session, so slow MQTT publishes don't hold up monitor traffic.
	s.remember()
	s.publishAll()
	return nil
}

func (s *monitorService) scanDDC(withCaps bool) error {
	sess, err := ddc.Open()
	if err != nil {
		return err
	}
	defer sess.Close()
	seen := map[string]bool{}
	positions := positionNames(len(sess.Monitors))
	for i, m := range sess.Monitors {
		seen[m.ID] = true
		s.mu.Lock()
		st := s.byID[m.ID]
		if st == nil {
			st = &monState{}
			s.byID[m.ID] = st
		}
		st.ID, st.Model, st.Serial, st.Year = m.ID, m.Name, m.Serial, m.Year
		st.Position = positions[i]
		st.Primary = m.Primary
		st.Resolution = fmt.Sprintf("%d×%d", m.Width, m.Height)
		st.Present = true
		needCaps := withCaps || !st.capsRead
		s.mu.Unlock()

		if needCaps {
			caps, err := m.Capabilities()
			s.mu.Lock()
			st.capsRead = true
			if err == nil {
				st.caps = caps
				st.MCCS = caps.MCCS
			} else {
				st.caps = ddc.Caps{}
			}
			s.mu.Unlock()
		}
		s.readInto(m, st)
	}
	s.mu.Lock()
	for id, st := range s.byID {
		if !seen[id] {
			st.Present = false
			st.Error = ""
		}
	}
	s.mu.Unlock()
	return nil
}

// readInto reads brightness and power for one open monitor.
func (s *monitorService) readInto(m *ddc.Monitor, st *monState) {
	s.mu.Lock()
	caps := st.caps
	s.mu.Unlock()
	known := caps.VCP != nil && len(caps.VCP) > 0
	var errs []string

	br, brMax, brErr := -1, uint32(0), error(nil)
	if !known || caps.Supports(ddc.VCPBrightness) {
		cur, max, err := m.Get(ddc.VCPBrightness)
		if err == nil && max > 0 {
			br, brMax = int((cur*100+max/2)/max), max
		}
		brErr = err
	}
	pw, code, pwErr := "", 0, error(nil)
	if !known || caps.Supports(ddc.VCPPowerMode) {
		cur, _, err := m.Get(ddc.VCPPowerMode)
		if err == nil {
			code = int(cur)
			if cur == ddc.PowerOn {
				pw = "ON"
			} else {
				pw = "OFF"
			}
		}
		pwErr = err
	}
	if brErr != nil && pwErr != nil {
		errs = append(errs, "the monitor didn't answer over DDC/CI. Check DDC/CI is on in the monitor's own menu")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	st.CanBrightness = known && caps.Supports(ddc.VCPBrightness) || !known && brErr == nil
	st.CanPower = known && caps.Supports(ddc.VCPPowerMode) || !known && pwErr == nil
	if br >= 0 {
		st.Brightness, st.brMax = br, brMax
	} else if !st.Present {
		st.Brightness = -1
	}
	if pw != "" {
		st.Power, st.PowerCode = pw, code
	}
	st.Error = ""
	if len(errs) > 0 {
		st.Error = errs[0]
	}
}

// with opens a session, finds the monitor and runs fn on it.
func (s *monitorService) with(id string, fn func(m *ddc.Monitor, st *monState) error) error {
	sess, err := ddc.Open()
	if err != nil {
		return err
	}
	defer sess.Close()
	for _, m := range sess.Monitors {
		if m.ID != id {
			continue
		}
		s.mu.Lock()
		st := s.byID[id]
		if st == nil {
			st = &monState{MonitorView: MonitorView{ID: id, Brightness: -1}}
			s.byID[id] = st
		}
		s.mu.Unlock()
		return fn(m, st)
	}
	return errors.New("that monitor isn't connected right now")
}

// SetPower wakes the monitor, or sends it to standby (or off, per the setting).
func (s *monitorService) SetPower(id string, on bool) error {
	return s.with(id, func(m *ddc.Monitor, st *monState) error {
		s.mu.Lock()
		caps := st.caps
		s.mu.Unlock()
		if caps.VCP != nil && len(caps.VCP) > 0 && !caps.Supports(ddc.VCPPowerMode) {
			return errors.New("this monitor doesn't offer power control over DDC/CI")
		}
		v := uint16(ddc.PowerOn)
		if !on {
			v = offCode(caps, store.Get().OffMode)
		}
		if err := m.Set(ddc.VCPPowerMode, uint32(v)); err != nil {
			return err
		}
		log.Printf("%s: power %s (D6=%02X)", id, map[bool]string{true: "on", false: "off"}[on], v)
		return nil
	})
}

// offCode picks the D6 value for "off": the setting's choice if the monitor lists it,
// otherwise whichever of standby or off it does list.
func offCode(caps ddc.Caps, mode string) uint16 {
	want, alt := uint16(ddc.PowerStandby), uint16(ddc.PowerOff)
	if mode == "off" {
		want, alt = alt, want
	}
	if len(caps.VCP[ddc.VCPPowerMode]) == 0 || caps.Allows(ddc.VCPPowerMode, want) {
		return want
	}
	if caps.Allows(ddc.VCPPowerMode, alt) {
		return alt
	}
	return want
}

// SetBrightness sets the backlight, 0-100 per cent of the monitor's own range.
func (s *monitorService) SetBrightness(id string, pct int) error {
	if pct < 0 || pct > 100 {
		return errors.New("brightness must be 0 to 100")
	}
	return s.with(id, func(m *ddc.Monitor, st *monState) error {
		s.mu.Lock()
		max := st.brMax
		s.mu.Unlock()
		if max == 0 {
			_, mx, err := m.Get(ddc.VCPBrightness)
			if err != nil {
				return err
			}
			max = mx
		}
		if max == 0 {
			return errors.New("the monitor reported a brightness range of 0")
		}
		v := (uint32(pct)*max + 50) / 100
		if err := m.Set(ddc.VCPBrightness, v); err != nil {
			return err
		}
		s.mu.Lock()
		st.Brightness = pct
		s.mu.Unlock()
		return nil
	})
}

// Refresh re-reads one monitor and publishes it. Safe to call from any goroutine.
func (s *monitorService) Refresh(id string) {
	go func() {
		err := s.with(id, func(m *ddc.Monitor, st *monState) error {
			s.readInto(m, st)
			return nil
		})
		if err != nil {
			s.mu.Lock()
			if st := s.byID[id]; st != nil {
				st.Present = false
			}
			s.mu.Unlock()
		}
		s.publishOne(id)
	}()
}

// views lists every known monitor (present or remembered), left to right.
func (s *monitorService) views() []MonitorView {
	set := store.Get()
	s.mu.Lock()
	out := make([]MonitorView, 0, len(s.byID))
	for id, st := range s.byID {
		v := st.MonitorView
		d := set.Devices[id]
		v.Exposed, v.Name = d.Exposed, d.Name
		v.DefaultName = defaultName(v.Model, v.Position)
		if !d.LastSeen.IsZero() {
			v.LastSeen = d.LastSeen.Format(time.RFC3339)
		}
		out = append(out, v)
	}
	s.mu.Unlock()
	// Remembered monitors that haven't been seen since nodoff started.
	for id, d := range set.Devices {
		found := false
		for _, v := range out {
			if v.ID == id {
				found = true
				break
			}
		}
		if !found {
			out = append(out, MonitorView{ID: id, Model: d.Model, Serial: d.Serial, Exposed: d.Exposed, Name: d.Name,
				DefaultName: d.Model, Brightness: -1, LastSeen: d.LastSeen.Format(time.RFC3339)})
		}
	}
	order := map[string]int{"left": 0, "only": 0, "1": 0, "middle": 1, "2": 1, "right": 2, "3": 2, "4": 3, "5": 4, "6": 5}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].Present != out[b].Present {
			return out[a].Present
		}
		return order[out[a].Position] < order[out[b].Position]
	})
	return out
}

// exposedNames is what the MQTT bridge publishes: ID -> name, for exposed monitors.
func (s *monitorService) exposedNames() map[string]string {
	out := map[string]string{}
	for _, v := range s.views() {
		if !v.Exposed {
			continue
		}
		name := v.Name
		if name == "" {
			name = v.DefaultName
		}
		if name == "" {
			name = v.ID
		}
		out[v.ID] = name
	}
	return out
}

// remember adds new monitors to settings and refreshes when each was last seen
// (at most hourly, so settings.json isn't rewritten on every poll).
func (s *monitorService) remember() {
	s.mu.Lock()
	var present []MonitorView
	for _, st := range s.byID {
		if st.Present {
			present = append(present, st.MonitorView)
		}
	}
	s.mu.Unlock()
	cur := store.Get()
	need := false
	for _, v := range present {
		d, ok := cur.Devices[v.ID]
		if !ok || time.Since(d.LastSeen) > time.Hour || d.Model != v.Model {
			need = true
		}
	}
	if !need {
		return
	}
	_, err := store.Update(func(set *settings.Settings) error {
		for _, v := range present {
			d := set.Devices[v.ID]
			d.Model, d.Serial, d.LastSeen = v.Model, v.Serial, time.Now().UTC().Truncate(time.Second)
			set.Devices[v.ID] = d
		}
		return nil
	})
	if err != nil {
		log.Printf("settings: %v", err)
	}
}

func (s *monitorService) publishAll() {
	if s.publish == nil {
		return
	}
	s.mu.Lock()
	ids := make([]string, 0, len(s.byID))
	for id := range s.byID {
		ids = append(ids, id)
	}
	s.mu.Unlock()
	for _, id := range ids {
		s.publishOne(id)
	}
}

func (s *monitorService) publishOne(id string) {
	if s.publish == nil {
		return
	}
	s.mu.Lock()
	st := s.byID[id]
	if st == nil {
		s.mu.Unlock()
		return
	}
	d := bridge.Display{ID: id, Present: st.Present, Power: st.Power, Brightness: st.Brightness}
	s.mu.Unlock()
	s.publish(d)
}

// allPower wakes or sleeps every connected monitor that offers power control.
func (s *monitorService) allPower(on bool) {
	for _, v := range s.views() {
		if v.Present && v.CanPower {
			if err := s.SetPower(v.ID, on); err != nil {
				log.Printf("%s: %v", v.ID, err)
			}
			s.Refresh(v.ID)
		}
	}
}

func positionNames(n int) []string {
	switch n {
	case 1:
		return []string{"only"}
	case 2:
		return []string{"left", "right"}
	case 3:
		return []string{"left", "middle", "right"}
	}
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprint(i + 1)
	}
	return out
}

func defaultName(model, pos string) string {
	if model == "" {
		model = "Monitor"
	}
	switch pos {
	case "", "only":
		return model
	case "left", "right", "middle":
		return model + " (" + pos + ")"
	default:
		return model + " " + pos
	}
}

// forget drops a monitor from the in-memory list (settings already dropped it).
func (s *monitorService) forget(id string) {
	s.mu.Lock()
	if st := s.byID[id]; st != nil && !st.Present {
		delete(s.byID, id)
	}
	s.mu.Unlock()
}

// sleepWakeTest sends the monitor to sleep, waits, wakes it and reads the power mode back.
func (s *monitorService) sleepWakeTest(id string) (map[string]any, error) {
	if err := s.SetPower(id, false); err != nil {
		return nil, fmt.Errorf("couldn't send it to sleep: %v", err)
	}
	time.Sleep(5 * time.Second)
	if err := s.SetPower(id, true); err != nil {
		return map[string]any{"ok": false, "error": "it went to sleep but the wake command failed: " + err.Error() +
			". Press the monitor's power button to wake it"}, nil
	}
	time.Sleep(1500 * time.Millisecond)
	var code uint32
	err := s.with(id, func(m *ddc.Monitor, st *monState) error {
		c, _, err := m.Get(ddc.VCPPowerMode)
		code = c
		return err
	})
	s.Refresh(id)
	if err != nil {
		return map[string]any{"ok": false, "error": "sent the wake command but couldn't read the power mode back"}, nil
	}
	ok := code == ddc.PowerOn
	log.Printf("%s: sleep/wake test, power mode after wake %02X", id, code)
	return map[string]any{"ok": ok, "power_code": code}, nil
}
