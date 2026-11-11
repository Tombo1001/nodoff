// Package ddc talks to monitors over DDC/CI (the I2C side channel in HDMI, DisplayPort
// and DVI cables). The parsing in this file is platform-neutral so it can be tested
// anywhere; the Windows calls live in ddc_windows.go.
package ddc

import (
	"strconv"
	"strings"
)

// VCP codes nodoff uses. MCCS 2.2 names them.
const (
	VCPBrightness = 0x10
	VCPContrast   = 0x12
	VCPInput      = 0x60
	VCPPowerMode  = 0xD6
)

// Values of VCP 0xD6 (power mode).
const (
	PowerOn      = 1 // DPM on
	PowerStandby = 4 // DPM off: the monitor sleeps and wakes on a DDC command or a signal change
	PowerOff     = 5 // as if the power button was pressed; some monitors won't wake from this over DDC
)

// Caps is a parsed MCCS capabilities string.
type Caps struct {
	Raw   string
	Model string
	MCCS  string
	// VCP maps each supported code to its allowed values (nil when the monitor gives
	// no list, which means "any value up to the maximum").
	VCP map[byte][]uint16
}

// Supports reports whether the monitor lists the VCP code.
func (c Caps) Supports(code byte) bool {
	if c.VCP == nil {
		return false
	}
	_, ok := c.VCP[code]
	return ok
}

// Allows reports whether value is allowed for code. A code with no value list
// allows anything.
func (c Caps) Allows(code byte, value uint16) bool {
	vals, ok := c.VCP[code]
	if !ok {
		return false
	}
	if len(vals) == 0 {
		return true
	}
	for _, v := range vals {
		if v == value {
			return true
		}
	}
	return false
}

// ParseCaps reads a capabilities string such as
// "(prot(monitor)type(LCD)model(L28u_30)vcp(02 04 10 12 60(0F 11) D6(01 04 05))mccs_ver(2.2))".
// Monitors are sloppy with these strings, so it never fails: anything it can't read is skipped.
func ParseCaps(s string) Caps {
	c := Caps{Raw: s, VCP: map[byte][]uint16{}}
	c.Model = group(s, "model")
	c.MCCS = group(s, "mccs_ver")
	vcp := group(s, "vcp")
	if vcp == "" {
		return c
	}
	// Walk the vcp group: hex codes, each optionally followed by a (...) list of values.
	i := 0
	for i < len(vcp) {
		for i < len(vcp) && (vcp[i] == ' ' || vcp[i] == '\t') {
			i++
		}
		if i >= len(vcp) {
			break
		}
		j := i
		for j < len(vcp) && isHex(vcp[j]) {
			j++
		}
		if j == i {
			i++ // stray character
			continue
		}
		code, err := strconv.ParseUint(vcp[i:j], 16, 8)
		i = j
		var vals []uint16
		for i < len(vcp) && vcp[i] == ' ' {
			i++
		}
		if i < len(vcp) && vcp[i] == '(' {
			end := matching(vcp, i)
			for _, f := range strings.Fields(vcp[i+1 : end]) {
				if v, err := strconv.ParseUint(f, 16, 16); err == nil {
					vals = append(vals, uint16(v))
				}
			}
			i = end + 1
		}
		if err == nil {
			c.VCP[byte(code)] = vals
		}
	}
	return c
}

// group returns the contents of name(...) at nesting depth 1, or "".
func group(s, name string) string {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
		default:
			if depth == 1 && strings.HasPrefix(s[i:], name+"(") && (i == 0 || !isWordByte(s[i-1])) {
				open := i + len(name)
				end := matching(s, open)
				return s[open+1 : end]
			}
		}
	}
	return ""
}

// matching returns the index of the ")" that closes the "(" at open, or len(s) if
// the string is cut short.
func matching(s string, open int) int {
	depth := 0
	for k := open; k < len(s); k++ {
		switch s[k] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return k
			}
		}
	}
	return len(s)
}

func isHex(b byte) bool {
	return b >= '0' && b <= '9' || b >= 'a' && b <= 'f' || b >= 'A' && b <= 'F'
}

func isWordByte(b byte) bool {
	return b == '_' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}
