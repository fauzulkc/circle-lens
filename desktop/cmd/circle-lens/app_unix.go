//go:build linux || darwin

package main

// The app on Linux and macOS: the same lens as on Windows (app_windows.go), built on a small system
// interface that desktop_linux.go (X11) and desktop_darwin.go (AppKit) provide. Everything in this file
// runs on the UI thread, the system's main loop; the system's other threads reach it through post.

import (
	"flag"
	"fmt"
	"math"
	"os"
	"runtime"
	"runtime/debug"
	"sync/atomic"
	"time"

	"circlelens/internal/gesture"
	"circlelens/internal/lens"
	"circlelens/internal/render"
	"circlelens/internal/settings"
	"circlelens/internal/shortcut"
)

// system is what the app needs from the OS.
type system interface {
	// run starts the main loop, calls ready on it, and returns once stop has been called.
	run(ready func())
	stop()
	// post runs a function on the main loop (from any thread).
	post(func())

	// The screen, in physical pixels.
	pointer() (x, y int, ok bool)
	scale() float64 // pixels per DIP (1 = 96 DPI)
	bounds() (left, top, right, bottom int)
	refreshRate() float64
	// excludesLens: grab never sees the lens, so the lens can be live. Otherwise it shows a still.
	excludesLens() bool
	// grab copies part of the screen into opaque BGRA pixels, rows packed.
	grab(left, top, width, height int, into []byte) bool

	// The lens window: premultiplied BGRA, its top-left corner at left, top.
	showLens(canvas *render.Image, left, top int)
	hideLens()
	// text draws one line: a coverage byte per pixel.
	text(line string, pixels float64) (coverage []byte, width, height int)
	dark() bool
	reducedMotion() bool

	// Input. Each reports to the app through post.
	watchPointer(on bool)     // the pointer moving, buttons and the wheel
	watchModifiers(on bool)   // modifier keys, for a double-tap shortcut; never which other key
	grabKeys(grabs []keyGrab) // key combinations taken from every app; replaces the previous set
	grabZoomWheel(on bool)    // Ctrl + wheel taken from every app (while the lens is open)

	// The tray (or menu bar) icon and its menu, rebuilt whenever something in it changes.
	setMenu(items []menuItem, open bool)
	openAddress(address string)

	startsAtLogin() bool
	setStartAtLogin(on bool) error
}

// keyGrab is a key combination the app takes while it runs, and what it does.
type keyGrab struct {
	key    shortcut.Shortcut
	action func()
}

// menuItem is one line of the tray menu.
type menuItem struct {
	label     string
	detail    string // a note after the label: the shortcut, "default"…
	checked   bool
	radio     bool // one of several choices (checked marks the chosen one)
	checkable bool
	disabled  bool
	separator bool
	action    func()
	children  []menuItem
}

type application struct {
	sys          system
	settingsPath string
	settings     settings.Settings
	shortcut     shortcut.Shortcut
	state        *lens.State
	capturer     *capturer
	label        *render.Image
	labelKey     string
	renderer     render.Renderer
	canvas       *render.Image
	dark         bool
	started      time.Time
	frames       int
	openedAt     time.Time
	framePending atomic.Bool // a frame is posted to the main loop and not yet drawn

	detector    *gesture.Detector
	buttonsHeld int
	watching    bool
	tap         shortcut.DoubleTap
	control     *controlServer
}

var app *application

func main() {
	openNow := flag.Bool("open", false, "open the lens at the pointer once started")
	toggle := flag.Bool("toggle", false, "open or close the lens of the copy already running (for a mouse button or launcher)")
	quit := flag.Bool("quit", false, "quit the copy already running")
	installNow := flag.Bool("install", false, "add it to the app menu and start it at login")
	uninstallNow := flag.Bool("uninstall", false, "take it out of the app menu and start-up")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}
	switch {
	case *installNow:
		if err := install(); err != nil {
			fmt.Fprintln(os.Stderr, "Could not install Circle Lens:", err)
			os.Exit(1)
		}
		return
	case *uninstallNow:
		sendCommand("quit")
		if err := uninstall(); err != nil {
			fmt.Fprintln(os.Stderr, "Could not uninstall Circle Lens:", err)
			os.Exit(1)
		}
		return
	case *quit:
		sendCommand("quit")
		return
	case *toggle && sendCommand("toggle"):
		return
	}
	// Starting it again replaces the running copy, so a new version takes over from an old one.
	if sendCommand("quit") {
		waitUntilGone(5 * time.Second)
	}
	control, err := listenForCommands()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Circle Lens is already running, or could not start:", err)
		os.Exit(1)
	}
	runtime.LockOSThread()
	debug.SetGCPercent(20) // the live heap is a few pixel buffers
	defer startProfile()()

	sys, err := newSystem()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Circle Lens can't run here:", err)
		os.Exit(1)
	}
	app = &application{sys: sys, started: time.Now(), control: control}
	app.settingsPath, _ = settings.Path(instance)
	app.settings = settings.Load(app.settingsPath)
	app.state = lens.New(app.settings.Zoom)
	sys.run(func() {
		control.serve(func(command string) { sys.post(func() { app.command(command) }) })
		app.applySettings()
		app.rebuildMenu()
		if *openNow || *toggle {
			app.toggle()
		}
	})
	app.finish()
	control.close()
}

func (a *application) command(command string) {
	switch command {
	case "toggle":
		a.toggle()
	case "open":
		a.open()
	case "quit":
		a.quit()
	}
}

func (a *application) quit() {
	a.finish()
	a.sys.watchPointer(false)
	a.sys.watchModifiers(false)
	a.sys.stop()
}

/* ---------- the lens ---------- */

func (a *application) now() float64 { return float64(time.Since(a.started).Microseconds()) / 1000 }

func (a *application) toggle() {
	if a.state.IsOpen() {
		a.close()
	} else {
		a.open()
	}
}

func (a *application) open() {
	if a.state.IsOpen() {
		return
	}
	a.dark = a.sys.dark()
	a.state.Open(a.now(), a.sys.reducedMotion())
	a.frames, a.openedAt = 0, time.Now()
	if a.capturer == nil {
		a.capturer = startCapturer(a.sys, a.requestFrame, a.settings.Radius, 1, !a.sys.excludesLens())
	}
	a.capturer.animating.Store(true)
	a.lensOpened(true)
}

func (a *application) close() {
	if !a.state.IsOpen() {
		return
	}
	a.state.Close(a.now())
	a.lensOpened(false)
	if a.capturer != nil {
		a.capturer.animating.Store(true)
	}
}

// lensOpened takes the keys and the wheel that work while the lens is open (or gives them back), and
// starts the gap before a circle counts again.
func (a *application) lensOpened(open bool) {
	a.grabKeys()
	a.sys.grabZoomWheel(open)
	if a.detector != nil {
		a.detector.Hold(a.now() + a.settings.Gap*1000)
	}
	a.rebuildMenu()
}

func (a *application) zoomBy(step float64) {
	if a.state.IsOpen() {
		a.state.SetZoom(a.now(), a.state.Target()+step)
		if a.capturer != nil {
			a.capturer.animating.Store(true)
		}
	}
}

// requestFrame is called by the capturer's ticker (any thread): draw a frame soon, once.
func (a *application) requestFrame() {
	if a.framePending.CompareAndSwap(false, true) {
		a.sys.post(a.drawFrame)
	}
}

// drawFrame draws the lens from the newest capture; once it has closed, it stops everything.
func (a *application) drawFrame() {
	a.framePending.Store(false)
	if a.capturer == nil {
		return
	}
	x, y, _ := a.sys.pointer()
	scale := a.sys.scale()
	a.state.Pointer(a.now(), float64(x)/scale, float64(y)/scale)
	frame := a.state.Frame(a.now())
	if !a.state.IsVisible() {
		a.finish()
		return
	}
	a.capturer.fresh.Store(false)
	capture := a.capturer.take()
	if capture == nil {
		return
	}
	capture.PointerX, capture.PointerY = float64(x)+0.5, float64(y)+0.5
	theme := render.Light
	if a.dark {
		theme = render.Dark
	}
	if a.frames == 0 {
		a.state.StartClock(a.now())
		frame = a.state.Frame(a.now())
	}
	radius := math.Round(a.settings.Radius * capture.Scale)
	side := render.Side(radius, capture.Scale)
	if a.canvas == nil || a.canvas.Width != side {
		a.canvas = render.NewImage(side, side)
	}
	a.renderer.Compose(a.canvas, capture, render.Scene{Frame: frame, Radius: radius, Theme: theme,
		Label: a.labelFor(a.state.Target(), a.shortcut.String(), capture.Scale, theme), EdgeRamp: a.settings.ConvexRim})
	saveFrame(a.canvas)
	pad := float64(render.Pad(capture.Scale))
	a.sys.showLens(a.canvas, int(math.Floor(capture.PointerX-radius-pad)), int(math.Floor(capture.PointerY-radius-pad)))

	// As on Windows: capture for the lowest zoom the animation will reach, so no capture competes with it.
	animating := a.state.Animating()
	captureZoom := frame.Zoom
	if animating {
		captureZoom = math.Min(frame.Zoom, a.state.Target())
		if a.state.Fast() > 0 || a.state.Speeding() {
			captureZoom = math.Min(captureZoom, lens.FastZoomLow(a.state.Target()))
		}
		if !a.state.IsOpen() {
			captureZoom = 1
		}
	}
	a.capturer.zoom.Store(math.Float64bits(captureZoom))
	a.capturer.animating.Store(animating)
	a.frames++
}

// finish hides the lens, stops the capture threads and hands the memory back.
func (a *application) finish() {
	if a.capturer == nil {
		return
	}
	a.capturer.stop()
	a.capturer = nil
	a.sys.hideLens()
	a.canvas, a.label, a.labelKey = nil, nil, ""
	a.renderer = render.Renderer{}
	if shown := time.Since(a.openedAt); a.frames > 0 {
		debugLog("lens shown %v: %d frames, %.0f fps", shown.Round(time.Millisecond), a.frames, float64(a.frames)/shown.Seconds())
	}
	debug.FreeOSMemory()
}

func (a *application) labelFor(zoom float64, closeHint string, scale float64, theme render.Theme) *render.Image {
	text := fmt.Sprintf("%.1f× · %s", zoom, closeHint)
	key := fmt.Sprintf("%s|%.2f|%v", text, scale, a.dark)
	if key != a.labelKey {
		a.label, a.labelKey = drawLabel(a.sys, text, scale, a.dark, theme), key
	}
	return a.label
}

/* ---------- input ---------- */

// pointerMoved feeds the gesture (x and y in pixels).
func (a *application) pointerMoved(x, y int) {
	if a.detector == nil {
		return
	}
	scale := a.sys.scale()
	if found, _, _ := a.detector.Feed(float64(x)/scale, float64(y)/scale, a.now(), a.buttonsHeld > 0); found {
		debugLog("toggle from a drawn circle")
		a.toggle()
	}
}

func (a *application) button(down bool) {
	if down {
		a.buttonsHeld++
		if a.detector != nil {
			a.detector.Reset()
		}
	} else {
		a.buttonsHeld = max(0, a.buttonsHeld-1)
	}
}

// scrolled: the wheel turned while the lens is open (notches), which eases the zoom out like fast motion.
func (a *application) scrolled(notches float64) {
	if a.state.IsOpen() {
		a.state.Scrolled(a.now(), math.Abs(notches)*100)
		if a.capturer != nil {
			a.capturer.animating.Store(true)
			a.requestFrame()
		}
	}
}

// modifier: a modifier key ("Ctrl", "Shift", "Alt") or another key ("") went down or up.
func (a *application) modifier(name string, down bool) {
	now := a.now()
	if down {
		a.tap.Down(name, now)
		return
	}
	if tapped := a.tap.Up(name, now); tapped != "" {
		debugLog("toggle from the shortcut")
		a.toggle()
	}
}

/* ---------- settings ---------- */

func (a *application) applySettings() {
	if a.settings.Enabled != a.watching {
		a.watching = a.settings.Enabled
		if a.watching {
			a.detector = gesture.New(a.settings.Loops)
		} else {
			a.detector = nil
		}
		a.sys.watchPointer(a.watching)
	}
	if a.detector != nil {
		a.detector.SetLoops(a.settings.Loops)
		a.detector.SetGap(a.settings.Gap)
	}
	a.state.SetZoomOutWhenFast(a.settings.ZoomOutWhenFast)
	if a.capturer != nil {
		a.capturer.radiusDip.Store(math.Float64bits(a.settings.Radius))
	}
	a.shortcut = shortcut.ParseOrDefault(a.settings.Shortcut)
	a.tap = shortcut.DoubleTap{Only: a.shortcut.Double}
	a.sys.watchModifiers(a.shortcut.IsDoubleTap())
	a.grabKeys()
}

// grabKeys takes the open/close combination always, and the zoom and close keys while the lens is open.
func (a *application) grabKeys() {
	var grabs []keyGrab
	if !a.shortcut.IsDoubleTap() {
		grabs = append(grabs, keyGrab{a.shortcut, a.toggle})
	}
	if a.state.IsOpen() {
		for _, keys := range []struct {
			text   string
			action func()
		}{
			{a.settings.ZoomInKeys, func() { a.zoomBy(0.2) }},
			{a.settings.ZoomOutKeys, func() { a.zoomBy(-0.2) }},
			{a.settings.CloseKey, a.close},
		} {
			if parsed, err := shortcut.ParseWhileOpen(keys.text); err == nil && keys.text != "" {
				grabs = append(grabs, keyGrab{parsed, keys.action})
			}
		}
	}
	a.sys.grabKeys(grabs)
}

func (a *application) save() {
	if a.settingsPath != "" {
		_ = settings.Save(a.settingsPath, a.settings)
	}
	a.applySettings()
	a.rebuildMenu()
}

// change applies a settings change from the menu and saves it.
func (a *application) change(edit func(*settings.Settings)) func() {
	return func() {
		edit(&a.settings)
		a.save()
	}
}
