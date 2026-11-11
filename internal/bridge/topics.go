// Package bridge publishes monitors to an MQTT broker and turns MQTT commands into
// monitor changes. Home Assistant discovery is optional; the plain topics work with
// anything that speaks MQTT.
package bridge

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Topics for one bridge. Base is e.g. "nodoff/desk-pc".
//
//	<base>/status                    online | offline (retained, last will)
//	<base>/<id>/available            online | offline (retained)
//	<base>/<id>/power                ON | OFF (retained)
//	<base>/<id>/power/set            ON | OFF
//	<base>/<id>/brightness           0-100 (retained)
//	<base>/<id>/brightness/set       0-100
type Topics struct {
	Base            string
	DiscoveryPrefix string
	Node            string // discovery node ID, [a-z0-9_]
}

func NewTopics(base, discoveryPrefix string) Topics {
	// Node is the base topic as one ID level, e.g. nodoff/desk-pc -> nodoff_desk_pc.
	node := slug(base)
	if !strings.HasPrefix(node, "nodoff") {
		node = "nodoff_" + node
	}
	return Topics{Base: base, DiscoveryPrefix: discoveryPrefix, Node: node}
}

func (t Topics) Status() string                 { return t.Base + "/status" }
func (t Topics) Available(id string) string     { return t.Base + "/" + id + "/available" }
func (t Topics) Power(id string) string         { return t.Base + "/" + id + "/power" }
func (t Topics) PowerSet(id string) string      { return t.Base + "/" + id + "/power/set" }
func (t Topics) Brightness(id string) string    { return t.Base + "/" + id + "/brightness" }
func (t Topics) BrightnessSet(id string) string { return t.Base + "/" + id + "/brightness/set" }
func (t Topics) CommandFilters() []string {
	return []string{t.Base + "/+/power/set", t.Base + "/+/brightness/set"}
}
func (t Topics) HAStatus() string { return t.DiscoveryPrefix + "/status" }
func (t Topics) Discovery(id string) string {
	return t.DiscoveryPrefix + "/light/" + t.Node + "/" + id + "/config"
}

// Command is a parsed MQTT command.
type Command struct {
	ID         string
	Power      *bool
	Brightness *int
}

// ParseCommand reads a message on one of the command topics. ok is false for anything
// that isn't a well-formed command for a monitor in exposed.
func (t Topics) ParseCommand(topic string, payload []byte, exposed map[string]bool) (Command, bool) {
	rest, found := strings.CutPrefix(topic, t.Base+"/")
	if !found || len(payload) > 32 {
		return Command{}, false
	}
	parts := strings.Split(rest, "/")
	if len(parts) != 3 || parts[2] != "set" || !exposed[parts[0]] {
		return Command{}, false
	}
	p := strings.ToUpper(strings.TrimSpace(string(payload)))
	c := Command{ID: parts[0]}
	switch parts[1] {
	case "power":
		switch p {
		case "ON", "1", "TRUE":
			on := true
			c.Power = &on
		case "OFF", "0", "FALSE":
			off := false
			c.Power = &off
		default:
			return Command{}, false
		}
	case "brightness":
		f, err := strconv.ParseFloat(p, 64)
		if err != nil || f != f || f < 0 || f > 100 {
			return Command{}, false
		}
		v := int(f + 0.5)
		c.Brightness = &v
	default:
		return Command{}, false
	}
	return c, true
}

// Info is what the discovery config needs about the PC.
type Info struct {
	NodeName string
	Version  string
	Model    string // e.g. "Windows PC"
}

// DiscoveryConfig is the Home Assistant MQTT light config for one monitor.
func (t Topics) DiscoveryConfig(id, name string, info Info) []byte {
	cfg := map[string]any{
		"name":                     name,
		"unique_id":                t.Node + "_" + id,
		"icon":                     "mdi:monitor",
		"command_topic":            t.PowerSet(id),
		"state_topic":              t.Power(id),
		"payload_on":               "ON",
		"payload_off":              "OFF",
		"brightness_command_topic": t.BrightnessSet(id),
		"brightness_state_topic":   t.Brightness(id),
		"brightness_scale":         100,
		"on_command_type":          "last",
		"retain":                   false,
		"availability_mode":        "all",
		"availability": []map[string]string{
			{"topic": t.Status()},
			{"topic": t.Available(id)},
		},
		"device": map[string]any{
			"identifiers":  []string{t.Node},
			"name":         info.NodeName,
			"manufacturer": "nodoff",
			"model":        info.Model,
			"sw_version":   info.Version,
		},
		"origin": map[string]string{
			"name":       "nodoff",
			"sw_version": info.Version,
		},
	}
	b, _ := json.Marshal(cfg)
	return b
}

func slug(s string) string {
	var b strings.Builder
	under := false
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			under = false
		} else if !under && b.Len() > 0 {
			b.WriteByte('_')
			under = true
		}
	}
	return strings.TrimRight(b.String(), "_")
}
