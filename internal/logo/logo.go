// Package logo draws the nodoff mark: a sleepy, half-closed eye on a dark rounded
// tile shaped like a screen. Amber when nodoff is connected, grey when it isn't.
//
// It's drawn per pixel with 4x4 supersampling rather than from a bitmap, so every
// size from the 16 px tray icon up to 256 px stays sharp, with no image files to ship.
package logo

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"math"
)

var (
	Amber = color.NRGBA{0xff, 0xb5, 0x47, 0xff}
	Grey  = color.NRGBA{0x86, 0x86, 0x8e, 0xff}
	Tile  = color.NRGBA{0x1d, 0x1d, 0x1f, 0xff}
	Edge  = color.NRGBA{0x3a, 0x3a, 0x3e, 0xff}
)

// Render draws the mark at size×size. eye is Amber or Grey.
func Render(size int, eye color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	s := float64(size)
	small := size <= 24
	// Stroke width grows at tray sizes so the eye survives being 16 px across.
	t := s * 0.075
	if small {
		t = math.Max(1.6, s*0.11)
	}
	const ss = 4
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			var r, g, b, a float64
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					x := (float64(px) + (float64(sx)+0.5)/ss) / s
					y := (float64(py) + (float64(sy)+0.5)/ss) / s
					c, ok := sample(x, y, t/s, small, eye)
					if !ok {
						continue
					}
					r += float64(c.R)
					g += float64(c.G)
					b += float64(c.B)
					a++
				}
			}
			if a == 0 {
				continue
			}
			img.SetNRGBA(px, py, color.NRGBA{
				uint8(r / a), uint8(g / a), uint8(b / a), uint8(a / (ss * ss) * 255),
			})
		}
	}
	return img
}

// sample returns the colour at (x, y) in unit coordinates, or false outside the tile.
func sample(x, y, t float64, small bool, eye color.NRGBA) (color.NRGBA, bool) {
	// Tile: a rounded square, inset slightly so the anti-aliased edge isn't clipped.
	inset, radius := 0.02, 0.22
	if !inRoundRect(x, y, inset, inset, 1-inset, 1-inset, radius) {
		return color.NRGBA{}, false
	}
	col := Tile
	if !small && !inRoundRect(x, y, inset+0.018, inset+0.018, 1-inset-0.018, 1-inset-0.018, radius-0.018) {
		col = Edge // a faint bezel at large sizes
	}

	// The eye: a lens between two parabolas, centred a little low.
	cx, cy := 0.5, 0.54
	w, h := 0.34, 0.20 // half-width, half-height
	if small {
		w, h = 0.40, 0.24
	}
	lid := cy - h*0.12 // the lid has dropped to just above the middle
	inLens := func(grow float64) bool {
		uu := (x - cx) / (w + grow)
		if uu <= -1 || uu >= 1 {
			return false
		}
		half := (h + grow) * (1 - uu*uu)
		return math.Abs(y-cy) < half
	}
	// Lid line: straight across the lens at the lid height, with round ends at the corners.
	reach := w * math.Sqrt(1-math.Abs(lid-cy)/h)
	if nearSegment(x, y, cx-reach, lid, cx+reach, lid, t/2) {
		return eye, true
	}
	// A small "z" at large sizes, drifting off the top-right.
	if !small {
		zx, zy, zs := 0.70, 0.17, 0.11
		zt := t * 0.55
		if nearSegment(x, y, zx, zy, zx+zs, zy, zt/2) ||
			nearSegment(x, y, zx+zs, zy, zx, zy+zs, zt/2) ||
			nearSegment(x, y, zx, zy+zs, zx+zs, zy+zs, zt/2) {
			return eye, true
		}
	}
	if y > lid {
		// Lower outline of the lens.
		if inLens(t/2) && !inLens(-t/2) {
			return eye, true
		}
		// Pupil, cut flat by the lid.
		pr := h * 0.62
		if small {
			pr = h * 0.70
		}
		dx, dy := x-cx, y-(cy+h*0.06)
		if dx*dx+dy*dy < pr*pr && inLens(-t/2) {
			return eye, true
		}
	}
	return col, true
}

// nearSegment reports whether (x, y) is within r of the segment (x0,y0)-(x1,y1).
func nearSegment(x, y, x0, y0, x1, y1, r float64) bool {
	dx, dy := x1-x0, y1-y0
	k := 0.0
	if l := dx*dx + dy*dy; l > 0 {
		k = math.Min(1, math.Max(0, ((x-x0)*dx+(y-y0)*dy)/l))
	}
	ex, ey := x-(x0+k*dx), y-(y0+k*dy)
	return ex*ex+ey*ey <= r*r
}

func inRoundRect(x, y, x0, y0, x1, y1, r float64) bool {
	if x < x0 || x > x1 || y < y0 || y > y1 {
		return false
	}
	cx := math.Min(math.Max(x, x0+r), x1-r)
	cy := math.Min(math.Max(y, y0+r), y1-r)
	dx, dy := x-cx, y-cy
	return dx*dx+dy*dy <= r*r
}

// PNG returns one render as PNG bytes.
func PNG(size int, eye color.NRGBA) []byte {
	var b bytes.Buffer
	_ = png.Encode(&b, Render(size, eye))
	return b.Bytes()
}

// ICO packs PNG renders at the given sizes into one .ico file.
func ICO(eye color.NRGBA, sizes ...int) []byte {
	pngs := make([][]byte, len(sizes))
	for i, n := range sizes {
		pngs[i] = PNG(n, eye)
	}
	var out bytes.Buffer
	binary.Write(&out, binary.LittleEndian, []uint16{0, 1, uint16(len(sizes))})
	offset := 6 + 16*len(sizes)
	for i, n := range sizes {
		d := byte(n)
		if n >= 256 {
			d = 0
		}
		out.Write([]byte{d, d, 0, 0})
		binary.Write(&out, binary.LittleEndian, []uint16{1, 32})
		binary.Write(&out, binary.LittleEndian, []uint32{uint32(len(pngs[i])), uint32(offset)})
		offset += len(pngs[i])
	}
	for _, p := range pngs {
		out.Write(p)
	}
	return out.Bytes()
}
