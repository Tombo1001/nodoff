package bridge

import (
	"encoding/json"
	"testing"
)

func TestParseCommand(t *testing.T) {
	tp := NewTopics("nodoff/desk-pc", "homeassistant")
	ex := map[string]bool{"len65fa_v9k27xqa": true}
	on, ok := tp.ParseCommand("nodoff/desk-pc/len65fa_v9k27xqa/power/set", []byte("on"), ex)
	if !ok || on.Power == nil || !*on.Power {
		t.Fatalf("power on: %+v %v", on, ok)
	}
	br, ok := tp.ParseCommand("nodoff/desk-pc/len65fa_v9k27xqa/brightness/set", []byte(" 42.6 "), ex)
	if !ok || br.Brightness == nil || *br.Brightness != 43 {
		t.Fatalf("brightness: %+v %v", br, ok)
	}
	bad := []struct{ topic, payload string }{
		{"nodoff/desk-pc/other/power/set", "ON"},                                 // not exposed
		{"nodoff/desk-pc/len65fa_v9k27xqa/power/set", "maybe"},                   // bad payload
		{"nodoff/desk-pc/len65fa_v9k27xqa/brightness/set", "101"},                // out of range
		{"nodoff/desk-pc/len65fa_v9k27xqa/brightness/set", "-1"},                 // out of range
		{"nodoff/desk-pc/len65fa_v9k27xqa/brightness/set", "NaN"},                // not a number
		{"nodoff/desk-pc/len65fa_v9k27xqa/input/set", "1"},                       // unknown control
		{"nodoff/desk-pc/len65fa_v9k27xqa/power", "ON"},                          // state topic, not a command
		{"nodoff/desk-pc-2/len65fa_v9k27xqa/power/set", "ON"},                    // another base
		{"nodoff/desk-pc/len65fa_v9k27xqa/power/set", string(make([]byte, 100))}, // too long
	}
	for _, c := range bad {
		if cmd, ok := tp.ParseCommand(c.topic, []byte(c.payload), ex); ok {
			t.Errorf("%s %q accepted: %+v", c.topic, c.payload, cmd)
		}
	}
}

func TestDiscoveryConfig(t *testing.T) {
	tp := NewTopics("nodoff/desk-pc", "homeassistant")
	if got := tp.Discovery("len65fa_x"); got != "homeassistant/light/nodoff_desk_pc/len65fa_x/config" {
		t.Fatalf("discovery topic %q", got)
	}
	var cfg map[string]any
	if err := json.Unmarshal(tp.DiscoveryConfig("len65fa_x", "Left screen", Info{NodeName: "Desk PC", Version: "0.1.0", Model: "Windows PC"}), &cfg); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"name":                     "Left screen",
		"unique_id":                "nodoff_desk_pc_len65fa_x",
		"command_topic":            "nodoff/desk-pc/len65fa_x/power/set",
		"state_topic":              "nodoff/desk-pc/len65fa_x/power",
		"brightness_command_topic": "nodoff/desk-pc/len65fa_x/brightness/set",
		"brightness_state_topic":   "nodoff/desk-pc/len65fa_x/brightness",
		"brightness_scale":         float64(100),
	}
	for k, v := range want {
		if cfg[k] != v {
			t.Errorf("%s = %v, want %v", k, cfg[k], v)
		}
	}
	dev := cfg["device"].(map[string]any)
	if dev["name"] != "Desk PC" {
		t.Errorf("device name %v", dev["name"])
	}
}

func TestLaterBrightness(t *testing.T) {
	v := 10
	batch := []Command{{ID: "a", Brightness: &v}, {ID: "b", Brightness: &v}, {ID: "a", Brightness: &v}}
	if !laterBrightness(batch[1:], "a") || laterBrightness(batch[1:], "c") {
		t.Fatal("coalescing check wrong")
	}
}
