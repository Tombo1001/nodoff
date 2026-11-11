package ddc

import "testing"

// Captured from a Lenovo L28u-30.
const l28u = "(prot(monitor)type(LCD)model(L28u_30)cmds(01 02 03 07 0C E3 F3)vcp(02 04 05 08 10 12 14(01 05 06 08 0B) 16 18 1A 52 60(0F 11) AC AE B2 B6 C6 C8 C9 CA(01 02) CC(02 03 04 05 06 09 0A 0D) D6(01 04 05) DF E0(00 01) EB(00 01) )mswhql(1)asset_eep(40)mccs_ver(2.2))"

func TestParseCaps(t *testing.T) {
	c := ParseCaps(l28u)
	if c.Model != "L28u_30" || c.MCCS != "2.2" {
		t.Fatalf("model %q mccs %q", c.Model, c.MCCS)
	}
	if !c.Supports(VCPBrightness) || !c.Supports(VCPPowerMode) || c.Supports(0x62) {
		t.Fatalf("supports: %v", c.VCP)
	}
	if !c.Allows(VCPPowerMode, PowerStandby) || !c.Allows(VCPPowerMode, PowerOff) || c.Allows(VCPPowerMode, 2) {
		t.Fatalf("D6 values: %v", c.VCP[VCPPowerMode])
	}
	if got := c.VCP[VCPInput]; len(got) != 2 || got[0] != 0x0f || got[1] != 0x11 {
		t.Fatalf("inputs: %v", got)
	}
	if !c.Allows(VCPBrightness, 73) {
		t.Fatal("brightness has no list, so any value is allowed")
	}
}

func TestParseCapsSloppy(t *testing.T) {
	for _, s := range []string{"", "(", "vcp(", "(vcp(10 zz 12(01 02", "(prot(monitor)vcp(10)(12))", "(model(x)vcp(D6(01 04)"} {
		c := ParseCaps(s) // must not panic
		_ = c.Supports(VCPBrightness)
	}
	c := ParseCaps("(vcp(10 12 D6(01 04)")
	if !c.Supports(VCPPowerMode) || !c.Allows(VCPPowerMode, 4) {
		t.Fatalf("truncated string: %v", c.VCP)
	}
	// A nested model( inside another group must not be read as the top-level model.
	if m := ParseCaps("(x(model(bad))model(good))").Model; m != "good" {
		t.Fatalf("model %q", m)
	}
}

func edidFixture() []byte {
	b := make([]byte, 128)
	copy(b, edidHeader)
	// "LEN" = L(12) E(5) N(14) -> 0b0_01100_00101_01110 = 0x30AE
	b[8], b[9] = 0x30, 0xAE
	b[10], b[11] = 0xFA, 0x65 // product 0x65FA
	b[12] = 0x2a              // numeric serial 42
	b[17] = 31                // 2021
	put := func(off int, tag byte, text string) {
		b[off+3] = tag
		s := []byte(text + "\n            ")
		copy(b[off+5:off+18], s)
	}
	put(54+18, 0xff, "V9K27XQA")
	put(54+36, 0xfc, "LEN L28u-30")
	b[54] = 1 // first descriptor is a timing, not a display descriptor
	return b
}

func TestParseEDID(t *testing.T) {
	e, err := ParseEDID(edidFixture())
	if err != nil {
		t.Fatal(err)
	}
	if e.Maker != "LEN" || e.Product != 0x65fa || e.Serial != "V9K27XQA" || e.Name != "LEN L28u-30" || e.Year != 2021 {
		t.Fatalf("%+v", e)
	}
	if id := e.ID("x"); id != "len65fa_v9k27xqa" {
		t.Fatalf("id %q", id)
	}
	if _, err := ParseEDID(make([]byte, 128)); err == nil {
		t.Fatal("bad header accepted")
	}
	if _, err := ParseEDID([]byte{0}); err == nil {
		t.Fatal("short edid accepted")
	}
}

func TestSlugAndInstance(t *testing.T) {
	if s := Slug("  DISPLAY\\LEN65FA\\4&1d4&&UID4353 "); s != "display_len65fa_4_1d4_uid4353" {
		t.Fatalf("slug %q", s)
	}
	got := InstanceFromInterface(`\\?\DISPLAY#LEN65FA#4&2a51c3e7&0&UID4353#{e6f07b5f-ee97-4a90-b076-33f57bf4eaa7}`)
	if got != `DISPLAY\LEN65FA\4&2a51c3e7&0&UID4353` {
		t.Fatalf("instance %q", got)
	}
}
