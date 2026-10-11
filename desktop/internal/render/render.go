// Package render draws the lens in software, for every platform backend. A backend hands it a capture
// of the screen around the pointer and a lens.Frame, and gets back a premultiplied BGRA canvas to put
// on screen in a per-pixel-alpha window. Drawing it here (rather than leaning on an OS magnifier) is what
// lets the desktop lens move exactly like the CSS original: the glass scales, fades and blurs as one
// element, its box shadows included, while the content zooms inside.
//
// Geometry mirrors shared/lens.css. The canvas is square: the glass (radius R, scaled by the frame's
// GlassScale about its centre) sits in the middle with Pad pixels around it for the drop shadow and the
// zoom label. The pointer is always at the canvas centre, and canvas offset d shows the screen point
// pointer + d / (zoom × glassScale).
package render

import (
	"math"
	"runtime"
	"sync"

	"circlelens/internal/lens"
)

// Image is 4 bytes per pixel, B G R A, rows packed. Captures are opaque; the canvas and label sprite
// are premultiplied.
type Image struct {
	Pixels        []byte
	Width, Height int
}

// NewImage allocates a cleared image.
func NewImage(width, height int) *Image {
	return &Image{Pixels: make([]byte, width*height*4), Width: width, Height: height}
}

// Capture is the screen around the pointer: Left/Top is its screen position, Pointer where the pointer
// was when it was taken, Scale the DPI scale (1 = 96 DPI) of that monitor.
type Capture struct {
	Image
	Left, Top          int
	PointerX, PointerY float64
	Scale              float64

	blurred      *Image
	blurredScale int
}

// Theme is the glass's palette, as RGB.
type Theme struct {
	// Border is the 2px ring: ink at 22% over the backdrop, so opaque.
	Border [3]byte
	// Surface is the label pill and the glass's inner highlight.
	Surface [3]byte
	// Backdrop shows where the glass reaches past the edge of the screen.
	Backdrop [3]byte
}

var (
	Light = Theme{Border: [3]byte{0xcb, 0xcc, 0xd0}, Surface: [3]byte{0xff, 0xff, 0xff}, Backdrop: [3]byte{0xff, 0xff, 0xff}}
	Dark  = Theme{Border: [3]byte{0x3d, 0x40, 0x44}, Surface: [3]byte{0x1a, 0x1d, 0x24}, Backdrop: [3]byte{0x0b, 0x0d, 0x12}}
)

// Pad is the room around the glass for the shadow and the label, in pixels.
func Pad(scale float64) int { return int(math.Ceil(72 * scale)) }

// Side is the canvas's width and height for a glass of `radius` pixels.
func Side(radius, scale float64) int { return 2*int(math.Round(radius)) + 2*Pad(scale) }

// SourceRadius is how far from the pointer, in screen pixels, a frame at `zoom` samples the screen:
// what a capture must cover (plus room for the blur).
func SourceRadius(radius, zoom, scale float64) float64 {
	return radius/math.Max(1, zoom) + 12*scale
}

// Scene is one frame to draw.
type Scene struct {
	Frame  lens.Frame
	Radius float64 // the full glass radius, in pixels
	Theme  Theme
	// Label is the zoom label sprite (premultiplied), drawn centred under the glass; nil for none.
	Label *Image
}

// Renderer draws frames, keeping what does not change between them. Everything the glass draws is
// linear in the screen content under it, so each pixel is `layer + content × weight`: the layer (the
// border, highlight, shadows and label, premultiplied) and the weights depend only on the glass's size,
// opacity and blur, and are rebuilt only when those change. Once the lens has opened, a frame is a copy
// of the layer plus one bilinear sample per pixel inside the glass.
type Renderer struct {
	key    layerKey
	layer  []byte   // premultiplied BGRA, canvas-sized
	weight []uint16 // how much of the content each pixel shows, premultiplied by its alpha (weightOne is all)
	spans  [][2]int // per row, the columns [from, to) with a weight
	// drawnInto is the canvas the current layer was last copied into.
	drawnInto *byte
	// The soft shadows, on a coarse grid (see build).
	dropShadow, insetKeep grid
}

type layerKey struct {
	side                    int
	radius, scale           float64
	glassScale, alpha, blur float64
	theme                   Theme
	label                   *Image
	labelAlpha, labelLift   float64
}

// Compose draws one frame without keeping anything (for tests and one-off frames).
func Compose(canvas *Image, capture *Capture, scene Scene) {
	(&Renderer{}).Compose(canvas, capture, scene)
}

// Compose draws `scene` into `canvas` (sized Side(scene.Radius, capture.Scale)), from `capture`.
func (renderer *Renderer) Compose(canvas *Image, capture *Capture, scene Scene) {
	frame := scene.Frame
	key := layerKey{
		side: canvas.Width, radius: math.Round(scene.Radius), scale: capture.Scale,
		glassScale: frame.GlassScale, alpha: frame.Alpha, blur: frame.Blur, theme: scene.Theme,
		label: scene.Label, labelAlpha: frame.LabelAlpha, labelLift: frame.LabelLift,
	}
	if key != renderer.key || len(renderer.layer) != len(canvas.Pixels) {
		renderer.build(key)
		renderer.drawnInto = nil
	}
	// Outside the glass the canvas only changes with the layer: copy it only when it is new here.
	if len(canvas.Pixels) > 0 && renderer.drawnInto != &canvas.Pixels[0] {
		copy(canvas.Pixels, renderer.layer)
		renderer.drawnInto = &canvas.Pixels[0]
	}
	if frame.Alpha <= 0 {
		return
	}

	pad := float64(Pad(capture.Scale))
	centre := pad + key.radius
	perZoom := 1 / (math.Max(1, frame.Zoom) * frame.GlassScale)
	originX := capture.PointerX - float64(capture.Left)
	originY := capture.PointerY - float64(capture.Top)
	var blurred *Image
	var blurMix int // 0-256
	var blurFactor float64
	if frame.Blur > 0.05 {
		blurred = capture.blurredCopy()
		blurMix = int(min(1, frame.Blur/lens.BlurMax) * 256)
		blurFactor = float64(capture.blurredScale)
	}
	backdrop := scene.Theme.Backdrop
	side := canvas.Width
	parallelRows(canvas.Height, func(y int) {
		span := renderer.spans[y]
		if span[0] >= span[1] {
			return
		}
		out := canvas.Pixels[y*side*4 : (y+1)*side*4]
		layer := renderer.layer[y*side*4 : (y+1)*side*4]
		weights := renderer.weight[y*side : (y+1)*side]
		// Source position of the first pixel in the span; it steps by perZoom per pixel.
		sourceY := originY + (float64(y)+0.5-centre)*perZoom
		sourceX := originX + (float64(span[0])+0.5-centre)*perZoom
		if blurred != nil {
			renderer.blurredRow(out, layer, weights, span, capture, blurred, sourceX, sourceY, perZoom, blurFactor, blurMix, backdrop)
			return
		}
		sharpRow(out, layer, weights, span, &capture.Image, sourceX, sourceY, perZoom, backdrop)
	})
}

// rowSampler reads one row of an image bilinearly, in fixed point, stepping along x: everything that
// depends only on the row is worked out once, so each pixel is a horizontal blend of two rows.
type rowSampler struct {
	row0, row1        []byte
	upper, lower      int
	width             int
	fixedX, fixedStep int
	outside           bool // the whole row is past the image's top or bottom
}

// newRowSampler starts at source (x, y) (pixel centres at .5), stepping `step` source pixels per pixel.
func newRowSampler(image *Image, x, y, step float64) rowSampler {
	width, height := image.Width, image.Height
	y -= 0.5
	if y < -1 || y > float64(height) {
		return rowSampler{outside: true}
	}
	fixedY := int((y + 1) * 256) // +1 keeps the truncation a floor just above the top edge
	y0, fy := fixedY>>8-1, fixedY&255
	y1 := min(height-1, y0+1)
	y0 = max(0, y0)
	return rowSampler{
		row0: image.Pixels[y0*width*4 : (y0+1)*width*4], row1: image.Pixels[y1*width*4 : (y1+1)*width*4],
		upper: 256 - fy, lower: fy, width: width,
		// x in 16.16 fixed point, offset by one pixel for the same reason as y.
		fixedX: int((x - 0.5 + 1) * 65536), fixedStep: int(step * 65536),
	}
}

// next returns this pixel's colour in 8.8 fixed point (ok false past the image's edge) and steps on.
func (r *rowSampler) next() (b, g, red int, ok bool) {
	fixedX := r.fixedX
	r.fixedX += r.fixedStep
	if r.outside {
		return 0, 0, 0, false
	}
	x0 := fixedX>>16 - 1
	if x0 < -1 || x0 >= r.width {
		return 0, 0, 0, false
	}
	fx := (fixedX >> 8) & 255
	x1 := min(r.width-1, x0+1)
	x0 = max(0, x0)
	i0, i1 := x0*4, x1*4
	left, right := 256-fx, fx
	row0, row1 := r.row0, r.row1
	b = ((int(row0[i0])*left+int(row0[i1])*right)*r.upper + (int(row1[i0])*left+int(row1[i1])*right)*r.lower) >> 8
	g = ((int(row0[i0+1])*left+int(row0[i1+1])*right)*r.upper + (int(row1[i0+1])*left+int(row1[i1+1])*right)*r.lower) >> 8
	red = ((int(row0[i0+2])*left+int(row0[i1+2])*right)*r.upper + (int(row1[i0+2])*left+int(row1[i1+2])*right)*r.lower) >> 8
	return b, g, red, true
}

// sharpRow adds one row of magnified content.
func sharpRow(out, layer []byte, weights []uint16, span [2]int, image *Image, sourceX, sourceY, step float64, backdrop [3]byte) {
	sampler := newRowSampler(image, sourceX, sourceY, step)
	backB, backG, backR := int(backdrop[2])<<8, int(backdrop[1])<<8, int(backdrop[0])<<8
	for x := span[0]; x < span[1]; x++ {
		b, g, r, ok := sampler.next()
		w := int(weights[x])
		if w == 0 {
			continue
		}
		if !ok {
			b, g, r = backB, backG, backR
		}
		addContent(out, layer, x*4, b, g, r, w)
	}
}

// blurredRow is sharpRow while the glass is blurred (opening and closing only): sharp and blurred
// samples mixed.
func (renderer *Renderer) blurredRow(out, layer []byte, weights []uint16, span [2]int, capture *Capture, blurred *Image, sourceX, sourceY, step, blurFactor float64, blurMix int, backdrop [3]byte) {
	sharp := newRowSampler(&capture.Image, sourceX, sourceY, step)
	soft := newRowSampler(blurred, sourceX/blurFactor, sourceY/blurFactor, step/blurFactor)
	backB, backG, backR := int(backdrop[2])<<8, int(backdrop[1])<<8, int(backdrop[0])<<8
	for x := span[0]; x < span[1]; x++ {
		b, g, r, ok := sharp.next()
		bb, bg, br, softOK := soft.next()
		w := int(weights[x])
		if w == 0 {
			continue
		}
		if !ok {
			b, g, r = backB, backG, backR
		}
		if !softOK {
			bb, bg, br = backB, backG, backR
		}
		addContent(out, layer, x*4, b+(bb-b)*blurMix>>8, g+(bg-g)*blurMix>>8, r+(br-r)*blurMix>>8, w)
	}
}

// addContent writes the layer pixel at i plus content (8.8 fixed point) at a 12-bit weight.
func addContent(out, layer []byte, i, b, g, r, w int) {
	out[i] = clampByte(int(layer[i]) + (b*w)>>20)
	out[i+1] = clampByte(int(layer[i+1]) + (g*w)>>20)
	out[i+2] = clampByte(int(layer[i+2]) + (r*w)>>20)
}

// weightOne is a content weight of 1 in the renderer's fixed point.
const weightOne = 1 << 12

func clampByte(value int) byte {
	if value > 255 {
		return 255
	}
	return byte(value)
}

// build draws the layer and weights for `key`. It runs every frame while the glass is opening or
// closing (its size changes each frame), so it has to be quick: the shadows, which are soft, are worked
// out on a quarter-resolution grid and interpolated, and only the thin anti-aliased ring at the glass's
// edge is worked out pixel by pixel.
func (renderer *Renderer) build(key layerKey) {
	renderer.key = key
	side := key.side
	if len(renderer.layer) != side*side*4 {
		renderer.layer = make([]byte, side*side*4)
		renderer.weight = make([]uint16, side*side)
		renderer.spans = make([][2]int, side)
	}
	clear(renderer.layer)
	clear(renderer.weight)
	for y := range renderer.spans {
		renderer.spans[y] = [2]int{}
	}
	layer := &Image{Pixels: renderer.layer, Width: side, Height: side}
	s := key.scale
	pad := float64(Pad(s))
	if key.alpha > 0 {
		gs := key.glassScale
		r := key.radius * gs
		k := s * gs // CSS pixel → canvas pixel, inside the scaled glass
		blur := key.blur * k
		centre := pad + key.radius
		// Two drop shadows, as in lens.css: 0 18px 50px -12px (35%) and 0 4px 14px -6px (25%).
		wide, tight := newShadow(r-12*k, 25*k, 0.35), newShadow(r-6*k, 7*k, 0.25)
		// The inset shadow, inset 0 10px 30px -18px (35%): a soft darkening along the top inside edge.
		inset := newShadow(r+18*k, 15*k, 1)
		renderer.dropShadow.fill(side, func(x, y float64) float64 {
			dx, wideDy, tightDy := x-centre, y-centre-18*k, y-centre-4*k
			return 1 - (1-wide.at(math.Sqrt(dx*dx+wideDy*wideDy)))*(1-tight.at(math.Sqrt(dx*dx+tightDy*tightDy)))
		})
		renderer.insetKeep.fill(side, func(x, y float64) float64 {
			dx, dy := x-centre, y-centre-10*k
			return 1 - 0.35*(1-inset.at(math.Sqrt(dx*dx+dy*dy)))
		})
		edge := 1 + blur
		innerEdge := r - 2*k
		job := layerJob{
			renderer: renderer, theme: key.theme,
			centre: centre, r: r, innerEdge: innerEdge, edge: edge, alpha: key.alpha, highlight: k,
			// Outside this, only shadow; inside that, only content (and the inset shadow's darkening).
			ringOuter: r + edge/2 + 1, ringInner: math.Max(0, innerEdge-k-2),
			reach: r + 18*k + 75*k + blur,
		}
		parallelRows(side, job.row)
	}
	if key.label != nil && key.labelAlpha > 0 {
		// lens.css: the label's bottom edge sits 26px below the (unscaled) glass, centred.
		top := pad + 2*key.radius + 26*s - float64(key.label.Height) - key.labelLift*s
		blit(layer, key.label, int(math.Round(pad+key.radius-float64(key.label.Width)/2)), int(math.Round(top)), key.labelAlpha)
		// The label sits below the glass, so it never overlaps a content pixel.
	}
}

// grid is a smooth quantity (a shadow) sampled every gridStep pixels and interpolated between.
type grid struct {
	values      []float32
	columns     int
	rows        int
	sampledSide int
}

const gridStep = 4

// fill samples `at` (canvas pixel coordinates) on the grid for a canvas `side` pixels square.
func (g *grid) fill(side int, at func(x, y float64) float64) {
	n := side/gridStep + 2
	if g.sampledSide != side {
		g.values = make([]float32, n*n)
		g.columns, g.rows, g.sampledSide = n, n, side
	}
	parallelRows(n, func(j int) {
		for i := 0; i < n; i++ {
			g.values[j*n+i] = float32(at(float64(i*gridStep), float64(j*gridStep)))
		}
	})
}

// at interpolates the grid at canvas pixel (x, y).
func (g *grid) at(x, y float64) float64 {
	gx, gy := x/gridStep, y/gridStep
	i, j := int(gx), int(gy)
	i, j = min(max(i, 0), g.columns-2), min(max(j, 0), g.rows-2)
	fx, fy := float32(gx-float64(i)), float32(gy-float64(j))
	row := g.values[j*g.columns:]
	next := g.values[(j+1)*g.columns:]
	top := row[i] + (row[i+1]-row[i])*fx
	bottom := next[i] + (next[i+1]-next[i])*fx
	return float64(top + (bottom-top)*fy)
}

type layerJob struct {
	renderer                   *Renderer
	theme                      Theme
	centre, r, innerEdge, edge float64
	alpha, highlight           float64
	ringOuter, ringInner       float64
	reach                      float64
}

// row draws one row of the layer and its content weights. Everything in the lens is mirror-symmetric
// left to right (the canvas is centred on the glass), so only the left half is worked out.
func (j *layerJob) row(y int) {
	py := float64(y) + 0.5
	dy := py - j.centre
	if math.Abs(dy) > j.reach {
		return
	}
	renderer := j.renderer
	side := renderer.key.side
	span := math.Sqrt(j.reach*j.reach - dy*dy)
	from, to := max(0, int(j.centre-span)), (side+1)/2
	out := renderer.layer[y*side*4 : (y+1)*side*4]
	weights := renderer.weight[y*side : (y+1)*side]
	contentFrom := side
	outerSquared, innerSquared := j.ringOuter*j.ringOuter, j.ringInner*j.ringInner
	theme := j.theme
	for x := from; x < to; x++ {
		px := float64(x) + 0.5
		dx := px - j.centre
		distanceSquared := dx*dx + dy*dy
		var pixel [4]byte
		weight := 0
		switch {
		case distanceSquared >= outerSquared:
			// Outside the glass: its drop shadow only.
			pixel[3] = byte(renderer.dropShadow.at(px, py)*j.alpha*255 + 0.5)
		case distanceSquared <= innerSquared:
			// Inside: the content, darkened towards the top by the inset shadow.
			pixel[3] = byte(j.alpha*255 + 0.5)
			weight = int(renderer.insetKeep.at(px, py)*j.alpha*weightOne + 0.5)
		default:
			// The ring at the edge: anti-aliased border, inner highlight, shadow under the edge.
			distance := math.Sqrt(distanceSquared)
			coverage := clamp01((j.r-distance)/j.edge + 0.5)
			keep, addB, addG, addR := 0.0, 0.0, 0.0, 0.0
			if coverage > 0 {
				if distance < j.innerEdge+1 {
					keep = renderer.insetKeep.at(px, py)
					if lit := 0.6 * clamp01(distance-(j.innerEdge-j.highlight)+0.5) * clamp01(j.innerEdge-distance+0.5); lit > 0 {
						keep *= 1 - lit
						addB, addG, addR = float64(theme.Surface[2])*lit, float64(theme.Surface[1])*lit, float64(theme.Surface[0])*lit
					}
				}
				if ring := clamp01(distance - j.innerEdge + 0.5); ring > 0 {
					keep *= 1 - ring
					addB = addB*(1-ring) + float64(theme.Border[2])*ring
					addG = addG*(1-ring) + float64(theme.Border[1])*ring
					addR = addR*(1-ring) + float64(theme.Border[0])*ring
				}
			}
			shade := 0.0
			if coverage < 1 {
				shade = renderer.dropShadow.at(px, py)
			}
			// Glass over its shadow, then the whole element's opacity.
			c := coverage * j.alpha
			pixel = [4]byte{byte(addB*c + 0.5), byte(addG*c + 0.5), byte(addR*c + 0.5), byte((coverage+shade*(1-coverage))*j.alpha*255 + 0.5)}
			weight = int(keep*c*weightOne + 0.5)
		}
		mirror := side - 1 - x
		copy(out[x*4:x*4+4], pixel[:])
		copy(out[mirror*4:mirror*4+4], pixel[:])
		if weight > 0 {
			weights[x], weights[mirror] = uint16(weight), uint16(weight)
			contentFrom = min(contentFrom, x)
		}
	}
	if contentFrom < side {
		renderer.spans[y] = [2]int{contentFrom, side - contentFrom}
	}
}

// blurredCopy is the capture box-averaged down by about 6 CSS pixels; sampled back up bilinearly it
// reads as a soft blur. Made once per capture, and only while the glass is blurred.
func (c *Capture) blurredCopy() *Image {
	if c.blurred != nil {
		return c.blurred
	}
	factor := max(2, int(math.Round(6*c.Scale)))
	width, height := max(1, c.Width/factor), max(1, c.Height/factor)
	out := NewImage(width, height)
	parallelRows(height, func(y int) {
		for x := 0; x < width; x++ {
			var b, g, r int
			for sy := y * factor; sy < (y+1)*factor; sy++ {
				i := (sy*c.Width + x*factor) * 4
				for sx := 0; sx < factor; sx++ {
					b, g, r = b+int(c.Pixels[i]), g+int(c.Pixels[i+1]), r+int(c.Pixels[i+2])
					i += 4
				}
			}
			n := factor * factor
			o := (y*width + x) * 4
			out.Pixels[o], out.Pixels[o+1], out.Pixels[o+2], out.Pixels[o+3] = byte(b/n), byte(g/n), byte(r/n), 255
		}
	})
	c.blurred, c.blurredScale = out, factor
	return out
}

// blit draws a premultiplied sprite over the canvas at (left, top), faded by `alpha`.
func blit(canvas, sprite *Image, left, top int, alpha float64) {
	for y := 0; y < sprite.Height; y++ {
		cy := top + y
		if cy < 0 || cy >= canvas.Height {
			continue
		}
		for x := 0; x < sprite.Width; x++ {
			cx := left + x
			if cx < 0 || cx >= canvas.Width {
				continue
			}
			s := sprite.Pixels[(y*sprite.Width+x)*4:]
			sa := float64(s[3]) / 255 * alpha
			if sa == 0 {
				continue
			}
			d := canvas.Pixels[(cy*canvas.Width+cx)*4:]
			keep := 1 - sa
			for c := 0; c < 3; c++ {
				d[c] = byte(float64(s[c])*alpha + float64(d[c])*keep + 0.5)
			}
			d[3] = byte(sa*255 + float64(d[3])*keep + 0.5)
		}
	}
}

// parallelRows runs `row` for every y in [0, height), shared across the CPU's cores.
func parallelRows(height int, row func(y int)) {
	// Eight is plenty for a few milliseconds of work, and leaves the other cores (and the E-cores) idle.
	workers := min(8, runtime.NumCPU(), height)
	var group sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		group.Add(1)
		go func(first int) {
			defer group.Done()
			for y := first; y < height; y += workers {
				row(y)
			}
		}(worker)
	}
	group.Wait()
}

// shadow is a blurred disc's alpha by distance from its centre (Gaussian edge), from a lookup table.
type shadow struct {
	start, step float64
	alpha       []float64
}

func newShadow(radius, sigma, strength float64) shadow {
	sigma = math.Max(sigma, 0.5)
	const step = 0.25
	start := radius - 3*sigma
	table := make([]float64, int(6*sigma/step)+2)
	for i := range table {
		distance := start + float64(i)*step
		table[i] = strength * 0.5 * math.Erfc((distance-radius)/(sigma*math.Sqrt2))
	}
	return shadow{start: start, step: step, alpha: table}
}

func (s shadow) at(distance float64) float64 {
	index := int((distance - s.start) / s.step)
	switch {
	case index < 0:
		return s.alpha[0]
	case index >= len(s.alpha):
		return 0
	}
	return s.alpha[index]
}

func mix(value float64, toward byte, amount float64) float64 {
	return value + (float64(toward)-value)*amount
}

func clamp01(value float64) float64 { return min(1, max(0, value)) }
