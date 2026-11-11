package ddc

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

// EDID holds the parts of a monitor's EDID block that identify it.
type EDID struct {
	Maker   string // three-letter PNP ID, e.g. "LEN"
	Product uint16
	Serial  string // the serial-number descriptor if present, else the numeric serial
	Name    string // the monitor-name descriptor, e.g. "LEN L28u-30"
	Year    int
}

var edidHeader = []byte{0, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0}

// ParseEDID reads the base 128-byte block.
func ParseEDID(b []byte) (EDID, error) {
	var e EDID
	if len(b) < 128 {
		return e, errors.New("edid: shorter than 128 bytes")
	}
	for i, v := range edidHeader {
		if b[i] != v {
			return e, errors.New("edid: bad header")
		}
	}
	m := binary.BigEndian.Uint16(b[8:10])
	e.Maker = string([]byte{
		byte('A' - 1 + (m>>10)&0x1f),
		byte('A' - 1 + (m>>5)&0x1f),
		byte('A' - 1 + m&0x1f),
	})
	e.Product = binary.LittleEndian.Uint16(b[10:12])
	if n := binary.LittleEndian.Uint32(b[12:16]); n != 0 {
		e.Serial = fmt.Sprint(n)
	}
	if b[17] != 0 {
		e.Year = 1990 + int(b[17])
	}
	// Four 18-byte descriptors from offset 54. Display descriptors start 00 00 00 <tag>.
	for d := 54; d+18 <= 126; d += 18 {
		if b[d] != 0 || b[d+1] != 0 || b[d+2] != 0 {
			continue
		}
		text := descText(b[d+5 : d+18])
		switch b[d+3] {
		case 0xff:
			if text != "" {
				e.Serial = text
			}
		case 0xfc:
			e.Name = text
		}
	}
	return e, nil
}

// descText trims a descriptor string, which ends at a newline and is padded with spaces.
func descText(b []byte) string {
	s := string(b)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r > 0x7e {
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}

// ID builds a stable identifier from the EDID: maker, product code and serial,
// lower case, safe for MQTT topics and HA unique IDs. Two identical monitors differ
// by serial. A monitor with no serial falls back to fallback (the Windows instance ID).
func (e EDID) ID(fallback string) string {
	serial := e.Serial
	if serial == "" {
		serial = fallback
	}
	return Slug(fmt.Sprintf("%s%04x_%s", e.Maker, e.Product, serial))
}

// Slug lower-cases s and keeps only a-z, 0-9 and single underscores.
func Slug(s string) string {
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

// InstanceFromInterface turns a device interface path such as
// `\\?\DISPLAY#LEN65FA#4&2a51c3e7&0&UID4353#{e6f07b5f-...}` into the device
// instance ID `DISPLAY\LEN65FA\4&2a51c3e7&0&UID4353`.
func InstanceFromInterface(path string) string {
	p := strings.TrimPrefix(path, `\\?\`)
	if i := strings.LastIndex(p, "#{"); i >= 0 {
		p = p[:i]
	}
	return strings.ReplaceAll(p, "#", `\`)
}
