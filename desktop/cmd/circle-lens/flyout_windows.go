//go:build windows

package main

// The flyout is what a left click on the tray icon opens, in the style of Windows 11's own tray flyouts
// (quick settings, volume): a rounded, shadowed panel above the taskbar with a header and an on/off
// switch, the main action as an accent button, segmented pickers for the settings, and a footer. It
// follows light or dark mode and the Windows accent colour, slides up as it fades in, and works from
// the keyboard (Tab, arrows, Enter or Space, Esc). A right click still opens the native menu, which
// screen readers know how to read.
//
// It is drawn the same way as the lens: shapes by hand into a per-pixel-alpha window, text by GDI.

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"syscall"
	"time"
	"unsafe"

	"circlelens/internal/icon"
	"circlelens/internal/motion"
	"circlelens/internal/render"
)

const (
	flyoutWidth  = 320 // DIPs, the panel without its shadow
	flyoutHeight = 468
	flyoutShadow = 24 // room around the panel for its shadow
	flyoutGap    = 12 // from the taskbar and the screen edge
	flyoutRadius = 8

	flyoutInMs  = 200
	flyoutOutMs = 120
	flyoutRise  = 10 // DIPs it slides up while appearing

	timerFlyout = 1
)

type flyoutTheme struct {
	panel, border, divider, control, controlHover, text, secondary, tertiary, accent, onAccent [3]byte
}

func newFlyoutTheme(dark bool) flyoutTheme {
	accent := accentColor()
	var theme flyoutTheme
	if dark {
		// Windows 11 fills with a lighter shade of the accent in dark mode, a darker one in light mode.
		theme = flyoutTheme{
			panel: rgb(0x2c2c2c), border: rgb(0x454545), divider: rgb(0x3d3d3d), control: rgb(0x373737), controlHover: rgb(0x414141),
			text: rgb(0xffffff), secondary: rgb(0xc9c9c9), tertiary: rgb(0x9a9a9a),
			accent: mixColour(accent, [3]byte{255, 255, 255}, 0.45),
		}
	} else {
		theme = flyoutTheme{
			panel: rgb(0xf9f9f9), border: rgb(0xdcdcdc), divider: rgb(0xe6e6e6), control: rgb(0xececec), controlHover: rgb(0xe1e1e1),
			text: rgb(0x1b1b1b), secondary: rgb(0x5d5d5d), tertiary: rgb(0x8a8a8a),
			accent: mixColour(accent, [3]byte{0, 0, 0}, 0.2),
		}
	}
	// Text on the accent is whichever of black or white reads better on it (any accent a person picks).
	theme.onAccent = rgb(0xffffff)
	if contrast(theme.accent, [3]byte{0, 0, 0}) > contrast(theme.accent, [3]byte{255, 255, 255}) {
		theme.onAccent = rgb(0x000000)
	}
	return theme
}

// contrast is the WCAG contrast ratio between two colours (1 to 21).
func contrast(a, b [3]byte) float64 {
	luminance := func(c [3]byte) float64 {
		channel := func(v byte) float64 {
			x := float64(v) / 255
			if x <= 0.03928 {
				return x / 12.92
			}
			return math.Pow((x+0.055)/1.055, 2.4)
		}
		return 0.2126*channel(c[0]) + 0.7152*channel(c[1]) + 0.0722*channel(c[2])
	}
	la, lb := luminance(a), luminance(b)
	return (math.Max(la, lb) + 0.05) / (math.Min(la, lb) + 0.05)
}

func rgb(value uint32) [3]byte { return [3]byte{byte(value >> 16), byte(value >> 8), byte(value)} }

func mixColour(from, to [3]byte, amount float64) [3]byte {
	var out [3]byte
	for i := range out {
		out[i] = byte(float64(from[i]) + (float64(to[i])-float64(from[i]))*amount + 0.5)
	}
	return out
}

type controlKind int

const (
	kindSwitch controlKind = iota
	kindButton
	kindSegment
	kindRow
	kindLink
)

// control is one clickable thing, laid out in physical pixels within the window.
type control struct {
	id         string
	stop       int // tab stop; a segmented picker's segments share one
	kind       controlKind
	x, y, w, h float64
	label      string
	detail     string // the button's shortcut
	on         bool   // a switch that is on, or the chosen segment
	action     func()
}

type flyout struct {
	window    uintptr
	canvas    *dib
	scale     float64
	theme     flyoutTheme
	dark      bool
	left, top int // window position on screen
	controls  []control
	hover     string
	pressed   string
	focus     int // the focused tab stop, or -1 until the keyboard is used
	fonts     map[[2]int]uintptr
	face      string

	shown, closing bool
	animationFrom  time.Time
	closedAt       time.Time
	tracking       bool
}

var theFlyout *flyout

func newFlyout(module uintptr) *flyout {
	className := wide(instance + "Flyout")
	cursor, _, _ := procLoadCursor.Call(0, idcArrow)
	class := windowClassEx{WndProc: newCallback(flyoutProcedure), Instance: module, ClassName: className, Cursor: cursor}
	class.Size = uint32(unsafe.Sizeof(class))
	procRegisterClassEx.Call(uintptr(unsafe.Pointer(&class)))
	window, _, _ := procCreateWindowEx.Call(wsExTopmost|wsExToolWindow|wsExLayered, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(wide("Circle Lens"))), wsPopup, 0, 0, 1, 1, 0, 0, module, 0)
	theFlyout = &flyout{window: window, focus: -1, fonts: map[[2]int]uintptr{}}
	return theFlyout
}

// toggle opens the flyout, or closes it. A click on the tray icon that closed it (by taking focus
// away) must not open it again straight after.
func (f *flyout) toggle() {
	if f.shown && !f.closing {
		f.close()
		return
	}
	if time.Since(f.closedAt) < 300*time.Millisecond {
		return
	}
	f.open()
}

func (f *flyout) open() {
	f.dark = darkMode()
	f.theme = newFlyoutTheme(f.dark)
	f.hover, f.pressed, f.focus = "", "", -1

	// Place it above the tray icon, inside the work area of the taskbar's monitor.
	anchor := f.trayIconRect()
	f.scale = monitorScale(point{(anchor.Left + anchor.Right) / 2, (anchor.Top + anchor.Bottom) / 2})
	work := workArea(point{(anchor.Left + anchor.Right) / 2, (anchor.Top + anchor.Bottom) / 2})
	width, height := f.px(flyoutWidth+2*flyoutShadow), f.px(flyoutHeight+2*flyoutShadow)
	shadow, gap := f.px(flyoutShadow), f.px(flyoutGap)
	centreX := int((anchor.Left + anchor.Right) / 2)
	f.left = max(int(work.Left)+gap-shadow, min(int(work.Right)-gap+shadow-width, centreX-width/2))
	switch {
	case anchor.Top >= work.Bottom: // taskbar at the bottom (the usual place)
		f.top = int(work.Bottom) - gap + shadow - height
	case anchor.Bottom <= work.Top: // at the top
		f.top = int(work.Top) + gap - shadow
	default: // at a side: level with the icon
		f.top = max(int(work.Top)+gap-shadow, min(int(work.Bottom)-gap+shadow-height, int(anchor.Top)-height/2))
		if anchor.Right <= work.Left {
			f.left = int(work.Left) + gap - shadow
		} else {
			f.left = int(work.Right) - gap + shadow - width
		}
	}

	if f.canvas == nil || f.canvas.Width != width || f.canvas.Height != height {
		if f.canvas != nil {
			f.canvas.Close()
		}
		var ok bool
		if f.canvas, ok = newDIB(width, height); !ok {
			f.canvas = nil
			return
		}
	}
	f.layout()
	f.render()
	debugLog("flyout at (%d, %d) %dx%d, tray icon %v, work area %v", f.left, f.top, width, height, anchor, work)
	f.shown, f.closing, f.animationFrom = true, false, time.Now()
	f.present(0, f.px(flyoutRise))
	procShowWindow.Call(f.window, swShow)
	procSetForegroundWindow.Call(f.window)
	procSetTimer.Call(f.window, timerFlyout, 8, 0)
}

func (f *flyout) close() {
	if !f.shown || f.closing {
		return
	}
	f.closing, f.animationFrom = true, time.Now()
	procSetTimer.Call(f.window, timerFlyout, 8, 0)
}

// animate moves the open or close animation on by one step.
func (f *flyout) animate() {
	elapsed := float64(time.Since(f.animationFrom).Milliseconds())
	if f.closing {
		t := elapsed / flyoutOutMs
		if t >= 1 {
			procKillTimer.Call(f.window, timerFlyout)
			procShowWindow.Call(f.window, swHide)
			f.shown, f.closing, f.closedAt = false, false, time.Now()
			f.release()
			return
		}
		f.present(1-motion.Ease(t), 0)
		return
	}
	t := elapsed / flyoutInMs
	if t >= 1 {
		procKillTimer.Call(f.window, timerFlyout)
		f.present(1, 0)
		return
	}
	e := motion.Glide(t)
	f.present(e, int(math.Round((1-e)*float64(f.px(flyoutRise)))))
}

// release frees what the flyout drew with; it is made again the next time it opens.
func (f *flyout) release() {
	if f.canvas != nil {
		f.canvas.Close()
		f.canvas = nil
	}
	for key, font := range f.fonts {
		procDeleteObject.Call(font)
		delete(f.fonts, key)
	}
}

func (f *flyout) present(opacity float64, rise int) {
	if f.canvas == nil {
		return
	}
	destination := point{int32(f.left), int32(f.top + rise)}
	dimensions := size{int32(f.canvas.Width), int32(f.canvas.Height)}
	origin := point{}
	blend := blendFunction{SourceConstantAlpha: byte(math.Round(math.Max(0, math.Min(1, opacity)) * 255)), AlphaFormat: acSrcAlpha}
	procUpdateLayeredWindow.Call(f.window, 0, uintptr(unsafe.Pointer(&destination)), uintptr(unsafe.Pointer(&dimensions)), f.canvas.dc,
		uintptr(unsafe.Pointer(&origin)), 0, uintptr(unsafe.Pointer(&blend)), ulwAlpha)
}

func (f *flyout) px(dip float64) int { return int(math.Round(dip * f.scale)) }

func (f *flyout) trayIconRect() rect {
	identifier := notifyIconIdentifier{Wnd: app.window, ID: 1}
	identifier.Size = uint32(unsafe.Sizeof(identifier))
	var r rect
	if result, _, _ := procShellNotifyIconGetRect.Call(uintptr(unsafe.Pointer(&identifier)), uintptr(unsafe.Pointer(&r))); result != 0 {
		// Not found (an overflow area that is closed): use the pointer, which just clicked it.
		var cursor point
		procGetCursorPos.Call(uintptr(unsafe.Pointer(&cursor)))
		r = rect{cursor.X, cursor.Y, cursor.X + 1, cursor.Y + 1}
	}
	return r
}

/* ---------- layout ---------- */

// layout places every control for the current settings, in window pixels.
func (f *flyout) layout() {
	s := func(dip float64) float64 { return dip * f.scale }
	x0, y0 := s(flyoutShadow), s(flyoutShadow)
	inner := s(flyoutWidth - 32)
	a := app
	var controls []control
	add := func(c control) { controls = append(controls, c) }

	add(control{id: "watch", stop: 0, kind: kindSwitch, x: x0 + s(flyoutWidth-16-40), y: y0 + s(22), w: s(40), h: s(20), on: a.settings.Enabled,
		label: "Watch for circles", action: func() { a.runCommand(commandEnabled) }})
	add(control{id: "open", stop: 1, kind: kindButton, x: x0 + s(16), y: y0 + s(64), w: inner, h: s(36), label: "Open the lens", detail: "Ctrl+Alt+L",
		action: func() { f.closeNow(); a.open() }})

	segments := func(stop int, group string, top float64, labels []string, chosen int, choose func(int)) {
		width := inner / float64(len(labels))
		for i, label := range labels {
			i := i
			add(control{id: fmt.Sprintf("%s-%d", group, i), stop: stop, kind: kindSegment, x: x0 + s(16) + float64(i)*width, y: y0 + s(top), w: width, h: s(32),
				label: label, on: i == chosen, action: func() { choose(i) }})
		}
	}
	loops := make([]string, len(loopChoices))
	chosenLoops := 0
	for i, choice := range loopChoices {
		loops[i] = map[float64]string{1: "1", 1.5: "1½", 2: "2", 2.5: "2½", 3: "3"}[choice.loops]
		if choice.loops == a.settings.Loops {
			chosenLoops = i
		}
	}
	segments(2, "loops", 134, loops, chosenLoops, func(i int) { a.runCommand(commandLoops + i) })
	zooms := make([]string, len(zoomChoices))
	chosenZoom := 0
	for i, zoom := range zoomChoices {
		zooms[i] = trimZoom(zoom) + "×"
		if zoom == a.settings.Zoom {
			chosenZoom = i
		}
	}
	segments(3, "zoom", 196, zooms, chosenZoom, func(i int) { a.runCommand(commandZoom + i) })
	sizes := []string{"Small", "Medium", "Large", "X-Large"}
	chosenSize := 0
	for i, choice := range sizeChoices {
		if choice.radius == a.settings.Radius {
			chosenSize = i
		}
	}
	segments(4, "size", 258, sizes, chosenSize, func(i int) { a.runCommand(commandSize + i) })
	gaps := make([]string, len(gapChoices))
	chosenGap := 0
	for i, gap := range gapChoices {
		gaps[i] = trimZoom(gap) + " s"
		if gap == a.settings.Gap {
			chosenGap = i
		}
	}
	segments(5, "gap", 320, gaps, chosenGap, func(i int) { a.runCommand(commandGap + i) })

	add(control{id: "startup", stop: 6, kind: kindRow, x: x0 + s(8), y: y0 + s(374), w: s(flyoutWidth - 16), h: s(36), on: startsWithWindows(),
		label: "Start with Windows", action: func() { a.runCommand(commandStartup) }})
	add(control{id: "quit", stop: 7, kind: kindLink, x: x0 + s(flyoutWidth-16-64), y: y0 + s(428), w: s(64), h: s(28), label: "Quit",
		action: func() { f.closeNow(); a.runCommand(commandQuit) }})
	f.controls = controls
}

func trimZoom(zoom float64) string {
	text := fmt.Sprintf("%.1f", zoom)
	if text[len(text)-2:] == ".0" {
		return text[:len(text)-2]
	}
	return text
}

// closeNow hides the flyout without animating (before an action that takes over the screen).
func (f *flyout) closeNow() {
	procKillTimer.Call(f.window, timerFlyout)
	procShowWindow.Call(f.window, swHide)
	f.shown, f.closing, f.closedAt = false, false, time.Now()
	f.release()
}

/* ---------- drawing ---------- */

func (f *flyout) render() {
	canvas := f.canvas
	if canvas == nil {
		return
	}
	t := f.theme
	s := func(dip float64) float64 { return dip * f.scale }
	x0, y0 := s(flyoutShadow), s(flyoutShadow)
	x1, y1 := x0+s(flyoutWidth), y0+s(flyoutHeight)
	radius := s(flyoutRadius)

	// The panel, opaque for now (its edge and shadow are worked out last), with a 1px border.
	clear(canvas.Pixels)
	f.fill(x0, y0, x1, y1, radius, t.border, 1)
	f.fill(x0+s(1), y0+s(1), x1-s(1), y1-s(1), radius-s(1), t.panel, 1)

	// Header: the mark, the name and what it is doing, and the on/off switch.
	mark := icon.Style{Ink: colourNRGBA(t.text, 255)}
	if app.settings.Enabled {
		mark.Glass = colourNRGBA(t.accent, 255)
		mark.GlassInk = colourNRGBA(t.onAccent, 255)
	} else {
		mark.Paused = true
	}
	f.drawIcon(icon.Draw(f.px(32), mark), x0+s(16), y0+s(16))
	f.text("Circle Lens", x0+s(60), y0+s(16), s(20), 14, fwSemibold, t.text, alignLeft)
	status := fmt.Sprintf("Draw %s to magnify", loopsLabel(app.settings.Loops))
	if !app.settings.Enabled {
		status = "Paused: Ctrl+Alt+L still opens it"
	}
	f.text(status, x0+s(60), y0+s(37), s(16), 12, fwNormal, t.secondary, alignLeft)

	f.text("Circles to draw", x0+s(16), y0+s(112), s(18), 12, fwNormal, t.secondary, alignLeft)
	f.text("Starting zoom", x0+s(16), y0+s(174), s(18), 12, fwNormal, t.secondary, alignLeft)
	f.text("Lens size", x0+s(16), y0+s(236), s(18), 12, fwNormal, t.secondary, alignLeft)
	f.text("Wait between open and close", x0+s(16), y0+s(298), s(18), 12, fwNormal, t.secondary, alignLeft)
	f.fill(x0+s(16), y0+s(364), x1-s(16), y0+s(365), 0, t.divider, 1)
	f.fill(x0+s(16), y0+s(418), x1-s(16), y0+s(419), 0, t.divider, 1)
	f.text("Circle Lens "+version, x0+s(16), y0+s(428), s(28), 12, fwNormal, t.tertiary, alignLeft)

	// Segmented pickers sit in a shared track.
	tracks := map[int][4]float64{}
	for _, c := range f.controls {
		if c.kind != kindSegment {
			continue
		}
		track, ok := tracks[c.stop]
		if !ok {
			track = [4]float64{c.x, c.y, c.x + c.w, c.y + c.h}
		}
		track[2] = c.x + c.w
		tracks[c.stop] = track
	}
	for _, track := range tracks {
		f.fill(track[0], track[1], track[2], track[3], s(5), t.control, 1)
	}

	for _, c := range f.controls {
		hovered, pressed := c.id == f.hover, c.id == f.pressed && c.id == f.hover
		switch c.kind {
		case kindSwitch:
			f.drawSwitch(c.x, c.y, c.w, c.h, c.on, hovered)
		case kindButton:
			fill := t.accent
			if pressed {
				fill = mixColour(t.accent, t.panel, 0.2)
			} else if hovered {
				fill = mixColour(t.accent, t.panel, 0.1)
			}
			f.fill(c.x, c.y, c.x+c.w, c.y+c.h, s(4), fill, 1)
			f.text(c.label, c.x+s(14), c.y, c.h, 14, fwSemibold, t.onAccent, alignLeft)
			f.text(c.detail, c.x+c.w-s(14), c.y, c.h, 12, fwNormal, mixColour(t.onAccent, fill, 0.3), alignRight)
		case kindSegment:
			inset := s(2)
			label := t.text
			switch {
			case c.on:
				f.fill(c.x+inset, c.y+inset, c.x+c.w-inset, c.y+c.h-inset, s(4), t.accent, 1)
				label = t.onAccent
			case pressed:
				f.fill(c.x+inset, c.y+inset, c.x+c.w-inset, c.y+c.h-inset, s(4), mixColour(t.controlHover, t.text, 0.06), 1)
			case hovered:
				f.fill(c.x+inset, c.y+inset, c.x+c.w-inset, c.y+c.h-inset, s(4), t.controlHover, 1)
			}
			f.text(c.label, c.x+c.w/2, c.y, c.h, 13, fwNormal, label, alignCentre)
		case kindRow:
			if hovered {
				f.fill(c.x, c.y, c.x+c.w, c.y+c.h, s(4), t.controlHover, 1)
			}
			f.text(c.label, c.x+s(8), c.y, c.h, 14, fwNormal, t.text, alignLeft)
			f.drawSwitch(c.x+c.w-s(8)-s(40), c.y+(c.h-s(20))/2, s(40), s(20), c.on, hovered)
		case kindLink:
			if hovered {
				f.fill(c.x, c.y, c.x+c.w, c.y+c.h, s(4), t.controlHover, 1)
			}
			f.text(c.label, c.x+c.w/2, c.y, c.h, 13, fwNormal, t.text, alignCentre)
		}
	}

	// The keyboard focus ring: around the focused control (a picker's chosen segment).
	if f.focus >= 0 {
		for _, c := range f.controls {
			if c.stop == f.focus && (c.kind != kindSegment || c.on) {
				f.ring(c.x-s(3), c.y-s(3), c.x+c.w+s(3), c.y+c.h+s(3), s(6), s(2), t.text)
				break
			}
		}
	}
	f.finish(x0, y0, x1, y1, radius)
	saveFrame(&render.Image{Pixels: canvas.Pixels, Width: canvas.Width, Height: canvas.Height})
}

// fill blends a rounded rectangle (radius 0 for square) of `colour` into the opaque panel.
func (f *flyout) fill(x0, y0, x1, y1, radius float64, colour [3]byte, opacity float64) {
	canvas := f.canvas
	for y := max(0, int(y0)); y < min(canvas.Height, int(math.Ceil(y1))); y++ {
		for x := max(0, int(x0)); x < min(canvas.Width, int(math.Ceil(x1))); x++ {
			coverage := roundedRectCoverage(float64(x)+0.5, float64(y)+0.5, x0, y0, x1, y1, radius) * opacity
			if coverage <= 0 {
				continue
			}
			i := (y*canvas.Width + x) * 4
			for c := 0; c < 3; c++ {
				canvas.Pixels[i+c] = byte(float64(canvas.Pixels[i+c])*(1-coverage) + float64(colour[2-c])*coverage + 0.5)
			}
		}
	}
}

// ring strokes a rounded rectangle's outline `width` thick.
func (f *flyout) ring(x0, y0, x1, y1, radius, width float64, colour [3]byte) {
	canvas := f.canvas
	for y := max(0, int(y0)); y < min(canvas.Height, int(math.Ceil(y1))); y++ {
		for x := max(0, int(x0)); x < min(canvas.Width, int(math.Ceil(x1))); x++ {
			px, py := float64(x)+0.5, float64(y)+0.5
			coverage := roundedRectCoverage(px, py, x0, y0, x1, y1, radius) - roundedRectCoverage(px, py, x0+width, y0+width, x1-width, y1-width, math.Max(0, radius-width))
			if coverage <= 0 {
				continue
			}
			i := (y*canvas.Width + x) * 4
			for c := 0; c < 3; c++ {
				canvas.Pixels[i+c] = byte(float64(canvas.Pixels[i+c])*(1-coverage) + float64(colour[2-c])*coverage + 0.5)
			}
		}
	}
}

// drawSwitch draws a Windows 11 toggle switch: an accent pill with the knob right when on, an outlined
// pill with the knob left when off; the knob grows a little under the pointer.
func (f *flyout) drawSwitch(x, y, w, h float64, on, hovered bool) {
	t := f.theme
	knob := h * 0.6
	if hovered {
		knob = h * 0.7
	}
	if on {
		f.fill(x, y, x+w, y+h, h/2, t.accent, 1)
		cx, cy := x+w-h/2, y+h/2
		f.fill(cx-knob/2, cy-knob/2, cx+knob/2, cy+knob/2, knob/2, t.onAccent, 1)
		return
	}
	f.ring(x, y, x+w, y+h, h/2, math.Max(1, f.scale), t.secondary)
	cx, cy := x+h/2, y+h/2
	f.fill(cx-knob/2, cy-knob/2, cx+knob/2, cy+knob/2, knob/2, t.secondary, 1)
}

// drawIcon blends a straight-alpha picture onto the panel at (x, y).
func (f *flyout) drawIcon(picture *image.NRGBA, x, y float64) {
	canvas := f.canvas
	left, top := int(math.Round(x)), int(math.Round(y))
	bounds := picture.Bounds()
	for py := 0; py < bounds.Dy(); py++ {
		for px := 0; px < bounds.Dx(); px++ {
			c := picture.NRGBAAt(px, py)
			cx, cy := left+px, top+py
			if c.A == 0 || cx < 0 || cy < 0 || cx >= canvas.Width || cy >= canvas.Height {
				continue
			}
			a := float64(c.A) / 255
			i := (cy*canvas.Width + cx) * 4
			for channel, value := range [3]byte{c.B, c.G, c.R} {
				canvas.Pixels[i+channel] = byte(float64(canvas.Pixels[i+channel])*(1-a) + float64(value)*a + 0.5)
			}
		}
	}
}

func colourNRGBA(c [3]byte, alpha byte) color.NRGBA {
	return color.NRGBA{R: c[0], G: c[1], B: c[2], A: alpha}
}

// finish works out the panel's anti-aliased edge and the drop shadow around it, and turns the canvas
// into premultiplied alpha for UpdateLayeredWindow. Text drawn by GDI is inside the panel, where the
// alpha is set to opaque here (GDI leaves it zero).
func (f *flyout) finish(x0, y0, x1, y1, radius float64) {
	canvas := f.canvas
	s := f.scale
	offset, sigma, depth := 6*s, 14*s, 0.22
	for y := 0; y < canvas.Height; y++ {
		for x := 0; x < canvas.Width; x++ {
			px, py := float64(x)+0.5, float64(y)+0.5
			coverage := roundedRectCoverage(px, py, x0, y0, x1, y1, radius)
			// A soft shadow below the panel (Windows 11's flyout depth), falling off with distance from it.
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

func roundedRectDistance(px, py, x0, y0, x1, y1, radius float64) float64 {
	cx, cy := (x0+x1)/2, (y0+y1)/2
	hx, hy := (x1-x0)/2-radius, (y1-y0)/2-radius
	qx, qy := math.Abs(px-cx)-hx, math.Abs(py-cy)-hy
	return math.Hypot(math.Max(qx, 0), math.Max(qy, 0)) + math.Min(math.Max(qx, qy), 0) - radius
}

func roundedRectCoverage(px, py, x0, y0, x1, y1, radius float64) float64 {
	return math.Max(0, math.Min(1, 0.5-roundedRectDistance(px, py, x0, y0, x1, y1, radius)))
}

/* ---------- text ---------- */

type alignment int

const (
	alignLeft alignment = iota
	alignCentre
	alignRight
)

// text draws one line with GDI (ClearType, over the opaque panel), vertically centred in a box `height`
// tall whose top is y, and anchored at x as `align` says.
func (f *flyout) text(line string, x, y, height, sizeDip float64, weight int, colour [3]byte, align alignment) {
	font := f.font(sizeDip, weight)
	previous, _, _ := procSelectObject.Call(f.canvas.dc, font)
	defer procSelectObject.Call(f.canvas.dc, previous)
	pointer := wide(line)
	length := uintptr(len([]rune(line)))
	var extent size
	procGetTextExtentPoint32.Call(f.canvas.dc, uintptr(unsafe.Pointer(pointer)), length, uintptr(unsafe.Pointer(&extent)))
	switch align {
	case alignCentre:
		x -= float64(extent.CX) / 2
	case alignRight:
		x -= float64(extent.CX)
	}
	top := y + (height-float64(extent.CY))/2
	procSetBkMode.Call(f.canvas.dc, transparentBkMode)
	procSetTextColor.Call(f.canvas.dc, uintptr(colour[0])|uintptr(colour[1])<<8|uintptr(colour[2])<<16)
	procTextOut.Call(f.canvas.dc, uintptr(int32(math.Round(x))), uintptr(int32(math.Round(top))), uintptr(unsafe.Pointer(pointer)), length)
	procGdiFlush.Call()
}

// font is Windows 11's UI face (Segoe UI Variable) where it exists, else Segoe UI.
func (f *flyout) font(sizeDip float64, weight int) uintptr {
	key := [2]int{int(math.Round(sizeDip * f.scale)), weight}
	if font, ok := f.fonts[key]; ok {
		return font
	}
	if f.face == "" {
		f.face = "Segoe UI"
		if fontExists("Segoe UI Variable Text") {
			f.face = "Segoe UI Variable Text"
		}
	}
	font, _, _ := procCreateFont.Call(uintptr(-int32(key[0])), 0, 0, 0, uintptr(weight), 0, 0, 0, defaultCharset, 0, 0, clearTypeQuality, 0, uintptr(unsafe.Pointer(wide(f.face))))
	f.fonts[key] = font
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

/* ---------- input ---------- */

func flyoutProcedure(window, msg, wParam, lParam uintptr) uintptr {
	f := theFlyout
	switch uint32(msg) {
	case wmTimer:
		if wParam == timerFlyout && f != nil {
			f.animate()
		}
		return 0
	case wmActivate:
		if f != nil && wParam&0xFFFF == waInactive {
			debugLog("flyout lost focus")
			f.close() // a click anywhere else
		}
		return 0
	case wmMouseMove:
		if f != nil {
			if !f.tracking {
				tracking := trackMouseEvent{Flags: tmeLeave, Window: window}
				tracking.Size = uint32(unsafe.Sizeof(tracking))
				procTrackMouseEvent.Call(uintptr(unsafe.Pointer(&tracking)))
				f.tracking = true
			}
			f.setHover(f.hit(lParam))
		}
		return 0
	case wmMouseLeave:
		if f != nil {
			f.tracking = false
			f.setHover("")
		}
		return 0
	case wmLButtonDown:
		if f != nil {
			f.pressed = f.hit(lParam)
			if f.pressed == "" && !f.insidePanel(lParam) {
				f.close() // the shadow around the panel
			}
			f.redraw()
		}
		return 0
	case wmLButtonUp:
		if f != nil {
			id := f.hit(lParam)
			pressed := f.pressed
			f.pressed = ""
			if id != "" && id == pressed {
				f.activate(id)
			} else {
				f.redraw()
			}
		}
		return 0
	case wmKeyDown:
		if f != nil {
			f.key(wParam)
		}
		return 0
	}
	result, _, _ := procDefWindowProc.Call(window, msg, wParam, lParam)
	return result
}

func clientPoint(lParam uintptr) (float64, float64) {
	return float64(int16(lParam & 0xFFFF)), float64(int16(lParam >> 16 & 0xFFFF))
}

func (f *flyout) hit(lParam uintptr) string {
	x, y := clientPoint(lParam)
	for _, c := range f.controls {
		if x >= c.x && x < c.x+c.w && y >= c.y && y < c.y+c.h {
			return c.id
		}
	}
	return ""
}

func (f *flyout) insidePanel(lParam uintptr) bool {
	x, y := clientPoint(lParam)
	x0, y0 := float64(f.px(flyoutShadow)), float64(f.px(flyoutShadow))
	return x >= x0 && y >= y0 && x < x0+float64(f.px(flyoutWidth)) && y < y0+float64(f.px(flyoutHeight))
}

func (f *flyout) setHover(id string) {
	if id != f.hover {
		f.hover = id
		f.redraw()
	}
}

func (f *flyout) redraw() {
	if f.canvas == nil || !f.shown {
		return
	}
	f.render()
	if !f.closing {
		f.present(1, 0)
	}
}

// activate runs a control's action, then draws the flyout again with the new settings.
func (f *flyout) activate(id string) {
	for _, c := range f.controls {
		if c.id == id {
			c.action()
			break
		}
	}
	if f.shown && f.canvas != nil {
		f.layout()
		f.redraw()
	}
}

// key handles the keyboard: Tab between controls, arrows within a picker, Enter or Space to act,
// Esc to close.
func (f *flyout) key(virtualKey uintptr) {
	const stops = 8
	switch virtualKey {
	case vkEscape:
		f.close()
		return
	case vkTab:
		step := 1
		if keyHeld(vkShift) {
			step = stops - 1
		}
		if f.focus < 0 {
			f.focus = 0
		} else {
			f.focus = (f.focus + step) % stops
		}
	case vkLeft, vkUp, vkRight, vkDown:
		if f.focus < 0 {
			f.focus = 1
			break
		}
		var group []control
		chosen := -1
		for _, c := range f.controls {
			if c.stop == f.focus && c.kind == kindSegment {
				if c.on {
					chosen = len(group)
				}
				group = append(group, c)
			}
		}
		if len(group) == 0 {
			f.focus = (f.focus + map[bool]int{true: stops - 1, false: 1}[virtualKey == vkLeft || virtualKey == vkUp]) % stops
			break
		}
		next := chosen + 1
		if virtualKey == vkLeft || virtualKey == vkUp {
			next = chosen - 1
		}
		if next >= 0 && next < len(group) {
			f.activate(group[next].id)
			return
		}
	case vkReturn, vkSpace:
		for _, c := range f.controls {
			if c.stop == f.focus && (c.kind != kindSegment || c.on) {
				f.activate(c.id)
				return
			}
		}
	default:
		return
	}
	f.redraw()
}
