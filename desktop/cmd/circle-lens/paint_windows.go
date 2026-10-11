//go:build windows

package main

// The painter draws the app's own windows (the settings panel, the installer): shapes by hand into a
// 32-bit DIB, text by GDI, then an anti-aliased edge and a drop shadow so the result can go straight to
// UpdateLayeredWindow. Everything is in physical pixels; sizes given in DIPs are scaled by `scale`.

import (
	"image"
	"image/color"
	"math"
	"syscall"
	"unsafe"
)

var procSetTextCharacterExtra = gdi32.NewProc("SetTextCharacterExtra")

type painter struct {
	canvas *dib
	scale  float64
	fonts  map[fontKey]uintptr
}

type fontKey struct {
	face   typeface
	px     int
	weight int
	smooth bool // greyscale anti-aliasing (for text used as a mask), not ClearType
}

// typeface is one of the three roles text plays. Each uses the best-looking font the computer has for
// it: fonts that people who care about type often install (the website's own Inter, Bricolage Grotesque
// and JetBrains Mono, or Geist, IBM Plex), then Windows' own. Nothing is bundled.
type typeface int

const (
	faceText    typeface = iota // Inter, Geist; else Segoe UI Variable Text, Segoe UI
	faceDisplay                 // Bricolage Grotesque, Inter Display, Inter, Geist; else Segoe UI Variable Display
	faceMono                    // JetBrains Mono, Geist Mono, IBM Plex Mono; else Cascadia Mono, Consolas (labels, keys)
)

// faceFamilies are the families to look for, best first. Google Fonts installs Inter as optical sizes
// ("Inter 18pt", "Inter 28pt"), so those are looked for too.
var faceFamilies = map[typeface][]string{
	faceText:    {"Inter", "Inter Variable", "Inter 18pt", "Geist", "Segoe UI Variable Text", "Segoe UI"},
	faceDisplay: {"Bricolage Grotesque", "Inter Display", "Inter 28pt", "Inter 24pt", "Inter", "Inter Variable", "Inter 18pt", "Geist", "Segoe UI Variable Display", "Segoe UI"},
	faceMono:    {"JetBrains Mono", "Geist Mono", "IBM Plex Mono", "Cascadia Mono", "Consolas"},
}

// weightNames are how a family names its weights when each is a font of its own ("Inter Medium").
var weightNames = map[int][]string{
	300: {"Light"},
	500: {"Medium"},
	600: {"SemiBold", "Semibold", "DemiBold", "Medium"},
	800: {"ExtraBold", "Bold"},
}

// chosenFonts caches which installed face (and weight to ask for) serves each role and weight.
var chosenFonts = map[[2]int]struct {
	name   string
	weight int
}{}

// chooseFont finds the installed face for a role at a weight: a face named for the weight if the family
// has one ("Inter 18pt Medium"), else the family itself at that weight. The mono role falls back to the
// text face when no monospace font worth using is installed.
func chooseFont(face typeface, weight int) (string, int) {
	key := [2]int{int(face), weight}
	if chosen, ok := chosenFonts[key]; ok {
		return chosen.name, chosen.weight
	}
	name, gdiWeight := "", weight
	for _, family := range faceFamilies[face] {
		for _, suffix := range weightNames[weight] {
			if fontExists(family + " " + suffix) {
				name = family + " " + suffix
				break
			}
		}
		if name == "" && fontExists(family) {
			name = family
		}
		if name != "" {
			break
		}
	}
	if name == "" {
		name, gdiWeight = chooseFont(faceText, weight)
	}
	chosenFonts[key] = struct {
		name   string
		weight int
	}{name, gdiWeight}
	return name, gdiWeight
}

// textStyle is a face, size (DIPs), weight and tracking (extra space between letters, in ems).
type textStyle struct {
	face     typeface
	size     float64
	weight   int
	tracking float64
}

type alignment int

const (
	alignLeft alignment = iota
	alignCentre
	alignRight
)

func newPainter() painter { return painter{fonts: map[fontKey]uintptr{}} }

// releaseFonts frees the GDI fonts; they are made again when next needed.
func (p *painter) releaseFonts() {
	for key, font := range p.fonts {
		procDeleteObject.Call(font)
		delete(p.fonts, key)
	}
}

/* ---------- shapes ---------- */

// fill blends a rounded rectangle (radius 0 for square) of `colour` into the opaque panel.
func (p *painter) fill(x0, y0, x1, y1, radius float64, colour [3]byte, opacity float64) {
	p.fillShaded(x0, y0, x1, y1, radius, func(float64, float64) [3]byte { return colour }, opacity)
}

// fillShaded is fill with a colour that varies across the shape: shade gets the position within it,
// each from 0 to 1.
func (p *painter) fillShaded(x0, y0, x1, y1, radius float64, shade func(fx, fy float64) [3]byte, opacity float64) {
	canvas := p.canvas
	width, height := math.Max(1, x1-x0), math.Max(1, y1-y0)
	for y := max(0, int(y0)); y < min(canvas.Height, int(math.Ceil(y1))); y++ {
		for x := max(0, int(x0)); x < min(canvas.Width, int(math.Ceil(x1))); x++ {
			px, py := float64(x)+0.5, float64(y)+0.5
			coverage := roundedRectCoverage(px, py, x0, y0, x1, y1, radius) * opacity
			if coverage <= 0 {
				continue
			}
			colour := shade((px-x0)/width, (py-y0)/height)
			blendPixel(canvas, x, y, colour, coverage)
		}
	}
}

// ring strokes a rounded rectangle's outline `width` thick.
func (p *painter) ring(x0, y0, x1, y1, radius, width float64, colour [3]byte) {
	p.ringOpacity(x0, y0, x1, y1, radius, width, colour, 1)
}

func (p *painter) ringOpacity(x0, y0, x1, y1, radius, width float64, colour [3]byte, opacity float64) {
	canvas := p.canvas
	for y := max(0, int(y0)); y < min(canvas.Height, int(math.Ceil(y1))); y++ {
		for x := max(0, int(x0)); x < min(canvas.Width, int(math.Ceil(x1))); x++ {
			px, py := float64(x)+0.5, float64(y)+0.5
			coverage := roundedRectCoverage(px, py, x0, y0, x1, y1, radius) - roundedRectCoverage(px, py, x0+width, y0+width, x1-width, y1-width, math.Max(0, radius-width))
			if coverage <= 0 {
				continue
			}
			blendPixel(canvas, x, y, colour, coverage*opacity)
		}
	}
}

// shadow lays a soft shadow (or glow) of a rounded rectangle, moved by (dx, dy) and grown by spread,
// fading over sigma, like CSS box-shadow.
func (p *painter) shadow(x0, y0, x1, y1, radius, dx, dy, spread, sigma float64, colour [3]byte, opacity float64) {
	canvas := p.canvas
	reach := 3 * sigma
	sx0, sy0, sx1, sy1 := x0+dx-spread, y0+dy-spread, x1+dx+spread, y1+dy+spread
	r := math.Max(0, radius+spread)
	for y := max(0, int(sy0-reach)); y < min(canvas.Height, int(math.Ceil(sy1+reach))); y++ {
		for x := max(0, int(sx0-reach)); x < min(canvas.Width, int(math.Ceil(sx1+reach))); x++ {
			distance := math.Max(0, roundedRectDistance(float64(x)+0.5, float64(y)+0.5, sx0, sy0, sx1, sy1, r))
			amount := opacity * math.Exp(-distance*distance/(2*sigma*sigma))
			if amount > 0.002 {
				blendPixel(canvas, x, y, colour, amount)
			}
		}
	}
}

// glow adds a soft elliptical light centred at (cx, cy), like a CSS radial-gradient to transparent.
func (p *painter) glow(cx, cy, rx, ry float64, colour [3]byte, strength float64) {
	canvas := p.canvas
	for y := max(0, int(cy-ry)); y < min(canvas.Height, int(math.Ceil(cy+ry))); y++ {
		for x := max(0, int(cx-rx)); x < min(canvas.Width, int(math.Ceil(cx+rx))); x++ {
			dx, dy := (float64(x)+0.5-cx)/rx, (float64(y)+0.5-cy)/ry
			d := math.Sqrt(dx*dx + dy*dy)
			if d >= 1 {
				continue
			}
			falloff := 1 - smooth(d/0.95)
			blendPixel(canvas, x, y, colour, strength*falloff)
		}
	}
}

// arc strokes part of a circle at (cx, cy), radius r, `width` thick, from angle `from` (radians, 0 at
// the top, clockwise) through `sweep`, coloured along its length by shade(0 to 1), with round ends.
func (p *painter) arc(cx, cy, r, width, from, sweep float64, shade func(t float64) [3]byte, opacity float64) {
	if sweep <= 0 {
		return
	}
	canvas := p.canvas
	half := width / 2
	reach := r + half + 1
	endPoint := func(angle float64) (float64, float64) { return cx + r*math.Sin(angle), cy - r*math.Cos(angle) }
	ax, ay := endPoint(from)
	bx, by := endPoint(from + sweep)
	for y := max(0, int(cy-reach)); y < min(canvas.Height, int(math.Ceil(cy+reach))); y++ {
		for x := max(0, int(cx-reach)); x < min(canvas.Width, int(math.Ceil(cx+reach))); x++ {
			px, py := float64(x)+0.5, float64(y)+0.5
			angle := math.Atan2(px-cx, cy-py) // 0 at the top, clockwise
			along := math.Mod(angle-from+4*math.Pi, 2*math.Pi)
			var distance float64
			if along <= sweep {
				distance = math.Abs(math.Hypot(px-cx, py-cy)-r) - half
			} else {
				distance = math.Min(math.Hypot(px-ax, py-ay), math.Hypot(px-bx, py-by)) - half // the round ends
				along = sweep
				if math.Hypot(px-ax, py-ay) < math.Hypot(px-bx, py-by) {
					along = 0
				}
			}
			coverage := math.Max(0, math.Min(1, 0.5-distance)) * opacity
			if coverage > 0 {
				blendPixel(canvas, x, y, shade(along/sweep), coverage)
			}
		}
	}
}

// line strokes a straight line from (ax, ay) to (bx, by), `width` thick, with round ends.
func (p *painter) line(ax, ay, bx, by, width float64, colour [3]byte, opacity float64) {
	canvas := p.canvas
	half := width / 2
	dx, dy := bx-ax, by-ay
	length := dx*dx + dy*dy
	for y := max(0, int(math.Min(ay, by)-half-1)); y < min(canvas.Height, int(math.Ceil(math.Max(ay, by)+half+1))); y++ {
		for x := max(0, int(math.Min(ax, bx)-half-1)); x < min(canvas.Width, int(math.Ceil(math.Max(ax, bx)+half+1))); x++ {
			px, py := float64(x)+0.5, float64(y)+0.5
			t := 0.0
			if length > 0 {
				t = math.Max(0, math.Min(1, ((px-ax)*dx+(py-ay)*dy)/length))
			}
			distance := math.Hypot(px-(ax+t*dx), py-(ay+t*dy)) - half
			if coverage := math.Max(0, math.Min(1, 0.5-distance)) * opacity; coverage > 0 {
				blendPixel(canvas, x, y, colour, coverage)
			}
		}
	}
}

// circle fills a disc.
func (p *painter) circle(cx, cy, r float64, colour [3]byte, opacity float64) {
	p.fill(cx-r, cy-r, cx+r, cy+r, r, colour, opacity)
}

func blendPixel(canvas *dib, x, y int, colour [3]byte, amount float64) {
	if amount >= 1 {
		amount = 1
	}
	i := (y*canvas.Width + x) * 4
	for c := 0; c < 3; c++ {
		canvas.Pixels[i+c] = byte(float64(canvas.Pixels[i+c])*(1-amount) + float64(colour[2-c])*amount + 0.5)
	}
}

// drawIcon blends a straight-alpha picture onto the panel at (x, y).
func (p *painter) drawIcon(picture *image.NRGBA, x, y float64) {
	canvas := p.canvas
	left, top := int(math.Round(x)), int(math.Round(y))
	bounds := picture.Bounds()
	for py := 0; py < bounds.Dy(); py++ {
		for px := 0; px < bounds.Dx(); px++ {
			c := picture.NRGBAAt(px, py)
			cx, cy := left+px, top+py
			if c.A == 0 || cx < 0 || cy < 0 || cx >= canvas.Width || cy >= canvas.Height {
				continue
			}
			blendPixel(canvas, cx, cy, [3]byte{c.R, c.G, c.B}, float64(c.A)/255)
		}
	}
}

// finish works out the panel's anti-aliased edge and the drop shadow around it, and turns the canvas
// into premultiplied alpha for UpdateLayeredWindow. Text drawn by GDI is inside the panel, where the
// alpha is set to opaque here (GDI leaves it zero).
func (p *painter) finish(x0, y0, x1, y1, radius float64) {
	p.finishWith(x0, y0, x1, y1, radius, 6*p.scale, 14*p.scale, 0.22)
}

func (p *painter) finishWith(x0, y0, x1, y1, radius, offset, sigma, depth float64) {
	canvas := p.canvas
	for y := 0; y < canvas.Height; y++ {
		for x := 0; x < canvas.Width; x++ {
			px, py := float64(x)+0.5, float64(y)+0.5
			coverage := roundedRectCoverage(px, py, x0, y0, x1, y1, radius)
			// A soft shadow below the panel, falling off with distance from it.
			distance := math.Max(0, roundedRectDistance(px, py-offset, x0, y0, x1, y1, radius))
			shade := depth * math.Exp(-distance*distance/(2*sigma*sigma))
			i := (y*canvas.Width + x) * 4
			alpha := coverage + shade*(1-coverage)
			for c := 0; c < 3; c++ {
				canvas.Pixels[i+c] = byte(float64(canvas.Pixels[i+c])*coverage + 0.5)
			}
			canvas.Pixels[i+3] = byte(alpha*255 + 0.5)
		}
	}
}

// panelLayer keeps what doesn't change while a panel is shown: its background, and its anti-aliased
// edge and shadow. begin starts a frame from the background; end applies the edge and shadow and turns
// the frame into premultiplied alpha for UpdateLayeredWindow.
type panelLayer struct {
	width, height int
	background    []byte
	coverage      []float32
	alpha         []byte
}

func (l *panelLayer) begin(p *painter, x0, y0, x1, y1, radius, offset, sigma, depth float64) {
	canvas := p.canvas
	if l.background == nil || l.width != canvas.Width || l.height != canvas.Height {
		l.width, l.height = canvas.Width, canvas.Height
		clear(canvas.Pixels)
		p.brandBackground(x0, y0, x1, y1, radius)
		l.background = append(l.background[:0], canvas.Pixels...)
		l.coverage = make([]float32, canvas.Width*canvas.Height)
		l.alpha = make([]byte, canvas.Width*canvas.Height)
		for y := 0; y < canvas.Height; y++ {
			for x := 0; x < canvas.Width; x++ {
				px, py := float64(x)+0.5, float64(y)+0.5
				i := y*canvas.Width + x
				inside := roundedRectDistance(px, py, x0, y0, x1, y1, radius)
				if inside < -1 { // well inside: opaque, no edge or shadow to work out
					l.coverage[i], l.alpha[i] = 1, 255
					continue
				}
				coverage := math.Max(0, math.Min(1, 0.5-inside))
				distance := math.Max(0, roundedRectDistance(px, py-offset, x0, y0, x1, y1, radius))
				shade := 0.0
				if distance < 4*sigma {
					shade = depth * math.Exp(-distance*distance/(2*sigma*sigma))
				}
				l.coverage[i] = float32(coverage)
				l.alpha[i] = byte((coverage+shade*(1-coverage))*255 + 0.5)
			}
		}
	}
	copy(canvas.Pixels, l.background)
}

func (l *panelLayer) end(p *painter) {
	pixels := p.canvas.Pixels
	for i, coverage := range l.coverage {
		j := i * 4
		if coverage < 1 {
			pixels[j] = byte(float32(pixels[j])*coverage + 0.5)
			pixels[j+1] = byte(float32(pixels[j+1])*coverage + 0.5)
			pixels[j+2] = byte(float32(pixels[j+2])*coverage + 0.5)
		}
		pixels[j+3] = l.alpha[i]
	}
}

// opaque sets the alpha of a region inside the panel back to opaque (GDI text leaves it zero), for
// drawing onto a finished frame.
func (p *painter) opaque(x0, y0, x1, y1 float64) {
	canvas := p.canvas
	for y := max(0, int(y0)); y < min(canvas.Height, int(math.Ceil(y1))); y++ {
		for x := max(0, int(x0)); x < min(canvas.Width, int(math.Ceil(x1))); x++ {
			canvas.Pixels[(y*canvas.Width+x)*4+3] = 255
		}
	}
}

func roundedRectDistance(px, py, x0, y0, x1, y1, radius float64) float64 {
	cx, cy := (x0+x1)/2, (y0+y1)/2
	hx, hy := (x1-x0)/2-radius, (y1-y0)/2-radius
	qx, qy := math.Abs(px-cx)-hx, math.Abs(py-cy)-hy
	return math.Hypot(math.Max(qx, 0), math.Max(qy, 0)) + math.Min(math.Max(qx, qy), 0) - radius
}

func roundedRectCoverage(px, py, x0, y0, x1, y1, radius float64) float64 {
	return math.Max(0, math.Min(1, 0.5-roundedRectDistance(px, py, x0, y0, x1, y1, radius)))
}

func smooth(t float64) float64 {
	t = math.Max(0, math.Min(1, t))
	return t * t * (3 - 2*t)
}

/* ---------- text ---------- */

// text draws one line in the UI face (ClearType, over the opaque panel), vertically centred in a box
// `height` tall whose top is y, and anchored at x as `align` says.
func (p *painter) text(line string, x, y, height, sizeDip float64, weight int, colour [3]byte, align alignment) {
	p.write(line, x, y, height, textStyle{face: faceText, size: sizeDip, weight: weight}, colour, align)
}

// textWidth measures a line in the UI face, semibold.
func (p *painter) textWidth(line string, sizeDip float64) float64 {
	return p.measure(line, textStyle{face: faceText, size: sizeDip, weight: fwSemibold})
}

// write draws one line in `style`; it returns the line's width.
func (p *painter) write(line string, x, y, height float64, style textStyle, colour [3]byte, align alignment) float64 {
	dc := p.canvas.dc
	previous, _, _ := procSelectObject.Call(dc, p.font(style, false))
	defer procSelectObject.Call(dc, previous)
	defer procSetTextCharacterExtra.Call(dc, 0)
	procSetTextCharacterExtra.Call(dc, uintptr(int32(math.Round(style.tracking*style.size*p.scale))))
	pointer := wide(line)
	length := uintptr(len([]rune(line)))
	var extent size
	procGetTextExtentPoint32.Call(dc, uintptr(unsafe.Pointer(pointer)), length, uintptr(unsafe.Pointer(&extent)))
	switch align {
	case alignCentre:
		x -= float64(extent.CX) / 2
	case alignRight:
		x -= float64(extent.CX)
	}
	top := y + (height-float64(extent.CY))/2
	procSetBkMode.Call(dc, transparentBkMode)
	procSetTextColor.Call(dc, uintptr(colour[0])|uintptr(colour[1])<<8|uintptr(colour[2])<<16)
	procTextOut.Call(dc, uintptr(int32(math.Round(x))), uintptr(int32(math.Round(top))), uintptr(unsafe.Pointer(pointer)), length)
	procGdiFlush.Call()
	return float64(extent.CX)
}

// measure is the width of a line in `style`.
func (p *painter) measure(line string, style textStyle) float64 {
	dc := p.canvas.dc
	previous, _, _ := procSelectObject.Call(dc, p.font(style, false))
	defer procSelectObject.Call(dc, previous)
	defer procSetTextCharacterExtra.Call(dc, 0)
	procSetTextCharacterExtra.Call(dc, uintptr(int32(math.Round(style.tracking*style.size*p.scale))))
	var extent size
	procGetTextExtentPoint32.Call(dc, uintptr(unsafe.Pointer(wide(line))), uintptr(len([]rune(line))), uintptr(unsafe.Pointer(&extent)))
	return float64(extent.CX)
}

// writeGradient draws a line filled with a gradient running across it, left to right; it returns the
// line's width. The text is drawn white on black into a scratch bitmap and used as a mask.
func (p *painter) writeGradient(line string, x, y, height float64, style textStyle, shade func(t float64) [3]byte, align alignment) float64 {
	width := p.measure(line, style)
	switch align {
	case alignCentre:
		x -= width / 2
	case alignRight:
		x -= width
	}
	pad := math.Ceil(style.size * p.scale * 0.3) // room for overhangs (italics, accents)
	maskWidth, maskHeight := int(math.Ceil(width+2*pad)), int(math.Ceil(height))
	mask, ok := newDIB(maskWidth, maskHeight)
	if !ok {
		return width
	}
	defer mask.Close()
	previous, _, _ := procSelectObject.Call(mask.dc, p.font(style, true))
	procSetTextCharacterExtra.Call(mask.dc, uintptr(int32(math.Round(style.tracking*style.size*p.scale))))
	pointer := wide(line)
	length := uintptr(len([]rune(line)))
	var extent size
	procGetTextExtentPoint32.Call(mask.dc, uintptr(unsafe.Pointer(pointer)), length, uintptr(unsafe.Pointer(&extent)))
	procSetBkMode.Call(mask.dc, transparentBkMode)
	procSetTextColor.Call(mask.dc, 0xFFFFFF)
	procTextOut.Call(mask.dc, uintptr(int32(pad)), uintptr(int32(math.Round((height-float64(extent.CY))/2))), uintptr(unsafe.Pointer(pointer)), length)
	procGdiFlush.Call()
	procSelectObject.Call(mask.dc, previous)
	left, top := int(math.Round(x-pad)), int(math.Round(y))
	for my := 0; my < maskHeight; my++ {
		for mx := 0; mx < maskWidth; mx++ {
			coverage := float64(mask.Pixels[(my*maskWidth+mx)*4+1]) / 255
			cx, cy := left+mx, top+my
			if coverage <= 0 || cx < 0 || cy < 0 || cx >= p.canvas.Width || cy >= p.canvas.Height {
				continue
			}
			blendPixel(p.canvas, cx, cy, shade(math.Max(0, math.Min(1, (float64(mx)-pad)/width))), coverage)
		}
	}
	return width
}

func (p *painter) font(style textStyle, smooth bool) uintptr {
	key := fontKey{face: style.face, px: int(math.Round(style.size * p.scale)), weight: style.weight, smooth: smooth}
	if font, ok := p.fonts[key]; ok {
		return font
	}
	quality := uintptr(clearTypeQuality)
	if smooth {
		quality = antialiasedQuality
	}
	name, weight := chooseFont(style.face, style.weight)
	font, _, _ := procCreateFont.Call(uintptr(-int32(key.px)), 0, 0, 0, uintptr(weight), 0, 0, 0, defaultCharset, 0, 0, quality, 0,
		uintptr(unsafe.Pointer(wide(name))))
	p.fonts[key] = font
	return font
}

// fontExists asks GDI for the face and checks it did not substitute another.
func fontExists(face string) bool {
	height := int32(-14)
	font, _, _ := procCreateFont.Call(uintptr(height), 0, 0, 0, fwNormal, 0, 0, 0, defaultCharset, 0, 0, 0, 0, uintptr(unsafe.Pointer(wide(face))))
	if font == 0 {
		return false
	}
	defer procDeleteObject.Call(font)
	probe, ok := newDIB(1, 1)
	if !ok {
		return false
	}
	defer probe.Close()
	previous, _, _ := procSelectObject.Call(probe.dc, font)
	defer procSelectObject.Call(probe.dc, previous)
	buffer := make([]uint16, 64)
	procGetTextFace.Call(probe.dc, uintptr(len(buffer)), uintptr(unsafe.Pointer(&buffer[0])))
	return syscall.UTF16ToString(buffer) == face
}

/* ---------- colour ---------- */

func colourNRGBA(c [3]byte, alpha byte) color.NRGBA {
	return color.NRGBA{R: c[0], G: c[1], B: c[2], A: alpha}
}

func mixColour(from, to [3]byte, amount float64) [3]byte {
	var out [3]byte
	for i := range out {
		out[i] = byte(float64(from[i]) + (float64(to[i])-float64(from[i]))*amount + 0.5)
	}
	return out
}

func rgb(value uint32) [3]byte { return [3]byte{byte(value >> 16), byte(value >> 8), byte(value)} }

// gradient returns colours evenly spread along stops, for t from 0 to 1.
func gradient(stops ...[3]byte) func(t float64) [3]byte {
	return func(t float64) [3]byte {
		t = math.Max(0, math.Min(1, t)) * float64(len(stops)-1)
		i := min(int(t), len(stops)-2)
		return mixColour(stops[i], stops[i+1], t-float64(i))
	}
}
