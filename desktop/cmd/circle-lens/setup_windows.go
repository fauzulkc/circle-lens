//go:build windows

package main

// The setup window: what a downloaded copy shows to install or update, and what Settings → Apps shows
// to uninstall. It has the website's look (brand_windows.go) and moves through a few screens:
//
//	install or update → installing (a ring fills around the lens as each step lands) → ready
//	                                                                                  ↘ failed
//	uninstall → removing → removed
//
// It works from the keyboard (Tab, Enter or Space, Esc) and can be dragged anywhere. With a screen
// reader running, Windows' own dialogs are used instead (install_windows.go), since this one is drawn
// by hand. CIRCLE_LENS_PREVIEW draws a screen to CIRCLE_LENS_FRAMES without showing or installing
// anything (test builds only).

import (
	"errors"
	"math"
	"os"
	"strings"
	"sync/atomic"
	"time"
	"unsafe"

	"circlelens/internal/lens"
	"circlelens/internal/render"
	"circlelens/internal/settings"
	"circlelens/internal/shortcut"
)

const (
	setupWidth  = 460.0 // DIPs, the panel without its shadow
	setupHeight = 540.0
	setupMargin = 48.0 // room around the panel for its shadow
	setupRadius = 20.0

	setupInMs   = 260
	setupFadeMs = 200 // between screens
	setupStepMs = 230 // between lines of the checklist
	setupMinMs  = 1100

	timerSetup         = 30
	messageSetupStep   = wmApp + 20
	messageSetupDone   = wmApp + 21
	wmNcHitTest        = 0x0084
	wmSetCursor        = 0x0020
	wmClose            = 0x0010
	htClient           = 1
	htCaption          = 2
	wsExAppWindow      = 0x00040000
	idcHand            = 32649
	setupStepReadyWait = 300 // after the last line, before the ready screen
)

var (
	procLoadIcon      = user32.NewProc("LoadIconW")
	procSetCursor     = user32.NewProc("SetCursor")
	procDestroyWindow = user32.NewProc("DestroyWindow")
	procGetWindowRect = user32.NewProc("GetWindowRect")
)

type setupScreen int

const (
	screenInstall setupScreen = iota
	screenUpdate
	screenWorking
	screenReady
	screenFailed
	screenRemove
	screenRemoving
	screenRemoved
	screenGuide // the tour (guide_windows.go)
	screenNews  // the release notes
)

type setupKind int

const (
	setupPrimary setupKind = iota
	setupGhost
	setupDanger
	setupToggle
	setupClose
)

type setupControl struct {
	id             string
	kind           setupKind
	x0, y0, x1, y1 float64
	label          string
	on             bool
	action         func()
}

type setupWindow struct {
	painter
	window    uintptr
	left, top int
	screen    setupScreen
	from      string // the installed version, when updating
	removing  bool   // this window uninstalls
	startup   bool
	updates   bool
	problem   string

	steps      []string // checklist lines shown so far
	pending    []string // lines the work reported, still to show
	workDone   bool
	workErr    error
	workFrom   time.Time
	lastStep   time.Time
	controls   []setupControl
	hover      string
	pressed    string
	focus      int
	tracking   bool
	opened     time.Time
	screenFrom time.Time
	before     []byte // the previous screen, faded out over the new one

	layer      panelLayer // the background, edge and shadow, drawn once
	base       []byte     // the current screen, finished, without what moves
	dirty      bool       // base needs drawing again
	ticking    bool
	finished   bool
	result     func() bool // what happens once the window has gone; true: this copy stops
	remember   func()      // remembers "just run it" for a copy that isn't installed
	stepSource chan string // checklist lines from the work, as they happen
	page       int         // the tour's page
	scroll     float64     // how far the release notes are scrolled, in pixels
	newsHeight float64
	quit       bool // WM_QUIT arrived while this window ran its own loop: pass it on
}

// setupWorkResult carries the work's outcome (an errorBox) to the UI thread.
var setupWorkResult atomic.Value

var theSetup *setupWindow

/* ---------- starting it ---------- */

// runSetupWindow offers to install (or update `installed`) and does it. It returns true when this copy
// should stop: installed, cancelled, or the installed copy started instead.
func runSetupWindow(installed string, rememberDeclined func()) bool {
	w := newSetupWindow()
	w.from, w.remember = installed, rememberDeclined
	w.screen = screenInstall
	if installed != "" {
		w.screen = screenUpdate
	}
	return w.run()
}

// runRemoveWindow asks to uninstall and does it.
func runRemoveWindow() {
	w := newSetupWindow()
	w.removing, w.screen = true, screenRemove
	w.run()
}

func newSetupWindow() *setupWindow {
	return &setupWindow{painter: newPainter(), startup: true, updates: true, focus: -1, result: func() bool { return true }}
}

func (w *setupWindow) run() bool {
	theSetup = w
	module, _, _ := procGetModuleHandle.Call(0)
	className := wide(instance + "Setup")
	cursor, _, _ := procLoadCursor.Call(0, idcArrow)
	appIcon, _, _ := procLoadIcon.Call(module, uintptr(unsafe.Pointer(wide("APP"))))
	class := windowClassEx{WndProc: newCallback(setupProcedure), Instance: module, ClassName: className, Cursor: cursor, Icon: appIcon, IconSm: appIcon}
	class.Size = uint32(unsafe.Sizeof(class))
	procRegisterClassEx.Call(uintptr(unsafe.Pointer(&class)))

	var cursorAt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&cursorAt)))
	w.scale = monitorScale(cursorAt)
	work := workArea(cursorAt)
	width, height := w.dip(setupWidth+2*setupMargin), w.dip(setupHeight+2*setupMargin)
	w.left = int(work.Left) + (int(work.Right-work.Left)-width)/2
	w.top = int(work.Top) + (int(work.Bottom-work.Top)-height)/2
	var ok bool
	if w.canvas, ok = newDIB(width, height); !ok {
		return w.result()
	}
	title := "Circle Lens Setup"
	switch {
	case w.removing:
		title = "Uninstall Circle Lens"
	case w.screen == screenGuide:
		title = "How to use Circle Lens"
	case w.screen == screenNews:
		title = "What's new in Circle Lens"
	}
	w.window, _, _ = procCreateWindowEx.Call(wsExLayered|wsExAppWindow, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(wide(title))),
		wsPopup, uintptr(w.left), uintptr(w.top), uintptr(width), uintptr(height), 0, 0, module, 0)
	if w.window == 0 {
		return w.result()
	}
	w.opened, w.screenFrom = time.Now(), time.Now()
	w.layout()
	started := time.Now()
	w.render()
	debugLog("setup window: first frame in %v", time.Since(started).Round(time.Millisecond))
	w.present(0, w.dip(10))
	procShowWindow.Call(w.window, swShow)
	procSetForegroundWindow.Call(w.window)
	w.tick()

	var msg message
	for !w.finished {
		if result, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0); int32(result) <= 0 {
			w.quit = true
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessage.Call(uintptr(unsafe.Pointer(&msg)))
	}
	procDestroyWindow.Call(w.window)
	w.window = 0
	if w.quit {
		procPostQuitMessage.Call(0) // run from the app (the tour, the notes): its own loop must end too
	}
	w.canvas.Close()
	w.releaseFonts()
	return w.result()
}

func (w *setupWindow) dip(value float64) int { return int(math.Round(value * w.scale)) }

// finish closes the window; then runs once it has gone.
func (w *setupWindow) finish(then func() bool) {
	w.result = then
	w.finished = true
	procPostMessage.Call(w.window, 0, 0, 0) // wake the message loop
}

/* ---------- what the buttons do ---------- */

func (w *setupWindow) startWork() {
	w.goTo(screenWorking)
	w.steps, w.pending, w.workDone, w.workErr = nil, nil, false, nil
	w.workFrom = time.Now()
	window, startup, updates, updating, from := w.window, w.startup, w.updates, w.from != "", w.from
	if updating {
		startup = startsWithWindows()
	}
	steps := make(chan string, 8)
	go func() {
		say := func(line string) {
			steps <- line
			procPostMessage.Call(window, messageSetupStep, 0, 0)
		}
		err := install(startup, func(step string) {
			switch step {
			case "copy":
				if updating {
					say("Replaced " + from + " with " + version)
				} else {
					say("Copied into your apps")
				}
			case "menu":
				if !updating {
					say("Added to the Start menu")
				}
			case "apps":
				if !updating {
					say("Listed in Settings → Apps")
				}
			case "startup":
				if !updating {
					say("Starts with Windows")
				}
			}
		})
		if err == nil {
			if path, pathErr := settings.Path(instance); pathErr == nil {
				saved := settings.Load(path)
				if !updating {
					saved.AutoUpdate = updates
				}
				saved.Welcomed = true // the ready screen has said how to use it
				err = settings.Save(path, saved)
			}
			switch {
			case updating:
				say("Kept your settings")
			case updates:
				say("Keeps itself up to date")
			default:
				say("Updates only when you ask")
			}
		}
		setupWorkResult.Store(errorBox{err})
		procPostMessage.Call(window, messageSetupDone, 0, 0)
	}()
	w.stepSource = steps
}

func (w *setupWindow) startRemoving() {
	w.goTo(screenRemoving)
	w.steps, w.pending, w.workDone, w.workErr = nil, nil, false, nil
	w.workFrom = time.Now()
	window := w.window
	steps := make(chan string, 8)
	go func() {
		err := removeInstallation()
		if err == nil {
			for _, line := range []string{"Closed Circle Lens", "Removed it from your apps", "Removed its settings"} {
				steps <- line
			}
			procPostMessage.Call(window, messageSetupStep, 0, 0)
		}
		setupWorkResult.Store(errorBox{err})
		procPostMessage.Call(window, messageSetupDone, 0, 0)
	}()
	w.stepSource = steps
}

// errorBox lets an error (or none) go through an atomic.Value.
type errorBox struct{ err error }

// cancel is the close button and Esc: on the first screen it leaves without doing anything; once
// installed it starts the app; on the failed screen it leaves, restarting an installed copy if there is
// one.
func (w *setupWindow) cancel() {
	switch w.screen {
	case screenWorking, screenRemoving:
		return // wait for it
	case screenGuide, screenNews:
		w.finish(func() bool { return true })
	case screenReady:
		w.finish(func() bool { return startInstalled() || true })
	case screenFailed:
		w.finish(func() bool {
			if _, err := os.Stat(installedExecutable()); err == nil && !w.removing {
				startInstalled()
			}
			return true
		})
	default:
		w.finish(func() bool { return true })
	}
}

/* ---------- moving between screens ---------- */

func (w *setupWindow) goTo(screen setupScreen) {
	if w.canvas != nil && w.window != 0 {
		w.before = append(w.before[:0], w.canvas.Pixels...)
	}
	w.screen, w.screenFrom = screen, time.Now()
	w.hover, w.pressed, w.focus = "", "", -1
	w.layout()
	w.dirty = true
	w.tick()
}

// tick keeps the animation timer running while something moves.
func (w *setupWindow) tick() {
	if w.window != 0 && !w.ticking {
		w.ticking = true
		procSetTimer.Call(w.window, timerSetup, 15, 0)
	}
}

// animate runs on the timer: the window fading in, the screens crossfading, the checklist and the ring.
func (w *setupWindow) animate() {
	now := time.Now()
	if w.screen == screenWorking || w.screen == screenRemoving {
		w.takeSteps()
		if len(w.pending) > 0 && now.Sub(w.lastStep) >= setupStepMs*time.Millisecond {
			w.steps = append(w.steps, w.pending[0])
			w.pending = w.pending[1:]
			w.lastStep = now
		}
		if w.workDone && len(w.pending) == 0 && now.Sub(w.workFrom) >= setupMinMs*time.Millisecond &&
			now.Sub(w.lastStep) >= setupStepReadyWait*time.Millisecond {
			switch {
			case w.workErr != nil:
				w.problem = sentence(w.workErr.Error())
				w.goTo(screenFailed)
			case w.removing:
				w.goTo(screenRemoved)
			default:
				w.goTo(screenReady)
			}
			return
		}
	}
	busy := w.screen == screenWorking || w.screen == screenRemoving || (w.screen == screenGuide && w.page == 0)
	if busy || w.dirty || now.Sub(w.screenFrom) < (setupFadeMs+30)*time.Millisecond {
		w.render() // the window fading in only needs presenting again
	}
	appear := math.Min(1, float64(now.Sub(w.opened).Milliseconds())/setupInMs)
	e := 1 - math.Pow(1-appear, 3)
	w.present(e, int(math.Round((1-e)*float64(w.dip(10)))))
	fading := now.Sub(w.screenFrom) < setupFadeMs*time.Millisecond && w.before != nil
	if appear >= 1 && !fading && !busy {
		procKillTimer.Call(w.window, timerSetup)
		w.ticking = false
	}
}

func (w *setupWindow) takeSteps() {
	for {
		select {
		case line := <-w.stepSource:
			w.pending = append(w.pending, line)
		default:
			return
		}
	}
}

/* ---------- layout ---------- */

// layout places the controls for the current screen, in window pixels.
func (w *setupWindow) layout() {
	s := w.scale
	x0, y0 := setupMargin*s, setupMargin*s
	cx := x0 + setupWidth*s/2
	w.controls = nil
	add := func(c setupControl) { w.controls = append(w.controls, c) }
	if w.screen != screenWorking && w.screen != screenRemoving {
		add(setupControl{id: "close", kind: setupClose, x0: x0 + (setupWidth-46)*s, y0: y0 + 14*s, x1: x0 + (setupWidth-14)*s, y1: y0 + 46*s, action: w.cancel})
	}
	buttons := func(primary, secondary string, primaryKind setupKind, onPrimary, onSecondary func()) {
		y := y0 + 372*s
		pw := w.measure(primary, styleButton) + 52*s
		sw := 0.0
		if secondary != "" {
			sw = w.measure(secondary, styleButton) + 40*s
		}
		gap := 12 * s
		total := pw
		if secondary != "" {
			total += gap + sw
		}
		left := cx - total/2
		add(setupControl{id: "primary", kind: primaryKind, x0: left, y0: y, x1: left + pw, y1: y + 46*s, label: primary, action: onPrimary})
		if secondary != "" {
			add(setupControl{id: "secondary", kind: setupGhost, x0: left + pw + gap, y0: y, x1: left + pw + gap + sw, y1: y + 46*s, label: secondary, action: onSecondary})
		}
	}
	switch w.screen {
	case screenInstall:
		buttons("Install Circle Lens", "Just run it", setupPrimary, w.startWork, func() {
			if w.remember != nil {
				w.remember()
			}
			w.finish(func() bool { return false })
		})
		// The two choices, side by side under the buttons.
		labelStyle := textStyle{face: faceText, size: 12.5, weight: fwNormal}
		first, second := "Start with Windows", "Update automatically"
		width := func(label string) float64 { return 30*s + 9*s + w.measure(label, labelStyle) }
		gap := 26 * s
		left := cx - (width(first)+gap+width(second))/2
		y := y0 + 440*s
		add(setupControl{id: "startup", kind: setupToggle, x0: left, y0: y, x1: left + width(first), y1: y + 22*s, label: first, on: w.startup,
			action: func() { w.startup = !w.startup }})
		left += width(first) + gap
		add(setupControl{id: "updates", kind: setupToggle, x0: left, y0: y, x1: left + width(second), y1: y + 22*s, label: second, on: w.updates,
			action: func() { w.updates = !w.updates }})
	case screenUpdate:
		buttons("Update now", "Just run it", setupPrimary, w.startWork, func() { w.finish(func() bool { return false }) })
	case screenReady:
		buttons("Try it now", "Done", setupPrimary, func() {
			w.finish(func() bool { startInstalled("-open"); return true })
		}, func() { w.finish(func() bool { startInstalled(); return true }) })
	case screenFailed:
		retry := w.startWork
		if w.removing {
			retry = w.startRemoving
		}
		buttons("Try again", "Close", setupPrimary, retry, w.cancel)
	case screenRemove:
		buttons("Uninstall", "Keep it", setupDanger, w.startRemoving, w.cancel)
	case screenRemoved:
		buttons("Close", "", setupPrimary, w.cancel, nil)
	case screenGuide:
		next := "Next"
		if w.page == guidePages-1 {
			next = "Done"
		}
		back := ""
		if w.page > 0 {
			back = "Back"
		}
		buttons(next, back, setupPrimary, func() {
			if w.page == guidePages-1 {
				w.cancel()
				return
			}
			w.turn(1)
		}, func() { w.turn(-1) })
		// Back goes on the left, as in any tour.
		if len(w.controls) >= 3 {
			primary, secondary := &w.controls[len(w.controls)-2], &w.controls[len(w.controls)-1]
			gap := secondary.x0 - primary.x1
			secondaryWidth, primaryWidth := secondary.x1-secondary.x0, primary.x1-primary.x0
			left := primary.x0
			secondary.x0, secondary.x1 = left, left+secondaryWidth
			primary.x0, primary.x1 = left+secondaryWidth+gap, left+secondaryWidth+gap+primaryWidth
		}
	case screenNews:
		buttons("Done", "All releases ↗", setupPrimary, w.cancel, func() { openAddress("https://github.com/fauzulkc/circle-lens/releases") })
		for i := range w.controls {
			if w.controls[i].id == "primary" || w.controls[i].id == "secondary" {
				w.controls[i].y0 += 92 * s
				w.controls[i].y1 += 92 * s
			}
		}
	}
}

// turn moves the tour a page forward (1) or back (-1).
func (w *setupWindow) turn(by int) {
	next := w.page + by
	if next < 0 || next >= guidePages {
		return
	}
	w.before = append(w.before[:0], w.canvas.Pixels...)
	w.page, w.screenFrom = next, time.Now()
	w.hover, w.pressed = "", ""
	w.layout()
	w.dirty = true
	w.tick()
}

/* ---------- drawing ---------- */

// render draws a frame: the screen (drawn again only when it changed), then what moves on it (the
// ring and checklist while busy), then the crossfade from the previous screen.
func (w *setupWindow) render() {
	if w.dirty || w.base == nil {
		w.renderBase()
	}
	canvas := w.canvas
	copy(canvas.Pixels, w.base)
	if w.screen == screenWorking || w.screen == screenRemoving {
		s := w.scale
		x0, y0 := setupMargin*s, setupMargin*s
		cx := x0 + setupWidth*s/2
		shown := float64(len(w.steps))
		total := shown + float64(len(w.pending))
		if !w.workDone {
			total++
		}
		progress := math.Max(0.08, shown/math.Max(total, 4))
		w.lensRing(cx, y0+100*s, 46*s, progress, time.Since(w.screenFrom).Seconds()*2.4, brandGradient)
		w.checklist(cx, y0+272*s)
		w.opaque(cx-130*s, y0+272*s, cx+170*s, y0+272*s+6*30*s)
	}
	if w.screen == screenGuide && w.page == 0 {
		s := w.scale
		w.guideSpin(setupMargin*s+setupWidth*s/2, setupMargin*s+100*s, 46*s)
	}
	if fade := time.Since(w.screenFrom).Seconds() * 1000 / setupFadeMs; fade < 1 && len(w.before) == len(canvas.Pixels) {
		t := float32(smooth(fade))
		pixels := canvas.Pixels
		for i := range pixels {
			pixels[i] = byte(float32(w.before[i])*(1-t) + float32(pixels[i])*t + 0.5)
		}
	}
	saveFrame(&render.Image{Pixels: canvas.Pixels, Width: canvas.Width, Height: canvas.Height})
}

// renderBase draws the current screen, finished, into base.
func (w *setupWindow) renderBase() {
	w.dirty = false
	canvas := w.canvas
	s := w.scale
	x0, y0 := setupMargin*s, setupMargin*s
	x1, y1 := x0+setupWidth*s, y0+setupHeight*s
	cx := (x0 + x1) / 2
	w.layer.begin(&w.painter, x0, y0, x1, y1, setupRadius*s, 16*s, 26*s, 0.42)

	artY := y0 + 100*s
	if w.screen != screenNews {
		w.lensArt(cx, artY, 46*s)
	}
	switch w.screen {
	case screenReady, screenRemoved:
		w.lensRing(cx, artY, 46*s, 1, 0, gradient(brand.green, brand.cyan))
	case screenFailed:
		w.lensRing(cx, artY, 46*s, 1, 0, gradient(brand.red, mixColour(brand.red, brand.violet, 0.5)))
	}
	title := func(first, second string) {
		w.write(first, cx, y0+194*s, 44*s, styleTitle, brand.text, alignCentre)
		w.writeGradient(second, cx, y0+236*s, 44*s, styleTitle, brandGradient, alignCentre)
	}
	lede := func(lines ...string) {
		for i, line := range lines {
			w.richLine(line, cx, y0+(298+float64(i)*24)*s, 22*s, styleLede, brand.muted)
		}
	}
	meta := func(line string) {
		w.write(line, cx, y0+492*s, 18*s, styleSmall, brand.dim, alignCentre)
	}
	eyebrowAt := y0 + 166*s
	switch w.screen {
	case screenInstall:
		w.eyebrow("FREE · OPEN SOURCE · v"+version, cx, eyebrowAt, 18*s, brand.green, brand.cyan, alignCentre)
		title("Draw a circle.", "Read anything.")
		lede("Magnify anything on screen, right where you look.", "Draw a circle twice with the mouse, or tap [Shift] twice.")
		meta("For you only · no administrator needed · remove any time")
	case screenUpdate:
		w.eyebrow("UPDATE · v"+w.from+" → v"+version, cx, eyebrowAt, 18*s, brand.green, brand.cyan, alignCentre)
		title("Circle Lens "+version, "is ready.")
		lede("You have "+w.from+". Updating keeps your settings", "and takes a second.")
		meta("From now on it keeps itself up to date, unless you turn that off.")
	case screenWorking, screenRemoving:
		label, heading := "SETTING UP", "Installing…"
		if w.from != "" {
			heading = "Updating…"
		}
		if w.removing {
			label, heading = "REMOVING", "Uninstalling…"
		}
		w.eyebrow(label, cx, eyebrowAt, 18*s, brand.blue, brand.cyan, alignCentre)
		w.write(heading, cx, y0+200*s, 48*s, styleTitle, brand.text, alignCentre)
		meta("This takes a second.")
	case screenReady:
		w.eyebrow("READY", cx, eyebrowAt, 18*s, brand.green, brand.green, alignCentre)
		title("Circle Lens", "is ready.")
		lede("Draw a circle twice anywhere, or tap [Shift] twice.", "Its settings are behind its icon by the clock.")
		choices := "It starts with Windows and keeps itself up to date."
		switch {
		case w.from != "":
			choices = "Your settings are just as they were."
		case !w.startup && !w.updates:
			choices = "It won't start with Windows or update by itself."
		case !w.startup:
			choices = "It keeps itself up to date; start it from the Start menu."
		case !w.updates:
			choices = "It starts with Windows; it updates when you ask."
		}
		meta(choices)
	case screenFailed:
		w.eyebrow("SOMETHING WENT WRONG", cx, eyebrowAt, 18*s, brand.red, brand.red, alignCentre)
		heading := "Couldn't install"
		switch {
		case w.removing:
			heading = "Couldn't uninstall"
		case w.from != "":
			heading = "Couldn't update"
		}
		w.write(heading, cx, y0+200*s, 48*s, styleTitle, brand.text, alignCentre)
		for i, line := range w.wrap(w.problem, styleLede, 360*s, 3) {
			w.write(line, cx, y0+(268+float64(i)*24)*s, 22*s, styleLede, brand.muted, alignCentre)
		}
		meta("Nothing else on your computer was changed.")
	case screenRemove:
		w.eyebrow("UNINSTALL", cx, eyebrowAt, 18*s, brand.red, brand.muted, alignCentre)
		title("Remove", "Circle Lens?")
		lede("Its settings go too. You can install it again", "any time from its GitHub page.")
		meta("Takes a second · nothing else is touched")
	case screenGuide:
		label, first, second, lines := guidePage(w.page)
		w.eyebrow("HOW TO USE · "+label, cx, eyebrowAt, 18*s, brand.blue, brand.cyan, alignCentre)
		title(first, second)
		lede(lines...)
		w.guideDots(cx, y0+500*s)
	case screenNews:
		heading := "What's new"
		if version != "dev" {
			w.eyebrow("WHAT'S NEW · v"+version, cx, y0+44*s, 18*s, brand.green, brand.cyan, alignCentre)
			heading = "In this version"
		}
		w.writeGradient(heading, cx, y0+66*s, 46*s, styleTitle, brandGradient, alignCentre)
		w.drawNews()
	case screenRemoved:
		w.eyebrow("DONE", cx, eyebrowAt, 18*s, brand.green, brand.muted, alignCentre)
		title("Circle Lens", "is removed.")
		lede("Thanks for trying it.", "Its settings were removed too.")
		meta("github.com/fauzulkc/circle-lens")
	}

	for _, c := range w.controls {
		hovered, pressed := c.id == w.hover, c.id == w.pressed && c.id == w.hover
		switch c.kind {
		case setupPrimary:
			w.primaryButton(c.x0, c.y0, c.x1, c.y1, c.label, hovered, pressed)
		case setupGhost:
			w.ghostButton(c.x0, c.y0, c.x1, c.y1, c.label, hovered, pressed, brand.white)
		case setupDanger:
			w.ghostButton(c.x0, c.y0, c.x1, c.y1, c.label, hovered, pressed, brand.red)
		case setupToggle:
			w.brandSwitch(c.x0, c.y0+2*s, 30*s, 18*s, c.on, hovered)
			colour := brand.muted
			if hovered {
				colour = brand.text
			}
			w.write(c.label, c.x0+39*s, c.y0, c.y1-c.y0, textStyle{face: faceText, size: 12.5, weight: fwNormal}, colour, alignLeft)
		case setupClose:
			if hovered {
				w.circle((c.x0+c.x1)/2, (c.y0+c.y1)/2, (c.x1-c.x0)/2, brand.white, 0.08)
			}
			mx, my, arm := (c.x0+c.x1)/2, (c.y0+c.y1)/2, 5*s
			colour := brand.dim
			if hovered {
				colour = brand.text
			}
			w.line(mx-arm, my-arm, mx+arm, my+arm, 1.6*s, colour, 1)
			w.line(mx-arm, my+arm, mx+arm, my-arm, 1.6*s, colour, 1)
		}
	}
	if w.focus >= 0 && w.focus < len(w.controls) {
		c := w.controls[w.focus]
		radius := (c.y1 - c.y0) / 2
		w.focusRing(c.x0, c.y0, c.x1, c.y1, radius)
	}

	w.layer.end(&w.painter)
	w.base = append(w.base[:0], canvas.Pixels...)
}

// checklist draws the steps done so far, each with a green tick, the newest sliding in.
func (w *setupWindow) checklist(cx, top float64) {
	s := w.scale
	left := cx - 120*s
	for i, line := range w.steps {
		y := top + float64(i)*30*s
		appear := 1.0
		if i == len(w.steps)-1 {
			appear = smooth(time.Since(w.lastStep).Seconds() * 1000 / 220)
		}
		w.circle(left+9*s, y+11*s, 9*s, brand.green, 0.14*appear)
		w.line(left+5*s, y+11.5*s, left+8*s, y+14.5*s, 1.8*s, brand.green, appear)
		w.line(left+8*s, y+14.5*s, left+13.5*s, y+8*s, 1.8*s, brand.green, appear)
		colour := mixColour(brand.bg, brand.body, appear)
		w.write(line, left+28*s+(1-appear)*8*s, y, 22*s, styleBody, colour, alignLeft)
	}
}

// richLine draws a centred line in which [Key] is drawn as a keycap.
func (w *setupWindow) richLine(line string, cx, y, height float64, style textStyle, colour [3]byte) {
	s := w.scale
	type run struct {
		text string
		key  bool
	}
	var runs []run
	for line != "" {
		open := strings.Index(line, "[")
		end := strings.Index(line, "]")
		if open < 0 || end < open {
			runs = append(runs, run{text: line})
			break
		}
		if open > 0 {
			runs = append(runs, run{text: line[:open]})
		}
		runs = append(runs, run{text: line[open+1 : end], key: true})
		line = line[end+1:]
	}
	width := 0.0
	for _, r := range runs {
		if r.key {
			width += w.measure(r.text, styleKey) + 13*s + 6*s
		} else {
			width += w.measure(r.text, style)
		}
	}
	x := cx - width/2
	for _, r := range runs {
		if r.key {
			x += 3 * s
			x += w.keycap(r.text, x, y+1*s, height-2*s) + 3*s
		} else {
			x += w.write(r.text, x, y, height, style, colour, alignLeft)
		}
	}
}

// scrollBy scrolls the release notes by `dips`.
func (w *setupWindow) scrollBy(dips float64) {
	_, y0, _, y1 := w.newsBox()
	limit := math.Max(0, w.newsHeight-(y1-y0))
	next := math.Max(0, math.Min(limit, w.scroll+dips*w.scale))
	if next != w.scroll {
		w.scroll = next
		w.redraw()
	}
}

// sentence starts an error message with a capital and ends it with a full stop.
func sentence(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return "Something went wrong."
	}
	text = strings.ToUpper(text[:1]) + text[1:]
	if !strings.HasSuffix(text, ".") {
		text += "."
	}
	return text
}

// wrap breaks text into at most `limit` lines no wider than maxWidth.
func (w *setupWindow) wrap(text string, style textStyle, maxWidth float64, limit int) []string {
	var lines []string
	current := ""
	for _, word := range strings.Fields(text) {
		candidate := strings.TrimSpace(current + " " + word)
		if current != "" && w.measure(candidate, style) > maxWidth {
			lines = append(lines, current)
			current = word
		} else {
			current = candidate
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	if len(lines) > limit {
		lines = append(lines[:limit-1], strings.Join(lines[limit-1:], " "))
	}
	return lines
}

func (w *setupWindow) present(opacity float64, rise int) {
	destination := point{int32(w.left), int32(w.top + rise)}
	dimensions := size{int32(w.canvas.Width), int32(w.canvas.Height)}
	origin := point{}
	blend := blendFunction{SourceConstantAlpha: byte(math.Round(math.Max(0, math.Min(1, opacity)) * 255)), AlphaFormat: acSrcAlpha}
	position := uintptr(unsafe.Pointer(&destination))
	if rise == 0 && opacity >= 1 {
		position = 0 // where it is: it may have been dragged
	}
	procUpdateLayeredWindow.Call(w.window, 0, position, uintptr(unsafe.Pointer(&dimensions)), w.canvas.dc,
		uintptr(unsafe.Pointer(&origin)), 0, uintptr(unsafe.Pointer(&blend)), ulwAlpha)
}

func (w *setupWindow) redraw() {
	w.dirty = true
	w.render()
	w.present(1, 0)
}

/* ---------- input ---------- */

func (w *setupWindow) hit(x, y float64) int {
	for i, c := range w.controls {
		if x >= c.x0 && x < c.x1 && y >= c.y0 && y < c.y1 {
			return i
		}
	}
	return -1
}

func (w *setupWindow) controlID(index int) string {
	if index < 0 {
		return ""
	}
	return w.controls[index].id
}

func (w *setupWindow) activate(id string) {
	for _, c := range w.controls {
		if c.id == id {
			c.action()
			break
		}
	}
	if !w.finished {
		w.layout()
		w.redraw()
	}
}

func (w *setupWindow) key(virtualKey uintptr) {
	switch virtualKey {
	case vkEscape:
		w.cancel()
	case vkTab:
		if len(w.controls) == 0 {
			return
		}
		step := 1
		if keyHeld(vkShift) {
			step = len(w.controls) - 1
		}
		w.focus = (max(w.focus, -1) + step + len(w.controls)) % len(w.controls)
		w.redraw()
	case vkLeft, vkRight:
		if w.screen == screenGuide {
			w.turn(map[bool]int{true: -1, false: 1}[virtualKey == vkLeft])
		}
	case vkUp, vkDown:
		if w.screen == screenNews {
			w.scrollBy(map[bool]float64{true: -40, false: 40}[virtualKey == vkUp])
		}
	case vkReturn, vkSpace:
		if w.focus >= 0 && w.focus < len(w.controls) {
			w.activate(w.controls[w.focus].id)
		} else if virtualKey == vkReturn {
			w.activate("primary")
		}
	}
}

func setupProcedure(window, msg, wParam, lParam uintptr) uintptr {
	w := theSetup
	if w == nil || w.window == 0 {
		result, _, _ := procDefWindowProc.Call(window, msg, wParam, lParam)
		return result
	}
	switch uint32(msg) {
	case wmTimer:
		if wParam == timerSetup {
			w.animate()
		}
		return 0
	case messageSetupStep:
		w.takeSteps()
		return 0
	case messageSetupDone:
		w.takeSteps()
		w.workErr = setupWorkResult.Load().(errorBox).err
		w.workDone = true
		return 0
	case wmNcHitTest:
		var r rect
		procGetWindowRect.Call(window, uintptr(unsafe.Pointer(&r)))
		x := float64(int16(lParam&0xFFFF)) - float64(r.Left)
		y := float64(int16(lParam>>16&0xFFFF)) - float64(r.Top)
		if w.hit(x, y) >= 0 {
			return htClient
		}
		s := w.scale
		if x >= setupMargin*s && y >= setupMargin*s && x < (setupMargin+setupWidth)*s && y < (setupMargin+setupHeight)*s {
			return htCaption // drag it by anything that isn't a control
		}
		return htClient
	case wmSetCursor:
		if w.hover != "" {
			hand, _, _ := procLoadCursor.Call(0, idcHand)
			procSetCursor.Call(hand)
			return 1
		}
	case wmMouseMove:
		if !w.tracking {
			tracking := trackMouseEvent{Flags: tmeLeave, Window: window}
			tracking.Size = uint32(unsafe.Sizeof(tracking))
			procTrackMouseEvent.Call(uintptr(unsafe.Pointer(&tracking)))
			w.tracking = true
		}
		x, y := clientPoint(lParam)
		if id := w.controlID(w.hit(x, y)); id != w.hover {
			w.hover = id
			w.redraw()
		}
		return 0
	case wmMouseLeave:
		w.tracking = false
		if w.hover != "" {
			w.hover = ""
			w.redraw()
		}
		return 0
	case wmLButtonDown:
		w.pressed = w.controlID(w.hit(clientPoint(lParam)))
		w.redraw()
		return 0
	case wmLButtonUp:
		id, pressed := w.controlID(w.hit(clientPoint(lParam))), w.pressed
		w.pressed = ""
		if id != "" && id == pressed {
			w.activate(id)
		} else {
			w.redraw()
		}
		return 0
	case wmKeyDown:
		w.key(wParam)
		return 0
	case wmClose:
		w.cancel()
		return 0
	case wmMouseWheel:
		if w.screen == screenNews {
			w.scrollBy(-float64(int16(wParam>>16)) / 120 * 60)
		}
		return 0
	}
	result, _, _ := procDefWindowProc.Call(window, msg, wParam, lParam)
	return result
}

/* ---------- previews (test builds) ---------- */

// previewSetup draws the screens CIRCLE_LENS_PREVIEW names (comma-separated: install, update, working,
// ready, failed, remove, removing, removed) to CIRCLE_LENS_FRAMES, offscreen, and reports whether it did.
func previewSetup() bool {
	names := os.Getenv("CIRCLE_LENS_PREVIEW")
	if names == "" || instance == "CircleLens" {
		return false
	}
	screens := map[string]setupScreen{"install": screenInstall, "update": screenUpdate, "working": screenWorking, "ready": screenReady,
		"failed": screenFailed, "remove": screenRemove, "removing": screenRemoving, "removed": screenRemoved,
		"guide": screenGuide, "guide2": screenGuide, "guide3": screenGuide, "guide4": screenGuide, "news": screenNews}
	previewing = true
	for _, name := range strings.Split(names, ",") {
		if strings.HasPrefix(name, "panel") {
			previewPanel(name)
			continue
		}
		screen, ok := screens[name]
		if !ok {
			continue
		}
		w := newSetupWindow()
		w.scale = monitorScale(point{})
		if screen == screenUpdate || name == "update" {
			w.from = "0.5.0"
		}
		w.removing = screen >= screenRemove
		if name == "failed" {
			w.problem = sentence(errors.New("could not write to the registry (5)").Error())
		}
		if screen == screenWorking {
			w.steps = []string{"Copied into your apps", "Added to the Start menu", "Listed in Settings → Apps"}
			w.pending = []string{"Starts with Windows"}
		}
		if screen == screenRemoving {
			w.steps = []string{"Closed Circle Lens", "Removed it from your apps"}
		}
		var ok2 bool
		if w.canvas, ok2 = newDIB(w.dip(setupWidth+2*setupMargin), w.dip(setupHeight+2*setupMargin)); !ok2 {
			continue
		}
		w.screen = screen
		if strings.HasPrefix(name, "guide") && len(name) == 6 {
			w.page = int(name[5]-'0') - 1
		}
		w.screenFrom = time.Now().Add(-time.Second)
		w.lastStep = time.Now().Add(-time.Second)
		w.layout()
		if name == "install" {
			w.hover = "primary"
		}
		started := time.Now()
		w.render()
		first := time.Since(started)
		started = time.Now()
		w.render() // an animation frame: only what moves is drawn again
		debugLog("preview %s: first frame %v, next frame %v", name, first.Round(time.Millisecond), time.Since(started).Round(time.Millisecond))
		w.canvas.Close()
		w.releaseFonts()
	}
	return true
}

// previewing is set while drawing previews: the panel shows its update controls.
var previewing bool

// previewPanel draws the tray panel with the default settings ("panel"), paused ("panel-paused"),
// recording a shortcut ("panel-recording") or the close key ("panel-closekey"), or with its own keys
// while the lens is open ("panel-custom").
func previewPanel(name string) {
	app = &application{settings: settings.Defaults}
	app.settings.Enabled = name != "panel-paused"
	app.shortcut = shortcut.ParseOrDefault(app.settings.Shortcut)
	app.state = lens.New(app.settings.Zoom)
	app.recording = name == "panel-recording" || name == "panel-closekey"
	if name == "panel-closekey" {
		app.recordingFor = "close"
	}
	if name == "panel-open" {
		app.state.Open(0, true) // the lens is open: the button says Close lens
	}
	if name == "panel-custom" {
		app.settings.ZoomInKeys, app.settings.ZoomOutKeys, app.settings.CloseKey = "Ctrl+Up", "Ctrl+Down", "Esc"
	}
	f := &flyout{painter: newPainter(), theme: newFlyoutTheme(), focus: -1}
	f.scale = monitorScale(point{})
	f.height = flyoutHeight + flyoutRow
	var ok bool
	if f.canvas, ok = newDIB(f.px(flyoutWidth+2*flyoutShadow), f.px(f.height+2*flyoutShadow)); !ok {
		return
	}
	f.layout()
	if name == "panel" {
		f.hover = "open"
	}
	started := time.Now()
	f.render()
	first := time.Since(started)
	started = time.Now()
	f.render() // a hover change
	debugLog("preview %s: first frame %v, next frame %v", name, first.Round(time.Millisecond), time.Since(started).Round(time.Millisecond))
	f.canvas.Close()
	f.releaseFonts()
}
