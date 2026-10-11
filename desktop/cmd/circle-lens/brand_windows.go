//go:build windows

package main

// The Circle Lens look, the same as the project's website: near-black with soft blue, violet and cyan
// light, a cyan → blue → violet gradient on the words that matter, pill buttons in a blue gradient,
// uppercase monospace labels, and keys drawn as keycaps. The installer and the settings panel use it.

import (
	"math"
)

// brand is the website's palette.
var brand = struct {
	bg, raised, surface, surfaceRaised [3]byte
	text, body, muted, dim             [3]byte
	blue, blueDeep, cyan, violet       [3]byte
	buttonTop, buttonBottom            [3]byte
	green, red, white                  [3]byte
}{
	bg: rgb(0x07080c), raised: rgb(0x0c0e14), surface: rgb(0x11141c), surfaceRaised: rgb(0x171b25),
	text: rgb(0xeef0f6), body: rgb(0xc9cdd8), muted: rgb(0xa3a9b8), dim: rgb(0x6d7487),
	blue: rgb(0x5b8cff), blueDeep: rgb(0x2563eb), cyan: rgb(0x8bd3ff), violet: rgb(0xa78bfa),
	buttonTop: rgb(0x6f9bff), buttonBottom: rgb(0x3b6cf0),
	green: rgb(0x5ee0a0), red: rgb(0xff8f8f), white: rgb(0xffffff),
}

// The text styles, in DIPs.
var (
	styleTitle   = textStyle{face: faceDisplay, size: 34, weight: 700, tracking: -0.03}
	styleEyebrow = textStyle{face: faceMono, size: 10.5, weight: 600, tracking: 0.14}
	styleLede    = textStyle{face: faceText, size: 14.5, weight: fwNormal}
	styleButton  = textStyle{face: faceText, size: 14.5, weight: fwSemibold}
	styleSmall   = textStyle{face: faceText, size: 12, weight: fwNormal}
	styleBody    = textStyle{face: faceText, size: 13.5, weight: fwNormal}
	styleKey     = textStyle{face: faceMono, size: 11.5, weight: fwMedium}
)

// brandGradient is the website's accent: cyan, blue, violet.
var brandGradient = gradient(brand.cyan, brand.blue, brand.violet)

// line and lineStrong are the website's hairlines: white at 7% and 13% over whatever is beneath.
const (
	lineSoft   = 0.07
	lineStrong = 0.13
)

// brandBackground paints a panel: near-black, lit from above in blue, with violet and cyan to the sides,
// and a hairline edge.
func (p *painter) brandBackground(x0, y0, x1, y1, radius float64) {
	p.fill(x0, y0, x1, y1, radius, brand.bg, 1)
	w, h := x1-x0, y1-y0
	p.glow(x0+w*0.5, y0-h*0.08, w*0.95, h*0.62, brand.blueDeep, 0.16)
	p.glow(x0+w*0.5, y0+h*0.2, w*0.62, h*0.36, brand.blue, 0.07)
	p.glow(x0+w*0.86, y0+h*0.62, w*0.55, h*0.34, brand.violet, 0.04)
	p.glow(x0+w*0.12, y0+h*0.72, w*0.55, h*0.34, brand.cyan, 0.03)
	p.ringOpacity(x0, y0, x1, y1, radius, math.Max(1, math.Round(p.scale)), brand.white, lineStrong)
}

// primaryButton is the website's main action: a blue gradient pill with a glow beneath, a light top
// edge and a hairline ring, glowing more under the pointer.
func (p *painter) primaryButton(x0, y0, x1, y1 float64, label string, hovered, pressed bool) {
	s := p.scale
	r := (y1 - y0) / 2
	glow := 0.22
	if hovered {
		glow = 0.38
	}
	p.shadow(x0, y0, x1, y1, r, 0, 6*s, -8*s, 9*s, brand.buttonBottom, glow)
	top, bottom := brand.buttonTop, brand.buttonBottom
	if pressed {
		top, bottom = mixColour(top, bottom, 0.4), mixColour(bottom, brand.bg, 0.15)
	}
	p.fillShaded(x0, y0, x1, y1, r, func(_, fy float64) [3]byte { return mixColour(top, bottom, fy) }, 1)
	ring, ringOpacity := brand.blue, 0.5
	if hovered {
		ring, ringOpacity = brand.cyan, 0.7
	}
	p.ringOpacity(x0-s, y0-s, x1+s, y1+s, r+s, s, ring, ringOpacity)
	// The light along the top edge (an inset highlight), fading out before the middle.
	p.topHighlight(x0, y0, x1, y1, r, 1.4*s, 0.3)
	p.write(label, (x0+x1)/2, y0, y1-y0, styleButton, brand.white, alignCentre)
}

// topHighlight lights the inside of a shape's upper edge, `width` deep, fading out towards the middle.
func (p *painter) topHighlight(x0, y0, x1, y1, radius, width, opacity float64) {
	canvas := p.canvas
	middle := (y0 + y1) / 2
	for y := max(0, int(y0)); y < min(canvas.Height, int(math.Ceil(middle))); y++ {
		for x := max(0, int(x0)); x < min(canvas.Width, int(math.Ceil(x1))); x++ {
			px, py := float64(x)+0.5, float64(y)+0.5
			depth := -roundedRectDistance(px, py, x0, y0, x1, y1, radius)
			if depth <= 0 || depth >= width+1 {
				continue
			}
			edge := math.Min(1, depth) * math.Max(0, math.Min(1, width+1-depth))
			fade := 1 - (py-y0)/(middle-y0)
			blendPixel(canvas, x, y, brand.white, opacity*edge*fade*fade)
		}
	}
}

// ghostButton is the website's secondary action: a faint pill with a hairline ring.
func (p *painter) ghostButton(x0, y0, x1, y1 float64, label string, hovered, pressed bool, tint [3]byte) {
	s := p.scale
	r := (y1 - y0) / 2
	fill, ring, ringOpacity := 0.04, brand.white, lineStrong
	if hovered {
		fill, ring, ringOpacity = 0.08, brand.cyan, 0.45
	}
	if pressed {
		fill = 0.12
	}
	if tint != brand.white {
		ring, ringOpacity = tint, 0.45
		if hovered {
			ringOpacity = 0.75
		}
		p.fill(x0, y0, x1, y1, r, tint, fill*1.6)
	} else {
		p.fill(x0, y0, x1, y1, r, brand.white, fill)
	}
	p.ringOpacity(x0, y0, x1, y1, r, s, ring, ringOpacity)
	colour := brand.text
	if tint != brand.white {
		colour = mixColour(tint, brand.white, 0.35)
	}
	p.write(label, (x0+x1)/2, y0, y1-y0, styleButton, colour, alignCentre)
}

// eyebrow is the website's small label: monospace capitals, widely spaced, in cyan, after a dot.
// It returns its width.
func (p *painter) eyebrow(label string, x, y, height float64, dot [3]byte, colour [3]byte, align alignment) float64 {
	s := p.scale
	textWidth := p.measure(label, styleEyebrow)
	dotSpace := 0.0
	if dot != ([3]byte{}) {
		dotSpace = 17 * s
	}
	total := dotSpace + textWidth
	left := x
	switch align {
	case alignCentre:
		left = x - total/2
	case alignRight:
		left = x - total
	}
	if dotSpace > 0 {
		cx, cy := left+3.5*s, y+height/2
		p.circle(cx, cy, 7.5*s, dot, 0.15)
		p.circle(cx, cy, 3.5*s, dot, 1)
	}
	p.write(label, left+dotSpace, y, height, styleEyebrow, colour, alignLeft)
	return total
}

// brandSwitch is an on/off switch, kept light: flat blue with a white knob on the right when on, a faint
// track with a grey knob on the left when off. No glow, no rim.
func (p *painter) brandSwitch(x, y, w, h float64, on, hovered bool) {
	s := p.scale
	r := h / 2
	if on {
		top, bottom := mixColour(brand.buttonTop, brand.buttonBottom, 0.3), brand.buttonBottom
		if hovered {
			top, bottom = brand.buttonTop, mixColour(brand.buttonTop, brand.buttonBottom, 0.6)
		}
		p.fillShaded(x, y, x+w, y+h, r, func(_, fy float64) [3]byte { return mixColour(top, bottom, fy) }, 1)
	} else {
		p.fill(x, y, x+w, y+h, r, brand.white, map[bool]float64{true: 0.18, false: 0.12}[hovered])
	}
	knob := h - 4*s
	cx, colour := x+h/2, mixColour(brand.muted, brand.white, 0.3)
	if on {
		cx, colour = x+w-h/2, brand.white
	}
	p.circle(cx, y+h/2, knob/2, colour, 1)
}

// keycap draws a keycap: monospace, a hairline border thicker at the bottom. It returns its width.
func (p *painter) keycap(label string, x, y, height float64) float64 {
	s := p.scale
	width := p.measure(label, styleKey) + 13*s
	p.fill(x, y, x+width, y+height, 6*s, brand.white, 0.06)
	p.ringOpacity(x, y, x+width, y+height, 6*s, s, brand.white, 0.16)
	p.fill(x+3*s, y+height-1.6*s, x+width-3*s, y+height, 0, brand.white, 0.1)
	p.write(label, x+width/2, y-s, height, styleKey, brand.text, alignCentre)
	return width
}

// focusRing marks the control the keyboard is on, in cyan, as the website does.
func (p *painter) focusRing(x0, y0, x1, y1, radius float64) {
	s := p.scale
	p.ringOpacity(x0-3*s, y0-3*s, x1+3*s, y1+3*s, radius+3*s, 2*s, brand.cyan, 0.9)
}

// lensArt is the mark at the top of the installer: the lens itself, a glass disc with "Aa" magnified
// inside and a faint halo.
func (p *painter) lensArt(cx, cy, r float64) {
	s := p.scale
	p.glow(cx, cy, r*1.9, r*1.9, brand.blue, 0.07)
	p.shadow(cx-r, cy-r, cx+r, cy+r, r, 0, 12*s, -6*s, 16*s, rgb(0x000000), 0.45)
	p.circle(cx, cy, r, brand.raised, 1)
	p.glow(cx, cy-r*0.55, r*1.1, r*0.8, brand.blue, 0.08)
	p.writeGradient("Aa", cx, cy-r, 2*r, textStyle{face: faceDisplay, size: r / s * 0.95, weight: 700, tracking: -0.04}, brandGradient, alignCentre)
	p.ringOpacity(cx-r, cy-r, cx+r, cy+r, r, 2*s, brand.text, 0.22)
	p.ringOpacity(cx-r+2*s, cy-r+2*s, cx+r-2*s, cy+r-2*s, r-2*s, s, brand.white, 0.08)
}

// lensRing is the ring around the lens: progress from 0 to 1, starting at `spin` radians, coloured
// along its length.
func (p *painter) lensRing(cx, cy, r, progress, spin float64, colour func(t float64) [3]byte) {
	s := p.scale
	track := r + 9*s
	p.arc(cx, cy, track, 3*s, 0, 2*math.Pi-0.0001, func(float64) [3]byte { return brand.white }, 0.06)
	if progress > 0 {
		p.arc(cx, cy, track, 3*s, spin, progress*2*math.Pi, colour, 1)
	}
}
