package bridge

import (
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net"
	"strconv"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/eclipse/paho.mqtt.golang/packets"
)

// Display is one monitor's state as the bridge sees it.
type Display struct {
	ID         string
	Present    bool   // plugged in and enumerated right now
	Power      string // "ON", "OFF" or "" when unknown
	Brightness int    // 0-100, or -1 when unknown
}

// Controller changes monitors. The app supplies it; it does the DDC/CI work.
type Controller interface {
	SetPower(id string, on bool) error
	SetBrightness(id string, pct int) error
	// Refresh re-reads the given monitor and hands the result back through
	// Bridge.Publish (the app does that).
	Refresh(id string)
}

// Config is what a bridge needs to connect.
type Config struct {
	Host      string
	Port      int
	TLS       bool
	Username  string
	Password  string
	ClientID  string
	Topics    Topics
	Discovery bool
	Info      Info
}

// Status is shown in the settings panel and the tray tooltip.
type Status struct {
	Connected bool   `json:"connected"`
	Error     string `json:"error,omitempty"`
	Since     string `json:"since,omitempty"`
}

// Bridge is one MQTT connection. Make a new one when the connection settings change.
type Bridge struct {
	cfg    Config
	ctl    Controller
	client mqtt.Client

	mu       sync.Mutex
	exposed  map[string]string  // id -> name for every monitor published
	last     map[string]Display // last state seen, republished on reconnect
	status   Status
	cmds     chan Command
	stop     chan struct{}
	stopped  bool
	onStatus func(Status)
}

// New prepares a bridge. Nothing connects until Start.
func New(cfg Config, ctl Controller, onStatus func(Status)) *Bridge {
	b := &Bridge{
		cfg:      cfg,
		ctl:      ctl,
		exposed:  map[string]string{},
		last:     map[string]Display{},
		cmds:     make(chan Command, 64),
		stop:     make(chan struct{}),
		onStatus: onStatus,
	}
	o := mqtt.NewClientOptions()
	scheme := "tcp"
	if cfg.TLS {
		scheme = "ssl"
		o.SetTLSConfig(&tls.Config{MinVersion: tls.VersionTLS12, ServerName: cfg.Host})
	}
	o.AddBroker(scheme + "://" + net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)))
	o.SetClientID(cfg.ClientID)
	o.SetUsername(cfg.Username)
	o.SetPassword(cfg.Password)
	o.SetCleanSession(true)
	o.SetOrderMatters(false)
	o.SetAutoReconnect(true)
	o.SetMaxReconnectInterval(2 * time.Minute)
	o.SetConnectTimeout(10 * time.Second)
	o.SetKeepAlive(30 * time.Second)
	o.SetWill(cfg.Topics.Status(), "offline", 1, true)
	o.SetOnConnectHandler(func(c mqtt.Client) { go b.onConnect() })
	o.SetConnectionLostHandler(func(c mqtt.Client, err error) {
		b.setStatus(false, "connection lost: "+err.Error())
	})
	b.client = mqtt.NewClient(o)
	return b
}

// Start connects in the background and keeps retrying until it gets in or Stop is called.
// After the first connection paho reconnects by itself.
func (b *Bridge) Start() {
	go b.worker()
	go func() {
		wait := 5 * time.Second
		for {
			t := b.client.Connect()
			if !t.WaitTimeout(15 * time.Second) {
				b.setStatus(false, "no answer from "+net.JoinHostPort(b.cfg.Host, strconv.Itoa(b.cfg.Port))+"; still trying")
			} else if err := t.Error(); err != nil {
				b.setStatus(false, friendly(err))
			} else {
				return // onConnect reports success
			}
			select {
			case <-b.stop:
				return
			case <-time.After(wait):
			}
			if wait < 2*time.Minute {
				wait *= 2
			}
		}
	}()
}

// Stop disconnects. With clearDiscovery it first removes this PC's monitors from Home
// Assistant and clears their retained state.
func (b *Bridge) Stop(clearDiscovery bool) {
	b.mu.Lock()
	if b.stopped {
		b.mu.Unlock()
		return
	}
	b.stopped = true
	ids := make([]string, 0, len(b.exposed))
	for id := range b.exposed {
		ids = append(ids, id)
	}
	b.mu.Unlock()
	close(b.stop)
	if b.client.IsConnectionOpen() {
		if clearDiscovery {
			for _, id := range ids {
				b.clear(id)
			}
		}
		b.publish(b.cfg.Topics.Status(), "offline", true)
		b.client.Disconnect(500)
	} else {
		b.client.Disconnect(0)
	}
}

// SetDevices sets which monitors are published, by ID with their display names.
// Monitors dropped from the set are removed from Home Assistant.
func (b *Bridge) SetDevices(devs map[string]string) {
	b.mu.Lock()
	var removed, changed []string
	for id := range b.exposed {
		if _, ok := devs[id]; !ok {
			removed = append(removed, id)
		}
	}
	for id, name := range devs {
		if old, ok := b.exposed[id]; !ok || old != name {
			changed = append(changed, id)
		}
	}
	b.exposed = map[string]string{}
	for id, name := range devs {
		b.exposed[id] = name
	}
	b.mu.Unlock()
	if !b.client.IsConnectionOpen() {
		return
	}
	for _, id := range removed {
		b.clear(id)
	}
	for _, id := range changed {
		b.announce(id)
		b.ctl.Refresh(id)
	}
}

// Publish sends a monitor's state. Unknown values are left as they were.
func (b *Bridge) Publish(d Display) {
	b.mu.Lock()
	_, exposed := b.exposed[d.ID]
	b.last[d.ID] = d
	b.mu.Unlock()
	if !exposed || !b.client.IsConnectionOpen() {
		return
	}
	t := b.cfg.Topics
	if !d.Present {
		b.publish(t.Available(d.ID), "offline", true)
		return
	}
	b.publish(t.Available(d.ID), "online", true)
	if d.Power != "" {
		b.publish(t.Power(d.ID), d.Power, true)
	}
	if d.Brightness >= 0 {
		b.publish(t.Brightness(d.ID), strconv.Itoa(d.Brightness), true)
	}
}

func (b *Bridge) onConnect() {
	t := b.cfg.Topics
	b.publish(t.Status(), "online", true)
	handler := func(c mqtt.Client, m mqtt.Message) {
		b.mu.Lock()
		ex := make(map[string]bool, len(b.exposed))
		for id := range b.exposed {
			ex[id] = true
		}
		b.mu.Unlock()
		cmd, ok := t.ParseCommand(m.Topic(), m.Payload(), ex)
		if !ok {
			return
		}
		select {
		case b.cmds <- cmd:
		default:
			log.Printf("mqtt: command queue full, dropped a command for %s", cmd.ID)
		}
	}
	filters := map[string]byte{}
	for _, f := range t.CommandFilters() {
		filters[f] = 1
	}
	if tok := b.client.SubscribeMultiple(filters, handler); tok.WaitTimeout(10*time.Second) && tok.Error() != nil {
		b.setStatus(false, "subscribe failed: "+tok.Error().Error())
		return
	}
	if b.cfg.Discovery {
		// Home Assistant asks everyone to announce again when it restarts.
		b.client.Subscribe(t.HAStatus(), 1, func(c mqtt.Client, m mqtt.Message) {
			if string(m.Payload()) == "online" {
				go b.announceAll()
			}
		})
	}
	b.setStatus(true, "")
	b.announceAll()
}

func (b *Bridge) announceAll() {
	b.mu.Lock()
	ids := make([]string, 0, len(b.exposed))
	for id := range b.exposed {
		ids = append(ids, id)
	}
	b.mu.Unlock()
	for _, id := range ids {
		b.announce(id)
		b.mu.Lock()
		d, ok := b.last[id]
		b.mu.Unlock()
		if ok {
			b.Publish(d)
		} else {
			b.ctl.Refresh(id)
		}
	}
}

func (b *Bridge) announce(id string) {
	if !b.cfg.Discovery {
		return
	}
	b.mu.Lock()
	name := b.exposed[id]
	b.mu.Unlock()
	b.publishBytes(b.cfg.Topics.Discovery(id), b.cfg.Topics.DiscoveryConfig(id, name, b.cfg.Info), true)
}

// clear removes a monitor from Home Assistant and wipes its retained topics.
func (b *Bridge) clear(id string) {
	t := b.cfg.Topics
	if b.cfg.Discovery {
		b.publish(t.Discovery(id), "", true)
	}
	for _, topic := range []string{t.Available(id), t.Power(id), t.Brightness(id)} {
		b.publish(topic, "", true)
	}
}

// worker applies commands one at a time. A run of brightness commands for the same
// monitor (a slider being dragged) collapses to the newest.
func (b *Bridge) worker() {
	for {
		var c Command
		select {
		case <-b.stop:
			return
		case c = <-b.cmds:
		}
		batch := []Command{c}
	drain:
		for {
			select {
			case n := <-b.cmds:
				batch = append(batch, n)
			default:
				break drain
			}
		}
		for i, c := range batch {
			if c.Brightness != nil && laterBrightness(batch[i+1:], c.ID) {
				continue
			}
			b.apply(c)
		}
	}
}

func laterBrightness(rest []Command, id string) bool {
	for _, n := range rest {
		if n.ID == id && n.Brightness != nil {
			return true
		}
	}
	return false
}

func (b *Bridge) apply(c Command) {
	var err error
	switch {
	case c.Power != nil:
		err = b.ctl.SetPower(c.ID, *c.Power)
	case c.Brightness != nil:
		err = b.ctl.SetBrightness(c.ID, *c.Brightness)
	}
	if err != nil {
		log.Printf("mqtt command for %s: %v", c.ID, err)
	}
	b.ctl.Refresh(c.ID)
}

func (b *Bridge) publish(topic, payload string, retained bool) {
	b.publishBytes(topic, []byte(payload), retained)
}

func (b *Bridge) publishBytes(topic string, payload []byte, retained bool) {
	t := b.client.Publish(topic, 1, retained, payload)
	if !t.WaitTimeout(5*time.Second) || t.Error() != nil {
		log.Printf("mqtt publish %s: %v", topic, t.Error())
	}
}

func (b *Bridge) setStatus(ok bool, msg string) {
	b.mu.Lock()
	if b.stopped {
		b.mu.Unlock()
		return
	}
	changed := b.status.Connected != ok || b.status.Error != msg
	b.status = Status{Connected: ok, Error: msg, Since: time.Now().Format(time.RFC3339)}
	s := b.status
	b.mu.Unlock()
	if changed {
		if ok {
			log.Printf("mqtt: connected to %s", net.JoinHostPort(b.cfg.Host, strconv.Itoa(b.cfg.Port)))
		} else {
			log.Printf("mqtt: %s", msg)
		}
		if b.onStatus != nil {
			b.onStatus(s)
		}
	}
}

// friendly turns paho's connect errors into something a person can act on.
func friendly(err error) string {
	switch {
	case errors.Is(err, packets.ErrorRefusedBadUsernameOrPassword), errors.Is(err, packets.ErrorRefusedNotAuthorised):
		return "the broker refused the username or password"
	case errors.Is(err, packets.ErrorRefusedIDRejected):
		return "the broker rejected the client ID"
	default:
		return fmt.Sprintf("couldn't connect: %v", err)
	}
}
