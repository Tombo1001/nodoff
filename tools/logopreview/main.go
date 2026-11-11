// logopreview writes a sheet of the nodoff mark at every size it ships at, for checking by eye.
// Run from the repo root: go run ./tools/logopreview docs
// Writes logo-sheet.png plus logo-128.png and logo-512.png for the README.
package main

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"

	"nodoff/internal/logo"
)

func main() {
	sizes := []int{16, 20, 24, 32, 48, 64, 128, 256}
	sheet := image.NewNRGBA(image.Rect(0, 0, 900, 600))
	draw.Draw(sheet, sheet.Bounds(), image.NewUniform(color.NRGBA{0xf0, 0xf0, 0xf0, 0xff}), image.Point{}, draw.Src)
	draw.Draw(sheet, image.Rect(0, 300, 900, 600), image.NewUniform(color.NRGBA{0x20, 0x20, 0x24, 0xff}), image.Point{}, draw.Src)
	for _, bgY := range []int{20, 320} {
		x := 20
		for i, c := range []color.NRGBA{logo.Amber, logo.Grey} {
			for _, n := range sizes {
				if i == 1 && n > 48 {
					continue
				}
				img := logo.Render(n, c)
				draw.Draw(sheet, image.Rect(x, bgY, x+n, bgY+n), img, image.Point{}, draw.Over)
				x += n + 14
			}
		}
	}
	dir := os.Args[1]
	write(filepath.Join(dir, "logo-sheet.png"), sheet)
	write(filepath.Join(dir, "logo-128.png"), logo.Render(128, logo.Amber))
	write(filepath.Join(dir, "logo-512.png"), logo.Render(512, logo.Amber))
}

func write(path string, img image.Image) {
	f, err := os.Create(path)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		panic(err)
	}
}
