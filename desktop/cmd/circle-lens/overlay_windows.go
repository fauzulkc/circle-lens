//go:build windows

package main

// The overlay is the one window the lens lives in: topmost, click-through, never focused, with
// per-pixel alpha, and left out of screen captures so the capture thread sees what is behind it.
// internal/render draws each frame straight into its DIB; UpdateLayeredWindow puts it on screen.

import (
	"fmt"
	"math"
	"unsafe"

	"circlelens/internal/lens"
	"circlelens/internal/render"
)

type overlay struct {
	window   uintptr
	excluded bool // Windows leaves it out of captures (Windows 10 2004 and later)
	shown    bool
	canvas   *dib
	label    *render.Image
	labelKey string
	renderer render.Renderer
}

func newOverlay(module uintptr) (*overlay, bool) {
	className := wide(instance + "Overlay")
	class := windowClassEx{WndProc: procDefWindowProc.Addr(), Instance: module, ClassName: className}
	class.Size = uint32(unsafe.Sizeof(class))
	procRegisterClassEx.Call(uintptr(unsafe.Pointer(&class)))
	style := uintptr(wsExTopmost | wsExLayered | wsExTransparent | wsExToolWindow | wsExNoActivate)
	window, _, _ := procCreateWindowEx.Call(style, uintptr(unsafe.Pointer(className)), 0, wsPopup, 0, 0, 1, 1, 0, 0, module, 0)
	if window == 0 {
		return nil, false
	}
	ok, _, err := procSetWindowDisplayAff.Call(window, wdaExcludeFromCapture)
	if ok == 0 {
		debugLog("cannot leave the lens out of captures (%v): it will show a still of the screen", err)
	}
	return &overlay{window: window, excluded: ok != 0}, true
}

// draw composes one frame around the capture's pointer and puts it on screen.
func (o *overlay) draw(capture *render.Capture, frame lens.Frame, radiusDip float64, theme render.Theme, dark bool, zoom float64) {
	scale := capture.Scale
	radius := math.Round(radiusDip * scale)
	side := render.Side(radius, scale)
	if o.canvas == nil || o.canvas.Width != side {
		if o.canvas != nil {
			o.canvas.Close()
		}
		var ok bool
		if o.canvas, ok = newDIB(side, side); !ok {
			o.canvas = nil
			return
		}
	}
	canvas := &render.Image{Pixels: o.canvas.Pixels, Width: side, Height: side}
	o.renderer.Compose(canvas, capture, render.Scene{Frame: frame, Radius: radius, Theme: theme, Label: o.labelFor(zoom, scale, dark, theme)})
	saveFrame(canvas)

	pad := float64(render.Pad(scale))
	destination := point{int32(math.Floor(capture.PointerX - radius - pad)), int32(math.Floor(capture.PointerY - radius - pad))}
	dimensions := size{int32(side), int32(side)}
	origin := point{}
	blend := blendFunction{SourceConstantAlpha: 255, AlphaFormat: acSrcAlpha}
	procUpdateLayeredWindow.Call(o.window, 0, uintptr(unsafe.Pointer(&destination)), uintptr(unsafe.Pointer(&dimensions)), o.canvas.dc,
		uintptr(unsafe.Pointer(&origin)), 0, uintptr(unsafe.Pointer(&blend)), ulwAlpha)
	if !o.shown {
		procShowWindow.Call(o.window, swShowNoActivate)
		procSetWindowPos.Call(o.window, hwndTopmost, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoActivate)
		o.shown = true
	}
}

func (o *overlay) hide() {
	if o.shown {
		procShowWindow.Call(o.window, swHide)
		o.shown = false
	}
}

// release frees the canvas and label; they are made again on the next open.
func (o *overlay) release() {
	if o.canvas != nil {
		o.canvas.Close()
		o.canvas = nil
	}
	o.label, o.labelKey = nil, ""
	o.renderer = render.Renderer{}
}

// labelFor is the "1.8× · Esc" pill, drawn by GDI once per text, scale and theme.
func (o *overlay) labelFor(zoom, scale float64, dark bool, theme render.Theme) *render.Image {
	text := fmt.Sprintf("%.1f× · Esc", zoom)
	key := fmt.Sprintf("%s|%.2f|%v", text, scale, dark)
	if key == o.labelKey {
		return o.label
	}
	o.label, o.labelKey = drawLabel(text, scale, dark, theme), key
	return o.label
}

// drawLabel renders the pill from lens.css: surface background, 1px line border, muted 11px text,
// fully rounded. GDI draws the text; the shape and its anti-aliasing are drawn here.
func drawLabel(text string, scale float64, dark bool, theme render.Theme) *render.Image {
	line, muted := [3]byte{0xde, 0xdf, 0xe1}, [3]byte{0x4b, 0x55, 0x63}
	if dark {
		line, muted = [3]byte{0x3d, 0x3f, 0x46}, [3]byte{0xa1, 0xa1, 0xaa}
	}
	font, _, _ := procCreateFont.Call(uintptr(-int32(math.Round(11*scale))), 0, 0, 0, fwMedium, 0, 0, 0, defaultCharset, 0, 0, antialiasedQuality, 0, uintptr(unsafe.Pointer(wide("Segoe UI"))))
	if font == 0 {
		return nil
	}
	defer procDeleteObject.Call(font)
	textPointer := wide(text)
	length := uintptr(len([]rune(text)))

	measuring, ok := newDIB(1, 1)
	if !ok {
		return nil
	}
	previous, _, _ := procSelectObject.Call(measuring.dc, font)
	var extent size
	procGetTextExtentPoint32.Call(measuring.dc, uintptr(unsafe.Pointer(textPointer)), length, uintptr(unsafe.Pointer(&extent)))
	procSelectObject.Call(measuring.dc, previous)
	measuring.Close()

	height := int(math.Round(22 * scale))
	width := int(extent.CX) + int(math.Round(20*scale))
	pill, ok := newDIB(width, height)
	if !ok {
		return nil
	}
	defer pill.Close()
	surface := theme.Surface
	for i := 0; i < len(pill.Pixels); i += 4 {
		pill.Pixels[i], pill.Pixels[i+1], pill.Pixels[i+2], pill.Pixels[i+3] = surface[2], surface[1], surface[0], 255
	}
	previous, _, _ = procSelectObject.Call(pill.dc, font)
	procSetBkMode.Call(pill.dc, transparentBkMode)
	procSetTextColor.Call(pill.dc, uintptr(muted[0])|uintptr(muted[1])<<8|uintptr(muted[2])<<16)
	procTextOut.Call(pill.dc, uintptr((width-int(extent.CX))/2), uintptr((height-int(extent.CY))/2), uintptr(unsafe.Pointer(textPointer)), length)
	procSelectObject.Call(pill.dc, previous)
	procGdiFlush.Call()

	out := render.NewImage(width, height)
	half := float64(height) / 2
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			px, py := float64(x)+0.5, float64(y)+0.5
			// A stadium: distance to its centre line, minus its half-height.
			along := math.Max(half, math.Min(float64(width)-half, px))
			edge := math.Hypot(px-along, py-half) - half
			coverage := math.Min(1, math.Max(0, 0.5-edge))
			if coverage == 0 {
				continue
			}
			onLine := math.Min(1, math.Max(0, edge+scale+0.5))
			i := (y*width + x) * 4
			for c := 0; c < 3; c++ {
				value := float64(pill.Pixels[i+c])*(1-onLine) + float64(line[2-c])*onLine
				out.Pixels[i+c] = byte(value*coverage + 0.5)
			}
			out.Pixels[i+3] = byte(coverage*255 + 0.5)
		}
	}
	return out
}
