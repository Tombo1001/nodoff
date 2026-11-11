package main

// ctrlTap follows key events and returns true on the second press of a double tap:
// Ctrl pressed and released within 300 ms, then pressed again within 400 ms, with no
// other key in between. Ctrl+C followed quickly by Ctrl+V never counts.
//
// msg is the Windows key message, vk the virtual key, flags the low-level hook flags
// and t the event time in milliseconds. Only the hook thread calls it.
func ctrlTap(msg, vk, flags, t uint32) bool {
	const wmKeyDown, wmSysKeyDown, injected = 0x0100, 0x0104, 0x10
	if flags&injected != 0 {
		return false // typed by a program, not a person
	}
	if vk != 0x11 && vk != 0xA2 && vk != 0xA3 { // VK_CONTROL, VK_LCONTROL, VK_RCONTROL
		tap.tapped, tap.other = false, true
		return false
	}
	if msg == wmKeyDown || msg == wmSysKeyDown {
		if tap.down {
			return false // auto-repeat
		}
		tap.down, tap.downAt, tap.other = true, t, false
		if tap.tapped && t-tap.upAt <= 400 {
			tap.tapped, tap.skipUp = false, true
			return true
		}
		tap.tapped = false
		return false
	}
	tap.down = false
	tap.tapped = !tap.skipUp && !tap.other && t-tap.downAt <= 300
	tap.skipUp = false
	tap.upAt = t
	return false
}

var tap struct {
	down   bool   // Ctrl is held
	downAt uint32 // when it went down, ms
	upAt   uint32 // when it was last released, ms
	tapped bool   // one short tap seen, nothing else pressed since
	other  bool   // another key was pressed since Ctrl went down
	skipUp bool   // the release after a double tap doesn't start a new one
}
