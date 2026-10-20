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
	"math"
	"time"
	"unsafe"

	"circlelens/internal/icon"
	"circlelens/internal/motion"
	"circlelens/internal/render"
)

const (
	flyoutWidth  = 300 // DIPs, the panel without its shadow
	flyoutHeight = 555 // without the update switch; with it, flyoutRow more
	flyoutRow    = 28
	flyoutPad    = 12
	flyoutShadow = 24 // room around the panel for its shadow
	flyoutGap    = 12 // from the taskbar and the screen edge
	flyoutRadius = 14

	flyoutInMs  = 200
	flyoutOutMs = 120
	flyoutRise  = 8 // DIPs it slides up while appearing

	timerFlyout = 1
	timerNote   = 2 // a note about the shortcut has had its time: draw again without it
)

// flyoutTheme is the website's palette (brand_windows.go) as the panel uses it; the panel is always dark.
type flyoutTheme struct {
	panel, border, divider, track, hover, chip, chipBorder [3]byte
	text, secondary, tertiary                              [3]byte
	accent, accentHover, accentPressed, onAccent           [3]byte
	switchOff, knobOff                                     [3]byte
}

func newFlyoutTheme() flyoutTheme {
	return flyoutTheme{
		panel: brand.bg, border: mixColour(brand.bg, brand.white, lineStrong), divider: mixColour(brand.bg, brand.white, lineSoft),
		track: mixColour(brand.bg, brand.white, 0.04), hover: mixColour(brand.bg, brand.white, 0.06),
		chip: mixColour(brand.bg, brand.white, 0.1), chipBorder: mixColour(brand.bg, brand.white, 0.16),
		text: brand.text, secondary: brand.muted, tertiary: brand.dim,
		accent: brand.primary, accentHover: brand.primaryHover, accentPressed: brand.primaryPressed, onAccent: brand.onPrimary,
		switchOff: brand.switchOff, knobOff: brand.knobOff,
	}
}

type controlKind int

const (
	kindSwitch controlKind = iota
	kindButton
	kindSegment
	kindRow
	kindLink
	kindShortcut
	kindKey // one of the keys while the lens is open, as a keycap; click to record
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
	window uintptr
	painter
	layer     panelLayer // the background, edge and shadow, kept while it is open
	theme     flyoutTheme
	left, top int // window position on screen
	controls  []control
	labels    []rowLabel
	dividers  []float64
	hover     string
	pressed   string
	focus     int     // the focused tab stop, or -1 until the keyboard is used
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
	theFlyout = &flyout{window: window, focus: -1, painter: newPainter()}
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
	f.releaseFonts()
	f.layer = panelLayer{}
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

	// Header: the switch that turns Circle Lens on and off (off, nothing opens the lens).
	add(control{id: "watch", stop: next(), kind: kindSwitch, x: right - s(26), y: y + s(14), w: s(26), h: s(15), on: a.settings.Enabled,
		label: "Circle Lens on", action: func() { a.runCommand(commandEnabled) }})
	y += s(56) // the header: name and status, with room to breathe before the button
	// The main action: opens the lens, or closes it while it's open.
	label := "Open lens"
	if a.state.IsOpen() {
		label = "Close lens"
	}
	add(control{id: "open", stop: next(), kind: kindButton, x: left, y: y, w: right - left, h: s(28), label: label, detail: a.shortcut.String(),
		disabled: !a.settings.Enabled, action: func() {
			f.closeNow()
			if a.state.IsOpen() {
				a.close()
			} else {
				a.open()
			}
		}})
	y += s(28 + 16)
	f.dividers = append(f.dividers, y)
	y += s(10)

	// One line per setting: label on the left, chips on the right.
	chips := func(label, group string, labels []string, chosen, chipWidth int, choose func(int)) {
		f.labels = append(f.labels, rowLabel{label, y, s(28)})
		groupStop := next()
		width := s(float64(chipWidth))
		x := right - width*float64(len(labels)) - s(2)
		for i, text := range labels {
			i := i
			add(control{id: fmt.Sprintf("%s-%d", group, i), stop: groupStop, kind: kindSegment, x: x + float64(i)*width, y: y + s(4), w: width, h: s(20),
				label: text, on: i == chosen, action: func() { choose(i) }})
		}
		y += s(28)
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
	y += s(10)
	f.dividers = append(f.dividers, y)
	y += s(10)

	// The shortcut: the keys in a chip; click to record new ones.
	add(control{id: "shortcut", stop: next(), kind: kindShortcut, x: left - s(4), y: y, w: right - left + s(8), h: s(46), on: a.recording && a.recordingFor == "",
		label: a.shortcut.String(), action: func() {
			if a.recording && a.recordingFor == "" {
				a.cancelRecording()
			} else {
				a.startRecording()
			}
		}})
	y += s(46)

	// The keys while the lens is open, as keycaps on their rows; click one to record another.
	keyRow := func(label string, keys [][2]string) {
		f.labels = append(f.labels, rowLabel{label, y, s(28)})
		x := right
		for i := len(keys) - 1; i >= 0; i-- {
			target, text := keys[i][0], keys[i][1]
			recordingThis := a.recording && a.recordingFor == target
			if recordingThis {
				text = "Press keys…"
			}
			width := f.measure(text, styleKey) + s(16)
			x -= width
			add(control{id: "key-" + target, stop: next(), kind: kindKey, x: x, y: y + s(4), w: width, h: s(20), label: text, on: recordingThis,
				action: func() {
					if a.recording && a.recordingFor == target {
						a.cancelRecording()
					} else {
						a.startRecordingFor(target)
					}
				}})
			x -= s(6)
		}
		y += s(28)
	}
	closeKey := a.settings.CloseKey
	if closeKey == "" {
		closeKey = "Off"
	}
	onOffKey := a.settings.OnOffKey
	if onOffKey == "" {
		onOffKey = "None"
	}
	keyRow("On/off key", [][2]string{{"onOff", onOffKey}})
	keyRow("Zoom keys", [][2]string{{"zoomIn", a.settings.ZoomInKeys}, {"zoomOut", a.settings.ZoomOutKeys}})
	keyRow("Close key", [][2]string{{"close", closeKey}})

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
		add(control{id: toggle.id, stop: next(), kind: kindRow, x: left - s(4), y: y, w: right - left + s(8), h: s(28), on: toggle.on,
			label: toggle.label, action: func() { a.runCommand(command) }})
		y += s(28)
	}

	// Links: the guided tour, the release notes, the website.
	y += s(6)
	x := left - s(4)
	for _, link := range []struct{ id, label, command string }{
		{"guide", "How to use", "guide"}, {"news", "What's new", "news"}, {"site", "Website ↗", "site"},
	} {
		command := link.command
		width := f.measure(link.label, textStyle{face: faceText, size: 11.5, weight: fwNormal}) + s(16)
		add(control{id: link.id, stop: next(), kind: kindLink, x: x, y: y, w: width, h: s(22), label: link.label, detail: "small",
			action: func() {
				f.closeNow()
				a.runCommand(map[string]int{"guide": commandGuide, "news": commandNews, "site": commandWebsite}[command])
			}})
		x += width + s(2)
	}
	y += s(24)
	y += s(10)
	f.dividers = append(f.dividers, y)
	y += s(10)
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

	// The panel, opaque for now (its edge and shadow are worked out last): the website's near-black, lit
	// from above, with a hairline edge.
	f.layer.begin(&f.painter, x0, y0, x1, y1, radius, s(12), s(14), 0.28)

	// Header: the mark, the name, and what it is doing as a small label.
	mark := icon.Style{Ink: colourNRGBA(t.text, 255), Paused: !app.settings.Enabled}
	f.drawIcon(icon.Draw(f.px(22), mark), left, y0+s(flyoutPad+10))
	f.write("Circle Lens", left+s(31), y0+s(flyoutPad+4), s(20), textStyle{face: faceDisplay, size: 14, weight: 700, tracking: -0.01}, t.text, alignLeft)
	circles := map[float64]string{1: "1 CIRCLE", 1.5: "1½ CIRCLES", 2: "2 CIRCLES", 2.5: "2½ CIRCLES", 3: "3 CIRCLES"}[app.settings.Loops]
	if app.settings.Enabled {
		f.eyebrow("WATCHING · "+circles, left+s(31), y0+s(flyoutPad+24), s(15), brand.live, brand.muted, alignLeft)
	} else {
		f.eyebrow("PAUSED", left+s(31), y0+s(flyoutPad+24), s(15), brand.switchOff, brand.dim, alignLeft)
	}

	for _, label := range f.labels {
		f.text(label.text, left, label.y, label.h, 12, fwNormal, t.secondary, alignLeft)
	}
	for _, y := range f.dividers {
		f.fill(x0+s(1), y, x1-s(1), y+math.Max(1, math.Round(f.scale)), 0, brand.white, lineSoft)
	}
	f.text("v"+version, left, f.footerY+s(2), s(22), 11, fwNormal, t.tertiary, alignLeft)

	for _, c := range f.controls {
		hovered, pressed := c.id == f.hover, c.id == f.pressed && c.id == f.hover
		switch c.kind {
		case kindSwitch:
			f.brandSwitch(c.x, c.y, c.w, c.h, c.on, hovered)
		case kindButton:
			// The main action, as the website's: solid near-white with dark text and 10 px corners, the
			// shortcut in a dark key at its end.
			// While Circle Lens is off it can't be pressed: a quiet outline with dim text.
			ink, keysInk := brand.onPrimary, brand.primary
			if c.disabled {
				f.ringOpacity(c.x, c.y, c.x+c.w, c.y+c.h, s(radiusButton), math.Max(1, math.Round(f.scale)), brand.white, lineStrong)
				ink, keysInk = brand.dim, brand.dim
			} else {
				f.primaryButton(c.x, c.y, c.x+c.w, c.y+c.h, "", hovered, pressed)
			}
			f.write(c.label, c.x+s(12), c.y, c.h, textStyle{face: faceText, size: 12.5, weight: fwSemibold}, ink, alignLeft)
			keysWidth := f.measure(c.detail, textStyle{face: faceMono, size: 11, weight: fwMedium}) + s(12)
			chip := [4]float64{c.x + c.w - s(4) - keysWidth, c.y + s(4), c.x + c.w - s(4), c.y + c.h - s(4)}
			f.fill(chip[0], chip[1], chip[2], chip[3], s(radiusChip), brand.track, 1)
			f.write(c.detail, (chip[0]+chip[2])/2, chip[1]-s(0.5), chip[3]-chip[1], textStyle{face: faceMono, size: 11, weight: fwMedium}, keysInk, alignCentre)
		case kindSegment:
			label := t.secondary
			switch {
			case c.on:
				f.fill(c.x, c.y, c.x+c.w, c.y+c.h, s(radiusChip), brand.chipOn, 1)
				label = brand.white
			case pressed:
				f.fill(c.x, c.y, c.x+c.w, c.y+c.h, s(radiusChip), brand.chipOn, 1)
				label = t.text
			case hovered:
				f.fill(c.x, c.y, c.x+c.w, c.y+c.h, s(6), brand.white, 0.06)
				label = t.text
			}
			f.text(c.label, c.x+c.w/2, c.y, c.h, 11.5, fwMedium, label, alignCentre)
		case kindRow:
			colour := t.secondary
			if hovered {
				f.fill(c.x, c.y, c.x+c.w, c.y+c.h, s(7), brand.white, 0.05)
				colour = t.text
			}
			f.text(c.label, c.x+s(4), c.y, c.h, 12, fwNormal, colour, alignLeft)
			f.brandSwitch(c.x+c.w-s(4)-s(26), c.y+(c.h-s(15))/2, s(26), s(15), c.on, hovered)
		case kindShortcut:
			f.drawShortcutRow(c, hovered)
		case kindKey:
			f.drawKeycap(c, hovered)
		case kindLink:
			colour, size := t.secondary, 12.0
			if c.detail == "update" || c.detail == "small" {
				size = 11.5 // the update link sits beside the version, a little smaller
			}
			switch {
			case c.disabled:
				colour = t.tertiary
			case hovered:
				f.fill(c.x, c.y, c.x+c.w, c.y+c.h, c.h/2, brand.white, 0.06)
				colour = t.text
				if c.on {
					colour = brand.white
				}
			case c.on:
				colour = brand.text
			}
			f.text(c.label, c.x+c.w/2, c.y, c.h, size, fwNormal, colour, alignCentre)
		}
	}

	// The keyboard focus ring: around the focused control (a picker's chosen chip).
	if f.focus >= 0 {
		for _, c := range f.controls {
			if c.stop == f.focus && (c.kind != kindSegment || c.on) {
				radius := s(7)
				switch c.kind {
				case kindButton:
					radius = s(radiusButton)
				case kindSwitch:
					radius = c.h / 2
				}
				f.focusRing(c.x, c.y, c.x+c.w, c.y+c.h, radius)
				break
			}
		}
	}
	f.layer.end(&f.painter)
	saveFrame(&render.Image{Pixels: canvas.Pixels, Width: canvas.Width, Height: canvas.Height})
}

/* ---------- text ---------- */

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
	f.text("Shortcut", c.x+s(4), c.y+s(4), s(22), 12, fwNormal, t.secondary, alignLeft)

	keys := c.label
	if c.on {
		keys = "Press keys…"
	}
	width := f.textWidth(keys, 11) + s(14)
	if !c.on {
		width = f.measure(keys, styleKey) + s(16) // drawn as a keycap, in the monospace face
	}
	right := c.x + c.w - s(4)
	chip := [4]float64{right - width, c.y + s(5), right, c.y + s(25)}
	if c.on {
		// Recording: the keys chip becomes a small blue pill.
		f.fill(chip[0], chip[1], chip[2], chip[3], s(radiusChip), brand.primary, 1)
		f.text(keys, (chip[0]+chip[2])/2, chip[1], chip[3]-chip[1], 11, fwSemibold, brand.onPrimary, alignCentre)
	} else {
		// The shortcut as a keycap, as the website draws keys.
		f.fill(chip[0], chip[1], chip[2], chip[3], s(radiusChip), brand.white, 0.04)
		f.ringOpacity(chip[0], chip[1], chip[2], chip[3], s(radiusChip), math.Max(1, math.Round(f.scale)), brand.white, lineStrong)
		f.write(keys, (chip[0]+chip[2])/2, chip[1]-s(0.5), chip[3]-chip[1], styleKey, t.text, alignCentre)
	}

	hint, colour := "Opens and closes the lens · click to record your own", t.tertiary
	switch {
	case c.on:
		hint, colour = "Press keys, or tap Ctrl, Shift or Alt twice · Esc cancels", t.secondary
	case app.recording && app.recordingFor == "onOff":
		hint, colour = "Press keys to turn it on and off · Backspace for none", t.secondary
	case app.recording && app.recordingFor == "close":
		hint, colour = "Press a key to close it · Esc for Esc · Backspace for none", t.secondary
	case app.recording:
		hint, colour = "Press keys to "+map[string]string{"zoomIn": "zoom in", "zoomOut": "zoom out"}[app.recordingFor]+" · Esc cancels", t.secondary
	case app.currentShortcutNote() != "":
		hint, colour = app.currentShortcutNote(), t.secondary
	}
	f.text(hint, c.x+s(4), c.y+s(26), s(16), 11, fwNormal, colour, alignLeft)
}

// drawKeycap draws one of the keys while the lens is open: a keycap, or a blue pill while recording.
func (f *flyout) drawKeycap(c control, hovered bool) {
	s := func(dip float64) float64 { return dip * f.scale }
	line := math.Max(1, math.Round(f.scale))
	if c.on {
		f.fill(c.x, c.y, c.x+c.w, c.y+c.h, s(radiusChip), brand.primary, 1)
		f.text(c.label, c.x+c.w/2, c.y, c.h, 11, fwSemibold, brand.onPrimary, alignCentre)
		return
	}
	fill, ring := 0.04, lineStrong
	if hovered {
		fill, ring = 0.07, 0.22
	}
	f.fill(c.x, c.y, c.x+c.w, c.y+c.h, s(radiusChip), brand.white, fill)
	f.ringOpacity(c.x, c.y, c.x+c.w, c.y+c.h, s(radiusChip), line, brand.white, ring)
	colour := brand.text
	if c.label == "Off" || c.label == "None" {
		colour = brand.dim
	}
	f.write(c.label, c.x+c.w/2, c.y-s(0.5), c.h, styleKey, colour, alignCentre)
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
