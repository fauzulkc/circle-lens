//go:build windows

package main

// The Circle Lens look, the same as the project's website: monochrome. Near-black with a soft neutral
// light (no coloured glow), white headlines over a grey second line, solid white primary buttons with
// dark text, hairline secondary buttons, 10 px corners on buttons and 6 px on chips and keys. No
// gradients on text, no glows, no pill buttons. The installer and the settings panel use it.

import (
	"math"
)

// brand is the website's palette.
var brand = struct {
	bg, surface, raised                              [3]byte
	text, body, muted, dim                           [3]byte
	primary, primaryHover, primaryPressed, onPrimary [3]byte
	switchOn, switchOff, knobOff, track, chipOn      [3]byte
	live, danger, white, black                       [3]byte
}{
	bg: rgb(0x0a0a0b), surface: rgb(0x141416), raised: rgb(0x1a1a1d),
	text: rgb(0xededef), body: rgb(0xd4d4d8), muted: rgb(0xa1a1aa), dim: rgb(0x71717a),
	primary: rgb(0xfafafa), primaryHover: rgb(0xffffff), primaryPressed: rgb(0xe4e4e7), onPrimary: rgb(0x0a0a0b),
	switchOn: rgb(0xe4e4e7), switchOff: rgb(0x3f3f46), knobOff: rgb(0xa1a1aa), track: rgb(0x27272a), chipOn: rgb(0x2a2a2e),
	live: rgb(0x86efac), danger: rgb(0xfca5a5), white: rgb(0xffffff), black: rgb(0x000000),
}

// The text styles, in DIPs.
var (
	styleTitle   = textStyle{face: faceDisplay, size: 34, weight: 700, tracking: -0.035}
	styleEyebrow = textStyle{face: faceMono, size: 10.5, weight: 600, tracking: 0.14}
	styleLede    = textStyle{face: faceText, size: 14.5, weight: fwNormal}
	styleButton  = textStyle{face: faceText, size: 14, weight: fwSemibold}
	styleSmall   = textStyle{face: faceText, size: 12, weight: fwNormal}
	styleBody    = textStyle{face: faceText, size: 13.5, weight: fwNormal}
	styleKey     = textStyle{face: faceMono, size: 11.5, weight: fwMedium}
)

// line and lineStrong are the website's hairlines: white at 7% and 12% over whatever is beneath.
const (
	lineSoft   = 0.07
	lineStrong = 0.12
)

// Corner radii in DIPs: buttons, and chips and keys.
const (
	radiusButton = 10
	radiusChip   = 6
)

// brandBackground paints a panel: near-black with a soft neutral light from the top left and a fainter
// one from the right (the website's mesh), and a hairline edge.
func (p *painter) brandBackground(x0, y0, x1, y1, radius float64) {
	p.fill(x0, y0, x1, y1, radius, brand.bg, 1)
	w, h := x1-x0, y1-y0
	p.glow(x0+w*0.22, y0-h*0.06, w*0.95, h*0.62, rgb(0x94a3b8), 0.11)
	p.glow(x0+w*1.02, y0+h*0.42, w*0.7, h*0.5, rgb(0x64748b), 0.08)
	p.ringOpacity(x0, y0, x1, y1, radius, math.Max(1, math.Round(p.scale)), brand.white, lineStrong)
}

// primaryButton is the website's main action: solid near-white with dark text, 10 px corners, and only
// the faintest shadow.
func (p *painter) primaryButton(x0, y0, x1, y1 float64, label string, hovered, pressed bool) {
	s := p.scale
	r := radiusButton * s
	fill := brand.primary
	switch {
	case pressed:
		fill = brand.primaryPressed
	case hovered:
		fill = brand.primaryHover
	}
	p.shadow(x0, y0, x1, y1, r, 0, 1*s, 0, 1.5*s, brand.black, 0.35)
	p.fill(x0, y0, x1, y1, r, fill, 1)
	if label != "" {
		p.write(label, (x0+x1)/2, y0, y1-y0, styleButton, brand.onPrimary, alignCentre)
	}
}

// ghostButton is the website's secondary action: transparent with a hairline border. A destructive one
// (tint brand.danger) keeps the same shape and puts the warning in its text colour.
func (p *painter) ghostButton(x0, y0, x1, y1 float64, label string, hovered, pressed bool, tint [3]byte) {
	s := p.scale
	r := radiusButton * s
	switch {
	case pressed:
		p.fill(x0, y0, x1, y1, r, brand.white, 0.08)
	case hovered:
		p.fill(x0, y0, x1, y1, r, brand.white, 0.05)
	}
	p.ringOpacity(x0, y0, x1, y1, r, math.Max(1, math.Round(s)), brand.white, map[bool]float64{true: 0.2, false: lineStrong}[hovered])
	colour := brand.text
	if tint == brand.danger {
		colour = brand.danger
	}
	p.write(label, (x0+x1)/2, y0, y1-y0, styleButton, colour, alignCentre)
}

// eyebrow is the website's small label: monospace capitals, widely spaced, after a small dot. It returns
// its width.
func (p *painter) eyebrow(label string, x, y, height float64, dot [3]byte, colour [3]byte, align alignment) float64 {
	s := p.scale
	textWidth := p.measure(label, styleEyebrow)
	dotSpace := 0.0
	if dot != ([3]byte{}) {
		dotSpace = 15 * s
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
		cx, cy := left+3*s, y+height/2
		p.circle(cx, cy, 5.5*s, dot, 0.12)
		p.circle(cx, cy, 3*s, dot, 1)
	}
	p.write(label, left+dotSpace, y, height, styleEyebrow, colour, alignLeft)
	return total
}

// brandSwitch is an on/off switch, small and flat: a light track with a dark knob on the right when on,
// a dark grey track with a grey knob on the left when off.
func (p *painter) brandSwitch(x, y, w, h float64, on, hovered bool) {
	s := p.scale
	r := h / 2
	track, knobColour := brand.switchOff, brand.knobOff
	if hovered {
		track = mixColour(brand.switchOff, brand.white, 0.08)
	}
	if on {
		track, knobColour = brand.switchOn, brand.onPrimary
		if hovered {
			track = brand.white
		}
	}
	p.fill(x, y, x+w, y+h, r, track, 1)
	knob := h - 4*s
	cx := x + h/2
	if on {
		cx = x + w - h/2
	}
	p.circle(cx, y+h/2, knob/2, knobColour, 1)
}

// keycap draws a key: monospace, 6 px corners, a hairline border. It returns its width.
func (p *painter) keycap(label string, x, y, height float64) float64 {
	s := p.scale
	width := p.measure(label, styleKey) + 13*s
	p.fill(x, y, x+width, y+height, radiusChip*s, brand.white, 0.04)
	p.ringOpacity(x, y, x+width, y+height, radiusChip*s, math.Max(1, math.Round(s)), brand.white, lineStrong)
	p.write(label, x+width/2, y-0.5*s, height, styleKey, brand.text, alignCentre)
	return width
}

// focusRing marks the control the keyboard is on with a soft white ring.
func (p *painter) focusRing(x0, y0, x1, y1, radius float64) {
	s := p.scale
	p.ringOpacity(x0-3*s, y0-3*s, x1+3*s, y1+3*s, radius+3*s, 1.5*s, brand.white, 0.55)
}

// lensArt is the mark at the top of the installer: the lens, a dark disc with a hairline edge and "Aa"
// in plain white inside.
func (p *painter) lensArt(cx, cy, r float64) {
	s := p.scale
	p.shadow(cx-r, cy-r, cx+r, cy+r, r, 0, 10*s, -6*s, 14*s, brand.black, 0.35)
	p.circle(cx, cy, r, brand.surface, 1)
	p.write("Aa", cx, cy-r, 2*r, textStyle{face: faceDisplay, size: r / s * 0.9, weight: 700, tracking: -0.04}, brand.text, alignCentre)
	p.ringOpacity(cx-r, cy-r, cx+r, cy+r, r, math.Max(1, math.Round(s)), brand.white, lineStrong)
}

// lensRing is the ring around the lens: a dark track, and progress from 0 to 1 starting at `spin`
// radians, drawn in `colour`.
func (p *painter) lensRing(cx, cy, r, progress, spin float64, colour [3]byte) {
	s := p.scale
	track := r + 9*s
	p.arc(cx, cy, track, 2.5*s, 0, 2*math.Pi-0.0001, func(float64) [3]byte { return brand.track }, 1)
	if progress > 0 {
		p.arc(cx, cy, track, 2.5*s, spin, progress*2*math.Pi, func(float64) [3]byte { return colour }, 1)
	}
}
