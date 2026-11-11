package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultsAreValid(t *testing.T) {
	for _, host := range []string{"DESKTOP-ABC123", "Sam's PC", "", "a.b.c"} {
		s := Defaults(host)
		if host == "" {
			s.MQTT.NodeName = "x"
		}
		if err := s.Normalise(); err != nil {
			t.Fatalf("%q: %v", host, err)
		}
	}
	if got := Defaults("DESKTOP-ABC123").MQTT.BaseTopic; got != "nodoff/desktop-abc123" {
		t.Fatalf("base topic %q", got)
	}
}

func TestCheckTopic(t *testing.T) {
	good := []string{"nodoff/desk", "homeassistant", "a/b/c-d_e"}
	bad := []string{"", "a/+/b", "a/#", "a//b", "a b", "$SYS/x", strings.Repeat("a", 201), "a\x00b"}
	for _, s := range good {
		if err := CheckTopic(s, "t"); err != nil {
			t.Errorf("%q rejected: %v", s, err)
		}
	}
	for _, s := range bad {
		if CheckTopic(s, "t") == nil {
			t.Errorf("%q accepted", s)
		}
	}
}

func TestNormaliseRejects(t *testing.T) {
	cases := map[string]func(*Settings){
		"port":      func(s *Settings) { s.MQTT.Port = 70000 },
		"host url":  func(s *Settings) { s.MQTT.Host = "mqtt://broker" },
		"host path": func(s *Settings) { s.MQTT.Host = "broker/x" },
		"no host":   func(s *Settings) { s.MQTT.Enabled = true },
		"poll":      func(s *Settings) { s.PollSeconds = 1 },
		"off mode":  func(s *Settings) { s.OffMode = "explode" },
		"wildcard":  func(s *Settings) { s.MQTT.BaseTopic = "nodoff/#" },
		"node":      func(s *Settings) { s.MQTT.NodeName = "a\nb" },
	}
	for name, mod := range cases {
		s := Defaults("pc")
		mod(&s)
		if s.Normalise() == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	s := Defaults("pc")
	s.Devices["../../evil"] = Device{Exposed: true}
	s.Devices["len65fa_v9k27xqa"] = Device{Exposed: true, Name: "  Left  "}
	if err := s.Normalise(); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Devices["../../evil"]; ok {
		t.Fatal("bad device id kept")
	}
	if s.Devices["len65fa_v9k27xqa"].Name != "Left" {
		t.Fatal("name not trimmed")
	}
}

func TestStoreRoundTripAndBrokenFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	st, err := Load(path, "pc")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Update(func(s *Settings) error { s.MQTT.Host = "broker.lan"; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Update(func(s *Settings) error { s.MQTT.Port = -1; return nil }); err == nil {
		t.Fatal("bad update accepted")
	}
	st2, err := Load(path, "pc")
	if err != nil || st2.Get().MQTT.Host != "broker.lan" || st2.Get().MQTT.Port != 1883 {
		t.Fatalf("reload: %v %+v", err, st2.Get().MQTT)
	}
	os.WriteFile(path, []byte("{not json"), 0o600)
	st3, err := Load(path, "pc")
	if err == nil || st3.Get().MQTT.Host != "" {
		t.Fatal("broken file should fall back to defaults with an error")
	}
	if m, _ := filepath.Glob(path + ".broken-*"); len(m) != 1 {
		t.Fatal("broken file not kept")
	}
}
