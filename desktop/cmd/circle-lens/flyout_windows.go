//go:build windows

package main

// The flyout is what a left click on the tray icon opens: a compact settings panel above the taskbar,
// dark in the manner of Linear (near-black, hairline borders, muted grey labels, indigo for the one
// action and the switches). One line per setting, label on the left, small chips or a switch on the
// right; the main action on top. It slides up as it fades in, and works from the keyboard (Tab,
// arrows, Enter or Space, Esc). A right click still opens the native menu, which screen readers can read.
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
	flyoutWidth  = 300 // DIPs, the panel without its shadow
	flyoutHeight = 425 // without the update switch; with it, flyoutRow more
	flyoutRow    = 30
	flyoutPad    = 12
	flyoutShadow = 24 // room around the panel for its shadow
	flyoutGap    = 12 // from the taskbar and the screen edge
	flyoutRadius = 10

	flyoutInMs  = 200
	flyoutOutMs = 120
	flyoutRise  = 8 // DIPs it slides up while appearing

	timerFlyout = 1
	timerNote   = 2 // a note about the shortcut has had its time: draw again without it
)

// flyoutTheme is a Linear-like dark palette (the panel is always dark).
type flyoutTheme struct {
	panel, border, divider, track, hover, chip, chipBorder [3]byte
	text, secondary, tertiary                              [3]byte
	accent, accentHover, accentPressed, onAccent           [3]byte
	switchOff, knobOff                                     [3]byte
}

func newFlyoutTheme() flyoutTheme {
	return flyoutTheme{
		panel: rgb(0x17181a), border: rgb(0x2b2c30), divider: rgb(0x232427),
		track: rgb(0x1f2023), hover: rgb(0x26272b), chip: rgb(0x313237), chipBorder: rgb(0x3d3e44),
		text: rgb(0xececee), secondary: rgb(0x9b9ca3), tertiary: rgb(0x6b6d75),
		accent: rgb(0x5e6ad2), accentHover: rgb(0x6b77de), accentPressed: rgb(0x5560c4), onAccent: rgb(0xffffff),
		switchOff: rgb(0x2e2f34), knobOff: rgb(0xa4a6ad),
	}
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
	kindShortcut
)

// rowLabel is a setting's name, drawn at the left of its line.
type rowLabel struct {
	text string
	y, h float64
}

// control is one clickable thing, laid out in physical pixels within the window.
type control struct {
	id         string
	stop       int // tab stop; a segmented picker's segments share one
	kind       controlKind
	x, y, w, h float64
	label      string
	detail     string // the button's shortcut
	on         bool   // a switch that is on, or the chosen segment; a link that stands out
	disabled   bool   // shown, but not clickable (a link while it is busy)
	action     func()
}

type flyout struct {
	window    uintptr
	canvas    *dib
	scale     float64
	theme     flyoutTheme
	left, top int // window position on screen
	controls  []control
	labels    []rowLabel
	dividers  []float64
	hover     string
	pressed   string
	focus     int // the focused tab stop, or -1 until the keyboard is used
	fonts     map[[2]int]uintptr
	face      string
	height    float64 // the panel's height in DIPs
	footerY   float64 // the top of the last line (version, update, Quit), in window pixels

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
	f.theme = newFlyoutTheme()
	f.hover, f.pressed, f.focus = "", "", -1

	// Place it above the tray icon, inside the work area of the taskbar's monitor.
	anchor := f.trayIconRect()
	f.scale = monitorScale(point{(anchor.Left + anchor.Right) / 2, (anchor.Top + anchor.Bottom) / 2})
	work := workArea(point{(anchor.Left + anchor.Right) / 2, (anchor.Top + anchor.Bottom) / 2})
	f.height = flyoutHeight
	if updatesSupported() {
		f.height += flyoutRow
	}
	width, height := f.px(flyoutWidth+2*flyoutShadow), f.px(f.height+2*flyoutShadow)
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
	app.cancelRecording() // closing it (Esc, a click elsewhere) while recording keeps the old shortcut
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
	left, right := x0+s(flyoutPad), x0+s(flyoutWidth-flyoutPad)
	a := app
	f.controls, f.labels, f.dividers = nil, nil, nil
	add := func(c control) { f.controls = append(f.controls, c) }
	y := y0 + s(flyoutPad)
	stop := 0
	next := func() int { stop++; return stop - 1 }

	// Header: the switch that watches for circles.
	add(control{id: "watch", stop: next(), kind: kindSwitch, x: right - s(28), y: y + s(10), w: s(28), h: s(16), on: a.settings.Enabled,
		label: "Watch for circles", action: func() { a.runCommand(commandEnabled) }})
	y += s(40)
	add(control{id: "open", stop: next(), kind: kindButton, x: left, y: y, w: right - left, h: s(30), label: "Open lens", detail: a.shortcut.String(),
		action: func() { f.closeNow(); a.open() }})
	y += s(40)
	f.dividers = append(f.dividers, y)
	y += s(8)

	// One line per setting: label on the left, chips on the right.
	chips := func(label, group string, labels []string, chosen, chipWidth int, choose func(int)) {
		f.labels = append(f.labels, rowLabel{label, y, s(30)})
		groupStop := next()
		width := s(float64(chipWidth))
		x := right - width*float64(len(labels)) - s(2)
		for i, text := range labels {
			i := i
			add(control{id: fmt.Sprintf("%s-%d", group, i), stop: groupStop, kind: kindSegment, x: x + float64(i)*width, y: y + s(5), w: width, h: s(20),
				label: text, on: i == chosen, action: func() { choose(i) }})
		}
		y += s(30)
	}
	loops, chosenLoops := make([]string, len(loopChoices)), 0
	for i, choice := range loopChoices {
		loops[i] = map[float64]string{1: "1", 1.5: "1½", 2: "2", 2.5: "2½", 3: "3"}[choice.loops]
		if choice.loops == a.settings.Loops {
			chosenLoops = i
		}
	}
	chips("Circles", "loops", loops, chosenLoops, 30, func(i int) { a.runCommand(commandLoops + i) })
	zooms, chosenZoom := make([]string, len(zoomChoices)), 0
	for i, zoom := range zoomChoices {
		zooms[i] = trimZoom(zoom) + "×"
		if zoom == a.settings.Zoom {
			chosenZoom = i
		}
	}
	chips("Zoom", "zoom", zooms, chosenZoom, 34, func(i int) { a.runCommand(commandZoom + i) })
	chosenSize := 0
	for i, choice := range sizeChoices {
		if choice.radius == a.settings.Radius {
			chosenSize = i
		}
	}
	chips("Size", "size", []string{"S", "M", "L", "XL"}, chosenSize, 34, func(i int) { a.runCommand(commandSize + i) })
	gaps, chosenGap := make([]string, len(gapChoices)), 0
	for i, gap := range gapChoices {
		gaps[i] = trimZoom(gap) + "s"
		if gap == a.settings.Gap {
			chosenGap = i
		}
	}
	chips("Wait", "gap", gaps, chosenGap, 38, func(i int) { a.runCommand(commandGap + i) })
	y += s(8)
	f.dividers = append(f.dividers, y)
	y += s(8)

	// The shortcut: the keys in a chip; click to record new ones.
	add(control{id: "shortcut", stop: next(), kind: kindShortcut, x: left - s(4), y: y, w: right - left + s(8), h: s(46), on: a.recording,
		label: a.shortcut.String(), action: func() {
			if a.recording {
				a.cancelRecording()
			} else {
				a.startRecording()
			}
		}})
	y += s(46)

	toggles := []struct {
		id, label string
		on        bool
		command   int
	}{
		{"fast", "Zoom out when moving fast", a.settings.ZoomOutWhenFast, commandFast},
		{"rim", "Convex rim", a.settings.ConvexRim, commandRim},
		{"startup", "Start with Windows", startsWithWindows(), commandStartup},
	}
	if updatesSupported() {
		toggles = append(toggles, struct {
			id, label string
			on        bool
			command   int
		}{"autoupdate", "Update automatically", a.settings.AutoUpdate, commandAutoUpdate})
	}
	for _, toggle := range toggles {
		command := toggle.command
		add(control{id: toggle.id, stop: next(), kind: kindRow, x: left - s(4), y: y, w: right - left + s(8), h: s(30), on: toggle.on,
			label: toggle.label, action: func() { a.runCommand(command) }})
		y += s(30)
	}
	y += s(8)
	f.dividers = append(f.dividers, y)
	y += s(8)
	// The last line: the version, then what updating is doing (or a link to check), then Quit.
	f.footerY = y
	if updatesSupported() {
		label, clickable := a.updateLabel()
		x := left + f.textWidth("v"+version, 11) + s(6)
		width := f.textWidth(label, 11.5) + s(14)
		add(control{id: "update", stop: next(), kind: kindLink, x: x, y: y + s(2), w: width, h: s(22), label: label, detail: "update",
			on: a.updates.status == updateAvailable || a.updates.status == updateReady, disabled: !clickable,
			action: func() { a.runCommand(commandUpdate) }})
	}
	add(control{id: "quit", stop: next(), kind: kindLink, x: right - s(48), y: y + s(2), w: s(48), h: s(22), label: "Quit",
		action: func() { f.closeNow(); a.runCommand(commandQuit) }})
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
	x1, y1 := x0+s(flyoutWidth), y0+s(f.height)
	left := x0 + s(flyoutPad)
	radius := s(flyoutRadius)

	// The panel, opaque for now (its edge and shadow are worked out last), with a hairline border.
	clear(canvas.Pixels)
	f.fill(x0, y0, x1, y1, radius, t.border, 1)
	f.fill(x0+s(1), y0+s(1), x1-s(1), y1-s(1), radius-s(1), t.panel, 1)

	// Header: the mark, the name, what it is doing.
	mark := icon.Style{Ink: colourNRGBA(t.text, 255)}
	if app.settings.Enabled {
		mark.Glass = colourNRGBA(t.accent, 255)
		mark.GlassInk = colourNRGBA(t.onAccent, 255)
	} else {
		mark.Paused = true
	}
	f.drawIcon(icon.Draw(f.px(20), mark), left, y0+s(flyoutPad+8))
	f.text("Circle Lens", left+s(28), y0+s(flyoutPad+1), s(18), 13, fwSemibold, t.text, alignLeft)
	status := "Draw " + map[float64]string{1: "one circle", 1.5: "1½ circles", 2: "two circles", 2.5: "2½ circles", 3: "three circles"}[app.settings.Loops] + " to magnify"
	if !app.settings.Enabled {
		status = "Paused · " + app.shortcut.String() + " still opens it"
	}
	f.text(status, left+s(28), y0+s(flyoutPad+19), s(15), 11, fwNormal, t.tertiary, alignLeft)

	for _, label := range f.labels {
		f.text(label.text, left, label.y, label.h, 12.5, fwNormal, t.secondary, alignLeft)
	}
	for _, y := range f.dividers {
		f.fill(x0+s(1), y, x1-s(1), y+math.Max(1, math.Round(f.scale)), 0, t.divider, 1)
	}
	f.text("v"+version, left, f.footerY+s(2), s(22), 11, fwNormal, t.tertiary, alignLeft)

	// Each chip group sits in a shared track.
	tracks := map[int][4]float64{}
	for _, c := range f.controls {
		if c.kind != kindSegment {
			continue
		}
		track, ok := tracks[c.stop]
		if !ok {
			track = [4]float64{c.x - s(2), c.y - s(2), 0, c.y + c.h + s(2)}
		}
		track[2] = c.x + c.w + s(2)
		tracks[c.stop] = track
	}
	for _, track := range tracks {
		f.fill(track[0], track[1], track[2], track[3], s(6), t.track, 1)
	}

	for _, c := range f.controls {
		hovered, pressed := c.id == f.hover, c.id == f.pressed && c.id == f.hover
		switch c.kind {
		case kindSwitch:
			f.drawSwitch(c.x, c.y, c.w, c.h, c.on, hovered)
		case kindButton:
			fill := t.accent
			if pressed {
				fill = t.accentPressed
			} else if hovered {
				fill = t.accentHover
			}
			f.fill(c.x, c.y, c.x+c.w, c.y+c.h, s(6), fill, 1)
			f.text(c.label, c.x+s(10), c.y, c.h, 13, fwMedium, t.onAccent, alignLeft)
			keysWidth := f.textWidth(c.detail, 11) + s(12)
			chip := [4]float64{c.x + c.w - s(5) - keysWidth, c.y + s(6), c.x + c.w - s(5), c.y + c.h - s(6)}
			f.fill(chip[0], chip[1], chip[2], chip[3], s(4), mixColour(fill, t.onAccent, 0.16), 1)
			f.text(c.detail, (chip[0]+chip[2])/2, chip[1], chip[3]-chip[1], 11, fwSemibold, mixColour(t.onAccent, fill, 0.12), alignCentre)
		case kindSegment:
			label := t.secondary
			switch {
			case c.on:
				f.fill(c.x, c.y, c.x+c.w, c.y+c.h, s(4), t.chipBorder, 1)
				f.fill(c.x+s(1), c.y+s(1), c.x+c.w-s(1), c.y+c.h-s(1), s(3), t.chip, 1)
				label = t.text
			case pressed:
				f.fill(c.x, c.y, c.x+c.w, c.y+c.h, s(4), t.chip, 1)
				label = t.text
			case hovered:
				f.fill(c.x, c.y, c.x+c.w, c.y+c.h, s(4), t.hover, 1)
				label = t.text
			}
			f.text(c.label, c.x+c.w/2, c.y, c.h, 12, fwMedium, label, alignCentre)
		case kindRow:
			if hovered {
				f.fill(c.x, c.y, c.x+c.w, c.y+c.h, s(5), t.hover, 1)
			}
			f.text(c.label, c.x+s(4), c.y, c.h, 12.5, fwNormal, t.secondary, alignLeft)
			f.drawSwitch(c.x+c.w-s(4)-s(28), c.y+(c.h-s(16))/2, s(28), s(16), c.on, hovered)
		case kindShortcut:
			f.drawShortcutRow(c, hovered)
		case kindLink:
			colour, size := t.secondary, 12.0
			if c.detail == "update" {
				size = 11.5 // the update link sits beside the version, a little smaller
			}
			switch {
			case c.disabled:
				colour = t.tertiary
			case hovered:
				f.fill(c.x, c.y, c.x+c.w, c.y+c.h, s(5), t.hover, 1)
				colour = t.text
				if c.on {
					colour = t.accentHover
				}
			case c.on:
				colour = t.accentHover
			}
			f.text(c.label, c.x+c.w/2, c.y, c.h, size, fwNormal, colour, alignCentre)
		}
	}

	// The keyboard focus ring: around the focused control (a picker's chosen chip).
	if f.focus >= 0 {
		for _, c := range f.controls {
			if c.stop == f.focus && (c.kind != kindSegment || c.on) {
				f.ring(c.x-s(2), c.y-s(2), c.x+c.w+s(2), c.y+c.h+s(2), s(6), math.Max(1.5, 1.5*f.scale), t.accent)
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

// drawSwitch draws a small switch: indigo with the knob right when on, a grey track with the knob left
// when off; the knob grows a little under the pointer.
func (f *flyout) drawSwitch(x, y, w, h float64, on, hovered bool) {
	t := f.theme
	knob := h - 6*f.scale
	if hovered {
		knob += f.scale
	}
	track, knobColour, cx := t.switchOff, t.knobOff, x+h/2
	if on {
		track, knobColour, cx = t.accent, t.onAccent, x+w-h/2
	}
	f.fill(x, y, x+w, y+h, h/2, track, 1)
	cy := y + h/2
	f.fill(cx-knob/2, cy-knob/2, cx+knob/2, cy+knob/2, knob/2, knobColour, 1)
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
		switch {
		case f == nil:
		case wParam == timerFlyout:
			f.animate()
		case wParam == timerNote:
			procKillTimer.Call(window, timerNote)
			f.refresh()
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
	return x >= x0 && y >= y0 && x < x0+float64(f.px(flyoutWidth)) && y < y0+float64(f.px(f.height))
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
			if !c.disabled {
				c.action()
			}
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
	stops := 0
	for _, c := range f.controls {
		stops = max(stops, c.stop+1)
	}
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

// drawShortcutRow draws the shortcut: its name and the keys in a chip on the first line, and below what
// is going on (how to change it, that it is recording, or a note about the last change).
func (f *flyout) drawShortcutRow(c control, hovered bool) {
	t := f.theme
	s := func(dip float64) float64 { return dip * f.scale }
	if hovered || c.on {
		f.fill(c.x, c.y, c.x+c.w, c.y+c.h, s(5), t.hover, 1)
	}
	f.text("Shortcut", c.x+s(4), c.y+s(4), s(22), 12.5, fwNormal, t.secondary, alignLeft)

	keys := c.label
	if c.on {
		keys = "Press keys…"
	}
	width := f.textWidth(keys, 11) + s(14)
	right := c.x + c.w - s(4)
	chip := [4]float64{right - width, c.y + s(5), right, c.y + s(25)}
	if c.on {
		f.fill(chip[0], chip[1], chip[2], chip[3], s(4), t.accent, 1)
		f.text(keys, (chip[0]+chip[2])/2, chip[1], chip[3]-chip[1], 11, fwSemibold, t.onAccent, alignCentre)
	} else {
		f.fill(chip[0], chip[1], chip[2], chip[3], s(4), t.chipBorder, 1)
		f.fill(chip[0]+s(1), chip[1]+s(1), chip[2]-s(1), chip[3]-s(1), s(3), t.chip, 1)
		f.text(keys, (chip[0]+chip[2])/2, chip[1], chip[3]-chip[1], 11, fwSemibold, t.text, alignCentre)
	}

	hint, colour := "Opens and closes the lens · click to record your own", t.tertiary
	switch {
	case c.on:
		hint, colour = "Press a combination, or tap Ctrl, Shift or Alt twice · Esc cancels", t.secondary
	case app.currentShortcutNote() != "":
		hint, colour = app.currentShortcutNote(), t.secondary
	}
	f.text(hint, c.x+s(4), c.y+s(26), s(16), 11, fwNormal, colour, alignLeft)
}

// textWidth measures a line at sizeDip in the flyout's font.
func (f *flyout) textWidth(line string, sizeDip float64) float64 {
	font := f.font(sizeDip, fwSemibold)
	previous, _, _ := procSelectObject.Call(f.canvas.dc, font)
	defer procSelectObject.Call(f.canvas.dc, previous)
	var extent size
	procGetTextExtentPoint32.Call(f.canvas.dc, uintptr(unsafe.Pointer(wide(line))), uintptr(len([]rune(line))), uintptr(unsafe.Pointer(&extent)))
	return float64(extent.CX)
}

// refresh lays out and draws the flyout again with the current settings, if it is open.
func (f *flyout) refresh() {
	if f.shown && !f.closing && f.canvas != nil {
		f.layout()
		f.redraw()
	}
}

// showNoteTimer redraws the flyout when the current shortcut note has had its time.
func (f *flyout) showNoteTimer() {
	if f.shown {
		procSetTimer.Call(f.window, timerNote, 4200, 0)
	}
}
