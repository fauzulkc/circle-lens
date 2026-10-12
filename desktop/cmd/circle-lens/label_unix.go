//go:build linux || darwin

package main

import (
	"math"

	"circlelens/internal/render"
)

// drawLabel renders the zoom pill from lens.css, as drawLabel in overlay_windows.go does: surface
// background, 1px line border, muted 11px text, fully rounded. The system draws the text; the shape and
// its anti-aliasing are drawn here.
func drawLabel(sys system, text string, scale float64, dark bool, theme render.Theme) *render.Image {
	line, muted := [3]byte{0xde, 0xdf, 0xe1}, [3]byte{0x4b, 0x55, 0x63}
	if dark {
		line, muted = [3]byte{0x3d, 0x3f, 0x46}, [3]byte{0xa1, 0xa1, 0xaa}
	}
	coverage, textWidth, textHeight := sys.text(text, 11*scale)
	if textWidth == 0 {
		return nil
	}
	height := int(math.Round(22 * scale))
	width := textWidth + int(math.Round(20*scale))
	surface := theme.Surface
	textLeft, textTop := (width-textWidth)/2, (height-textHeight)/2
	out := render.NewImage(width, height)
	half := float64(height) / 2
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			px, py := float64(x)+0.5, float64(y)+0.5
			along := math.Max(half, math.Min(float64(width)-half, px))
			edge := math.Hypot(px-along, py-half) - half
			cover := math.Min(1, math.Max(0, 0.5-edge))
			if cover == 0 {
				continue
			}
			ink := 0.0
			if tx, ty := x-textLeft, y-textTop; tx >= 0 && ty >= 0 && tx < textWidth && ty < textHeight {
				ink = float64(coverage[ty*textWidth+tx]) / 255
			}
			onLine := math.Min(1, math.Max(0, edge+scale+0.5))
			i := (y*width + x) * 4
			for c := 0; c < 3; c++ {
				// surface is R, G, B; the image is B, G, R.
				value := float64(surface[2-c])*(1-ink) + float64(muted[2-c])*ink
				value = value*(1-onLine) + float64(line[2-c])*onLine
				out.Pixels[i+c] = byte(value*cover + 0.5)
			}
			out.Pixels[i+3] = byte(cover*255 + 0.5)
		}
	}
	return out
}
