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
