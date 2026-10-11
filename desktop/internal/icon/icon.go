// Package icon draws the Circle Lens mark at any size: a magnifying glass with a circular arrow inside
// it (the gesture: draw a circle). It is drawn in code, so there are no image assets to keep, and every
// size is drawn for its own pixel grid rather than scaled from another.
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
	// Ink is the ring, handle and arrow.
	Ink color.NRGBA
	// Glass fills the inside of the ring (zero for clear).
	Glass color.NRGBA
	// GlassInk is the arrow's colour over a filled glass (zero to use Ink).
	GlassInk color.NRGBA
	// Paused dims the mark and strikes it through.
	Paused bool
	// GlassTop, if set, shades the glass from it at the top to Glass at the bottom.
	GlassTop color.NRGBA
}

// Draw renders the mark at size×size, anti-aliased with 8×8 supersampling.
func Draw(size int, style Style) *image.NRGBA {
	out := image.NewNRGBA(image.Rect(0, 0, size, size))
	s := float64(size)
	// Strokes never get thinner than about 1.4 px, so small icons stay crisp.
	stroke := func(fraction, least float64) float64 { return math.Max(least, fraction*s) }
	centreX, centreY := 0.41*s, 0.41*s
	ringRadius := 0.30 * s
	ringWidth := stroke(0.095, 1.4)
	handleWidth := stroke(0.135, 1.9)
	handleFromX, handleFromY := centreX+(ringRadius+ringWidth/2)*math.Cos(math.Pi/4), centreY+(ringRadius+ringWidth/2)*math.Sin(math.Pi/4)
	handleToX, handleToY := 0.87*s, 0.87*s
	// The arrow only reads from 20 px up; smaller icons show the glass alone.
	arrow := size >= 20 && !style.Paused
	arrowRadius, arrowWidth := 0.145*s, stroke(0.065, 1.3)
	const arrowFrom, arrowSweep = -0.35 * math.Pi, 1.45 * math.Pi // clockwise on screen
	arrowEnd := arrowFrom + arrowSweep
	tangentX, tangentY := -math.Sin(arrowEnd), math.Cos(arrowEnd)
	radialX, radialY := math.Cos(arrowEnd), math.Sin(arrowEnd)
	endX, endY := centreX+arrowRadius*radialX, centreY+arrowRadius*radialY
	headLength, headHalf := 0.13*s, 0.095*s
	tipX, tipY := endX+tangentX*headLength*0.75, endY+tangentY*headLength*0.75
	baseX, baseY := endX-tangentX*headLength*0.25, endY-tangentY*headLength*0.25
	head := [3][2]float64{{tipX, tipY}, {baseX + radialX*headHalf, baseY + radialY*headHalf}, {baseX - radialX*headHalf, baseY - radialY*headHalf}}

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
					distance := math.Hypot(px-centreX, py-centreY)
					inside := distance < ringRadius
					var c color.NRGBA
					var coverage float64
					switch {
					case math.Abs(distance-ringRadius) <= ringWidth/2,
						segmentDistance(px, py, handleFromX, handleFromY, handleToX, handleToY) <= handleWidth/2:
						c, coverage = style.Ink, inkAlpha
					case arrow && inside && (onArc(px, py, centreX, centreY, arrowRadius, arrowWidth, arrowFrom, arrowSweep) || inTriangle(px, py, head)):
						c, coverage = glassInk, inkAlpha
					case inside && style.Glass.A > 0:
						c, coverage = style.Glass, 1
						if style.GlassTop.A > 0 {
							c = mix(style.GlassTop, style.Glass, (py-(centreY-ringRadius))/(2*ringRadius))
						}
					}
					if style.Paused {
						switch slash(px, py, s) {
						case onSlash:
							c, coverage = style.Ink, 1
						case aroundSlash:
							coverage = 0 // a clear gap either side, so the slash reads over the glass
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

type slashPart int

const (
	offSlash slashPart = iota
	onSlash
	aroundSlash
)

// slash is the paused mark's stroke, top right to bottom left (across the handle, never along it),
// with a clear gap either side of it.
func slash(px, py, s float64) slashPart {
	distance := segmentDistance(px, py, 0.86*s, 0.1*s, 0.1*s, 0.86*s)
	width := math.Max(0.75, 0.05*s)
	switch {
	case distance <= width:
		return onSlash
	case distance <= width+math.Max(1, 0.06*s):
		return aroundSlash
	}
	return offSlash
}

// onArc is the stroke of a circle of radius r from angle `from` through `sweep` (radians, clockwise on
// screen), with a round cap where it starts.
func onArc(px, py, cx, cy, r, width, from, sweep float64) bool {
	distance := math.Hypot(px-cx, py-cy)
	if math.Abs(distance-r) > width/2 {
		startX, startY := cx+r*math.Cos(from), cy+r*math.Sin(from)
		return math.Hypot(px-startX, py-startY) <= width/2
	}
	angle := math.Atan2(py-cy, px-cx) - from
	for angle < 0 {
		angle += 2 * math.Pi
	}
	return angle <= sweep
}

func inTriangle(px, py float64, t [3][2]float64) bool {
	side := func(a, b [2]float64) float64 { return (b[0]-a[0])*(py-a[1]) - (b[1]-a[1])*(px-a[0]) }
	d1, d2, d3 := side(t[0], t[1]), side(t[1], t[2]), side(t[2], t[0])
	negative := d1 < 0 || d2 < 0 || d3 < 0
	positive := d1 > 0 || d2 > 0 || d3 > 0
	return !(negative && positive)
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
// store icon): the mark in white on a rounded tile in the brand's blue, lighter at the top left, with a
// small four-pointed sparkle in the top right corner. Being a solid tile, it reads at any size on light
// and dark backgrounds alike. Anti-aliased with 8×8 supersampling.
func DrawApp(size int) *image.NRGBA {
	out := image.NewNRGBA(image.Rect(0, 0, size, size))
	s := float64(size)
	// The tile fills the icon less a hair of margin, as Windows' own app icons do.
	margin := math.Max(0.5, 0.04*s)
	x0, y0, x1, y1 := margin, margin, s-margin, s-margin
	radius := 0.22 * (x1 - x0)
	top := color.NRGBA{R: 0x7a, G: 0xa6, B: 0xff, A: 0xff}    // light blue
	middle := color.NRGBA{R: 0x3b, G: 0x6c, B: 0xf0, A: 0xff} // the brand's blue
	bottom := color.NRGBA{R: 0x5b, G: 0x3f, B: 0xd8, A: 0xff} // towards violet
	white := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}

	// The mark, white, drawn on its own and placed a little low and left of centre, leaving the top
	// right corner for the sparkle.
	markSize := int(math.Round(0.74 * s))
	mark := Draw(markSize, Style{Ink: white, Glass: color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x38}, GlassInk: white})
	markX, markY := 0.11*s, 0.15*s
	sparkle := size >= 20

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
					// Diagonal gradient, light at the top left; a soft light along the top edge.
					t := ((px - x0) + (py - y0)) / ((x1 - x0) + (y1 - y0))
					c := mix(top, middle, t/0.5)
					if t > 0.5 {
						c = mix(middle, bottom, (t-0.5)/0.5)
					}
					if py-y0 < 0.06*s {
						c = mix(c, white, 0.12*(1-(py-y0)/(0.06*s)))
					}
					if sparkle {
						if star := sparkleAt(px, py, s); star > 0 {
							c = white
						}
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
	// The mark over the tile.
	left, topEdge := int(math.Round(markX)), int(math.Round(markY))
	for y := 0; y < markSize; y++ {
		for x := 0; x < markSize; x++ {
			c := mark.NRGBAAt(x, y)
			if c.A == 0 || left+x >= size || topEdge+y >= size {
				continue
			}
			under := out.NRGBAAt(left+x, topEdge+y)
			alpha := float64(c.A) / 255
			blend := func(over, base uint8) uint8 { return uint8(float64(over)*alpha + float64(base)*(1-alpha) + 0.5) }
			out.SetNRGBA(left+x, topEdge+y, color.NRGBA{R: blend(c.R, under.R), G: blend(c.G, under.G), B: blend(c.B, under.B), A: under.A})
		}
	}
	return out
}

// sparkleAt is 1 inside the sparkle: a four-pointed star in the top right corner of the icon.
func sparkleAt(px, py, s float64) int {
	cx, cy, r := 0.77*s, 0.22*s, math.Max(2.2, 0.13*s)
	dx, dy := math.Abs(px-cx), math.Abs(py-cy)
	if math.Sqrt(dx)+math.Sqrt(dy) <= math.Sqrt(r) { // an astroid: points along the axes
		return 1
	}
	return 0
}

// insideRoundedRect is whether a point is inside a rectangle with rounded corners.
func insideRoundedRect(px, py, x0, y0, x1, y1, r float64) bool {
	cx, cy := math.Max(x0+r, math.Min(px, x1-r)), math.Max(y0+r, math.Min(py, y1-r))
	return px >= x0 && px <= x1 && py >= y0 && py <= y1 && math.Hypot(px-cx, py-cy) <= r
}
