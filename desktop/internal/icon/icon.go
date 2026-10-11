// Package icon draws the Circle Lens mark at any size: a lens, a thin hollow ring with a curved glint of
// light inside its top left, like a reflection on glass. It is drawn in code, so there are no image assets
// to keep, and every size is drawn for its own pixel grid rather than scaled from another.
package icon

import (
	"image"
	"image/color"
	"math"
)

// Accent is the brand blue, for the browser extension's icons.
var Accent = color.NRGBA{R: 0x25, G: 0x63, B: 0xeb, A: 0xff}

// Style says how to draw the mark.
type Style struct {
	// Ink is the ring and the glint.
	Ink color.NRGBA
	// Glass fills the inside of the ring (zero for clear: hollow).
	Glass color.NRGBA
	// GlassInk is the glint's colour over a filled glass (zero to use Ink).
	GlassInk color.NRGBA
	// Paused dims the mark and strikes it through.
	Paused bool
	// GlassTop, if set, shades the glass from it at the top to Glass at the bottom.
	GlassTop color.NRGBA
}

// The glint's arc: around the top left of the glass (radians, clockwise on screen from the right).
const (
	glintFrom  = -0.86 * math.Pi
	glintSweep = 0.32 * math.Pi
)

// Draw renders the mark at size×size, anti-aliased with 8×8 supersampling: a thin ring, and inside it a
// short curved glint. Strokes stay lean but never thinner than about 1.3 px, so small icons stay crisp.
func Draw(size int, style Style) *image.NRGBA {
	out := image.NewNRGBA(image.Rect(0, 0, size, size))
	s := float64(size)
	cx, cy := 0.5*s, 0.5*s
	ringWidth := math.Max(1.3, 0.085*s)
	ringRadius := 0.5*s - ringWidth/2 - math.Max(0.5, 0.06*s)
	glintWidth := math.Max(1.1, 0.065*s)
	glintRadius := ringRadius - ringWidth/2 - glintWidth/2 - math.Max(0.9, 0.07*s)
	glint := size >= 16 && !style.Paused

	inkAlpha := 1.0
	if style.Paused {
		inkAlpha = 0.55
	}
	glassInk := style.GlassInk
	if glassInk.A == 0 {
		glassInk = style.Ink
	}
	const samples = 8
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			var r, g, b, a float64 // premultiplied accumulation
			for sy := 0; sy < samples; sy++ {
				for sx := 0; sx < samples; sx++ {
					px := float64(x) + (float64(sx)+0.5)/samples
					py := float64(y) + (float64(sy)+0.5)/samples
					distance := math.Hypot(px-cx, py-cy)
					var c color.NRGBA
					var coverage float64
					switch {
					case math.Abs(distance-ringRadius) <= ringWidth/2:
						c, coverage = style.Ink, inkAlpha
					case glint && onGlint(px, py, cx, cy, glintRadius, glintWidth):
						c, coverage = glassInk, inkAlpha
						if style.Glass.A == 0 {
							coverage *= 0.85
						}
					case distance < ringRadius && style.Glass.A > 0:
						c, coverage = style.Glass, 1
						if style.GlassTop.A > 0 {
							c = mix(style.GlassTop, style.Glass, (py-(cy-ringRadius))/(2*ringRadius))
						}
					}
					if style.Paused {
						switch slash(px, py, s) {
						case onSlash:
							c, coverage = style.Ink, 1
						case aroundSlash:
							coverage = 0 // a clear gap either side, so the slash reads over the ring
						}
					}
					if coverage == 0 || c.A == 0 {
						continue
					}
					alpha := coverage * float64(c.A) / 255
					r, g, b, a = r+float64(c.R)*alpha, g+float64(c.G)*alpha, b+float64(c.B)*alpha, a+alpha
				}
			}
			if a == 0 {
				continue
			}
			n := float64(samples * samples)
			out.SetNRGBA(x, y, color.NRGBA{R: uint8(r/a + 0.5), G: uint8(g/a + 0.5), B: uint8(b/a + 0.5), A: uint8(a/n*255 + 0.5)})
		}
	}
	return out
}

// onGlint is the glint: a short arc with round ends, around the top left inside the ring.
func onGlint(px, py, cx, cy, r, width float64) bool {
	angle := math.Atan2(py-cy, px-cx) - glintFrom
	for angle < 0 {
		angle += 2 * math.Pi
	}
	if angle <= glintSweep && math.Abs(math.Hypot(px-cx, py-cy)-r) <= width/2 {
		return true
	}
	for _, end := range []float64{glintFrom, glintFrom + glintSweep} {
		if math.Hypot(px-(cx+r*math.Cos(end)), py-(cy+r*math.Sin(end))) <= width/2 {
			return true
		}
	}
	return false
}

type slashPart int

const (
	offSlash slashPart = iota
	onSlash
	aroundSlash
)

// slash is the paused mark's stroke, top right to bottom left, with a clear gap either side of it.
func slash(px, py, s float64) slashPart {
	distance := segmentDistance(px, py, 0.86*s, 0.14*s, 0.14*s, 0.86*s)
	width := math.Max(0.75, 0.05*s)
	switch {
	case distance <= width:
		return onSlash
	case distance <= width+math.Max(1, 0.06*s):
		return aroundSlash
	}
	return offSlash
}

func segmentDistance(px, py, ax, ay, bx, by float64) float64 {
	dx, dy := bx-ax, by-ay
	t := math.Max(0, math.Min(1, ((px-ax)*dx+(py-ay)*dy)/(dx*dx+dy*dy)))
	return math.Hypot(px-(ax+t*dx), py-(ay+t*dy))
}

// mix blends from a to b by t (0 to 1).
func mix(a, b color.NRGBA, t float64) color.NRGBA {
	t = math.Max(0, math.Min(1, t))
	m := func(x, y uint8) uint8 { return uint8(float64(x) + (float64(y)-float64(x))*t + 0.5) }
	return color.NRGBA{R: m(a.R, b.R), G: m(a.G, b.G), B: m(a.B, b.B), A: m(a.A, b.A)}
}

// DrawApp renders the app's own icon (the exe, Start menu, Settings → Apps, the browser extension's
// store icon) in the website's monochrome look: the tray's mark, a thin hollow ring with its curved glint,
// in near-black on a white rounded tile with a fine dark edge (so it holds its shape on white too). The ring is large, reaching nearly to the
// tile's edge, and fine. Nothing in the corners: a dot there reads as a camera's flash. Anti-aliased with
// 8×8 supersampling.
func DrawApp(size int) *image.NRGBA {
	out := image.NewNRGBA(image.Rect(0, 0, size, size))
	s := float64(size)
	margin := math.Max(0.5, 0.04*s)
	x0, y0, x1, y1 := margin, margin, s-margin, s-margin
	radius := 0.22 * (x1 - x0)
	tileTop := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	tileBottom := color.NRGBA{R: 0xf1, G: 0xf1, B: 0xf3, A: 0xff}
	ink := color.NRGBA{R: 0x0a, G: 0x0a, B: 0x0b, A: 0xff}
	edge := math.Max(1, 0.012*s)

	// The ring: centred, its outside a small step in from the tile's edge, and fine.
	cx, cy := 0.5*s, 0.5*s
	ringWidth := math.Max(1.2, 0.035*s)
	ringRadius := 0.5*s - margin - math.Max(1.5, 0.075*s) - ringWidth/2
	glintWidth := math.Max(1.1, 0.03*s)
	glintRadius := ringRadius * 0.74
	glint := size >= 20

	const samples = 8
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			var r, g, b, a float64
			for sy := 0; sy < samples; sy++ {
				for sx := 0; sx < samples; sx++ {
					px := float64(x) + (float64(sx)+0.5)/samples
					py := float64(y) + (float64(sy)+0.5)/samples
					if !insideRoundedRect(px, py, x0, y0, x1, y1, radius) {
						continue
					}
					// White, a touch greyer at the bottom, with a fine dark edge.
					c := mix(tileTop, tileBottom, (py-y0)/(y1-y0))
					if !insideRoundedRect(px, py, x0+edge, y0+edge, x1-edge, y1-edge, math.Max(0, radius-edge)) {
						c = mix(c, ink, 0.14)
					}
					switch {
					case math.Abs(math.Hypot(px-cx, py-cy)-ringRadius) <= ringWidth/2:
						c = ink
					case glint && onGlint(px, py, cx, cy, glintRadius, glintWidth):
						c = mix(c, ink, 0.55)
					}
					r, g, b, a = r+float64(c.R), g+float64(c.G), b+float64(c.B), a+1
				}
			}
			if a == 0 {
				continue
			}
			n := float64(samples * samples)
			out.SetNRGBA(x, y, color.NRGBA{R: uint8(r/a + 0.5), G: uint8(g/a + 0.5), B: uint8(b/a + 0.5), A: uint8(a/n*255 + 0.5)})
		}
	}
	return out
}

// insideRoundedRect is whether a point is inside a rectangle with rounded corners.
func insideRoundedRect(px, py, x0, y0, x1, y1, r float64) bool {
	cx, cy := math.Max(x0+r, math.Min(px, x1-r)), math.Max(y0+r, math.Min(py, y1-r))
	return px >= x0 && px <= x1 && py >= y0 && py <= y1 && math.Hypot(px-cx, py-cy) <= r
}
