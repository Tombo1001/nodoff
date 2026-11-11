package main

import "testing"

func TestCtrlTap(t *testing.T) {
	const down, up, ctrl, c, v = 0x0100, 0x0101, 0xA2, 0x43, 0x56
	type ev struct{ msg, vk, flags, t uint32 }
	cases := []struct {
		name string
		evs  []ev
		want int // how many times it fires
	}{
		{"double tap", []ev{{down, ctrl, 0, 0}, {up, ctrl, 0, 80}, {down, ctrl, 0, 200}, {up, ctrl, 0, 260}}, 1},
		{"triple tap fires once", []ev{{down, ctrl, 0, 0}, {up, ctrl, 0, 80}, {down, ctrl, 0, 200}, {up, ctrl, 0, 260}, {down, ctrl, 0, 380}, {up, ctrl, 0, 440}}, 1},
		{"too slow", []ev{{down, ctrl, 0, 0}, {up, ctrl, 0, 80}, {down, ctrl, 0, 600}, {up, ctrl, 0, 660}}, 0},
		{"first press held", []ev{{down, ctrl, 0, 0}, {up, ctrl, 0, 500}, {down, ctrl, 0, 600}, {up, ctrl, 0, 660}}, 0},
		{"auto-repeat while held", []ev{{down, ctrl, 0, 0}, {down, ctrl, 0, 30}, {down, ctrl, 0, 60}, {up, ctrl, 0, 90}}, 0},
		{"copy then paste", []ev{{down, ctrl, 0, 0}, {down, c, 0, 40}, {up, c, 0, 80}, {up, ctrl, 0, 120}, {down, ctrl, 0, 200}, {down, v, 0, 240}, {up, v, 0, 280}, {up, ctrl, 0, 320}}, 0},
		{"key between taps", []ev{{down, ctrl, 0, 0}, {up, ctrl, 0, 80}, {down, c, 0, 120}, {up, c, 0, 160}, {down, ctrl, 0, 200}}, 0},
		{"injected", []ev{{down, ctrl, 0x10, 0}, {up, ctrl, 0x10, 80}, {down, ctrl, 0x10, 200}}, 0},
	}
	for _, tc := range cases {
		tap = struct {
			down         bool
			downAt, upAt uint32
			tapped       bool
			other        bool
			skipUp       bool
		}{}
		got := 0
		for _, e := range tc.evs {
			if ctrlTap(e.msg, e.vk, e.flags, e.t) {
				got++
			}
		}
		if got != tc.want {
			t.Errorf("%s: fired %d times, want %d", tc.name, got, tc.want)
		}
	}
}
