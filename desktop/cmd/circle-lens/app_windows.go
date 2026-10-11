//go:build windows

package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"circlelens/internal/lens"
	"circlelens/internal/render"
	"circlelens/internal/settings"
	"circlelens/internal/shortcut"
)

// Messages posted to the UI window, by the hook thread, the capture thread, or another copy of the app.
const (
	messageTray   = wmApp + 1
	messageToggle = wmApp + 2
	messageFrame  = wmApp + 3
	messageZoom   = wmApp + 4
	messageQuit   = wmApp + 5
	// A wheel scroll under the open lens (it eases the zoom out, like fast movement).
	messageScrolled = wmApp + 6

	// messageToggle's wParam: what asked for it (for the log).
	toggleFromCommand  = 0
	toggleFromGesture  = 1
	toggleFromShortcut = 2

	// The keyboard hook finished recording a shortcut.
	messageRecorded = wmApp + 7

	hotkeyToggle = 1
	hotkeyIn     = 3
	hotkeyOut    = 4
	hotkeyPadIn  = 5
	hotkeyPadOut = 6
)

type application struct {
	instance, window uintptr
	settingsPath     string
	settings         settings.Settings
	watcher          *mouseWatcher
	overlay          *overlay
	flyout           *flyout
	keyboard         *keyWatcher
	shortcut         shortcut.Shortcut
	recording        bool
	// A note about the shortcut for the flyout ("Ctrl+Alt+Z is used by another app"), and until when.
	shortcutNote      string
	shortcutNoteUntil time.Time
	capturer          *capturer
	state             *lens.State
	dark              bool
	keysRegistered    bool
	trayIcon          uintptr
	taskbarCreated    uint32
	started           time.Time
	frames            int
	openedAt          time.Time
	// The open animation's frame times, to report how smooth it was (debug log only).
	openingFrames    []time.Time
	measuringOpening bool
	closingFrames    []time.Time
	updates          updater
	// What a click on the current tray notification does ("update", or a web address).
	balloonAction string
}

// app is reached from Win32 callbacks; all of it runs on the one locked UI thread (the mouse hook and
// the screen capture have threads of their own).
var app *application

func main() {
	openNow := flag.Bool("open", false, "open the lens at the pointer once started")
	toggle := flag.Bool("toggle", false, "open or close the lens of the copy already running (for a mouse button or launcher)")
	quit := flag.Bool("quit", false, "quit the copy already running")
	installNow := flag.Bool("install", false, "install (or update) without asking")
	uninstallNow := flag.Bool("uninstall", false, "uninstall (Settings → Apps runs this)")
	quiet := flag.Bool("quiet", false, "with -uninstall: do not ask or report")
	showVersion := flag.Bool("version", false, "print the version and exit")
	updating := flag.Bool("update", false, "with -install: an update the running copy started (quiet, and it restarts that copy if it fails)")
	updated := flag.Bool("updated", false, "started by an update: say so")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}
	runtime.LockOSThread()
	if previewSetup() {
		return // drew the setup screens for checking (test builds only)
	}
	// The live heap is a few pixel buffers; collect early rather than let it grow to twice that.
	debug.SetGCPercent(20)
	procSetProcessDpiAwareness.Call(dpiAwarenessPerMonitorV2)

	switch {
	case *uninstallNow:
		uninstall(*quiet)
		return
	case *installNow:
		// Silent (winget, the updater): a new install starts with Windows; an update keeps the choice.
		if err := install(installedVersion() == "" || startsWithWindows(), nil); err != nil {
			if *updating {
				startInstalled() // the copy that was running comes back, and tries again another day
				return
			}
			tell("Could not install "+displayName()+": "+err.Error(), mbIconError)
			os.Exit(1) // so winget, or a script, knows it failed
		}
		if *updating {
			startInstalled("-updated")
		} else {
			startInstalled()
		}
		return
	case !*toggle && !*quit && !runningInstalled():
		// Started from anywhere but its install folder (a download, say): offer to install or update.
		path, _ := settings.Path(instance)
		saved := settings.Load(path)
		if offerInstall(saved.DeclinedInstall, func() {
			saved.DeclinedInstall = true
			_ = settings.Save(path, saved)
		}) {
			return
		}
	}

	running, _, _ := procFindWindow.Call(uintptr(unsafe.Pointer(wide(instance+"App"))), 0)
	switch {
	case *toggle && running != 0:
		procPostMessage.Call(running, messageToggle, 0, 0)
		return
	case *quit:
		if running != 0 {
			procPostMessage.Call(running, messageQuit, 0, 0)
		}
		return
	}
	// Starting it again replaces the running copy, so a new version takes over from an old one.
	if running != 0 && !handOver(running) {
		return
	}
	if !holdInstanceLock() {
		return // another copy started at the same moment, or the old one would not leave
	}
	followSystemTheme()
	defer startProfile()()

	app = &application{started: time.Now()}
	app.instance, _, _ = procGetModuleHandle.Call(0)
	app.settingsPath, _ = settings.Path(instance)
	app.settings = settings.Load(app.settingsPath)
	app.state = lens.New(app.settings.Zoom)

	className := wide(instance + "App")
	class := windowClassEx{WndProc: newCallback(windowProcedure), Instance: app.instance, ClassName: className}
	class.Size = uint32(unsafe.Sizeof(class))
	procRegisterClassEx.Call(uintptr(unsafe.Pointer(&class)))
	app.window, _, _ = procCreateWindowEx.Call(0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(wide("Circle Lens "+version))), wsPopup, 0, 0, 0, 0, 0, 0, app.instance, 0)
	if app.window == 0 {
		fail("Could not create the app window.")
	}
	// Let a later copy (or -toggle) reach this one even if this one runs as administrator and that
	// one does not; otherwise Windows drops their messages.
	for _, accepted := range []uintptr{messageToggle, messageQuit} {
		procChangeWindowMessageFilterEx.Call(app.window, accepted, msgfltAllow, 0)
	}

	var ok bool
	if app.overlay, ok = newOverlay(app.instance); !ok {
		fail("Could not create the lens window.")
	}
	app.flyout = newFlyout(app.instance)

	taskbarCreated, _, _ := procRegisterWindowMessage.Call(uintptr(unsafe.Pointer(wide("TaskbarCreated"))))
	app.taskbarCreated = uint32(taskbarCreated)
	app.addTrayIcon()
	defer app.removeTrayIcon()
	app.applySettings()
	app.startShortcut()
	defer app.stopKeyboard()
	defer app.stopWatching()
	if !app.settings.Welcomed {
		app.welcome()
	} else if *updated {
		app.notifyUpdated()
	}
	app.startUpdates()
	if *openNow || *toggle {
		procPostMessage.Call(app.window, messageToggle, 0, 0)
	}
	// For checking the menus by eye: CIRCLE_LENS_SHOW=flyout or menu opens one at start, record starts
	// recording a shortcut, update presses "Check for updates" (several, comma-separated, in order).
	for _, show := range strings.Split(os.Getenv("CIRCLE_LENS_SHOW"), ",") {
		switch show {
		case "flyout":
			procPostMessage.Call(app.window, messageTray, 0, wmLButtonUp)
		case "record":
			app.runCommand(commandRecord)
		case "menu":
			procPostMessage.Call(app.window, messageTray, 0, wmRButtonUp)
		case "update":
			app.runCommand(commandUpdate)
		}
	}

	var msg message
	for {
		result, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(result) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessage.Call(uintptr(unsafe.Pointer(&msg)))
	}
	app.finish()
}

func windowProcedure(window, msg, wParam, lParam uintptr) uintptr {
	switch uint32(msg) {
	case messageTray:
		switch uint32(lParam) & 0xFFFF {
		case wmLButtonUp:
			app.flyout.toggle()
		case wmRButtonUp:
			app.showMenu()
		case ninBalloonUserClick:
			app.balloonClicked()
		}
		return 0
	case messageUpdate:
		app.takeUpdateResults()
		return 0
	case wmTimer:
		if wParam == timerUpdate {
			app.updateTimer()
		}
		return 0
	case messageToggle:
		debugLog("toggle from %s", map[uintptr]string{toggleFromGesture: "a drawn circle", toggleFromCommand: "-toggle or -open", toggleFromShortcut: "the shortcut"}[wParam])
		app.toggle()
		return 0
	case messageFrame:
		app.drawFrame()
		return 0
	case messageZoom:
		app.zoomBy(float64(int32(wParam)) / 10)
		return 0
	case messageQuit:
		app.runCommand(commandQuit)
		return 0
	case messageRecorded:
		app.finishRecording()
		return 0
	case messageScrolled:
		if app.state.IsOpen() {
			// A notch (120) is about 100 DIPs of content going past.
			app.state.Scrolled(app.now(), float64(wParam)/120*100)
			if app.capturer != nil {
				app.capturer.animating.Store(true)
				app.capturer.requestFrame()
			}
		}
		return 0
	case wmHotKey:
		switch wParam {
		case hotkeyToggle:
			app.toggle()
		case hotkeyIn, hotkeyPadIn:
			app.zoomBy(0.2)
		case hotkeyOut, hotkeyPadOut:
			app.zoomBy(-0.2)
		}
		return 0
	case wmSettingChange:
		flushMenuTheme() // light or dark mode (or the accent colour) may have changed
		app.updateTrayIcon()
	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}
	if app != nil && uint32(msg) == app.taskbarCreated && app.taskbarCreated != 0 {
		app.addTrayIcon() // Explorer restarted
		return 0
	}
	result, _, _ := procDefWindowProc.Call(window, msg, wParam, lParam)
	return result
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

// open starts the capture thread; each capture it delivers draws one frame (drawFrame), so the lens
// moves at the display's refresh rate from the first frame on.
func (a *application) open() {
	if a.state.IsOpen() {
		return
	}
	a.dark = darkMode()
	a.state.Open(a.now(), reducedMotion())
	a.frames, a.openedAt = 0, time.Now()
	a.openingFrames, a.measuringOpening = a.openingFrames[:0], debugFile != nil
	if a.capturer == nil {
		a.capturer = startCapturer(a.window, a.settings.Radius, 1, !a.overlay.excluded)
	}
	a.capturer.animating.Store(true)
	a.setLensOpen(true)
	a.updateTrayIcon()
}

func (a *application) close() {
	if !a.state.IsOpen() {
		return
	}
	a.state.Close(a.now())
	a.setLensOpen(false)
	a.updateTrayIcon()
	a.closingFrames = a.closingFrames[:0]
	if a.capturer != nil {
		a.capturer.animating.Store(true)
	}
}

// setLensOpen tells the keys and the gesture: the lens opened or closed, which also starts the gap
// before a circle can count again (whatever opened or closed it).
func (a *application) setLensOpen(open bool) {
	a.registerKeys(open)
	if a.watcher != nil {
		a.watcher.lensOpen.Store(open)
		a.watcher.holdForGap()
	}
}

func (a *application) zoomBy(step float64) {
	if a.state.IsOpen() {
		a.state.SetZoom(a.now(), a.state.Target()+step)
		if a.capturer != nil {
			a.capturer.animating.Store(true)
		}
	}
}

// drawFrame draws the lens from the newest capture; once it has closed, it stops everything.
func (a *application) drawFrame() {
	if a.capturer == nil {
		return
	}
	a.capturer.pending.Store(false)
	// Where the pointer is now: the glass goes there, and its speed may ease the zoom out.
	var cursor point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&cursor)))
	scale := monitorScale(cursor)
	a.state.Pointer(a.now(), float64(cursor.X)/scale, float64(cursor.Y)/scale)
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
	// The glass goes over the newest capture, which may be a frame behind the pointer.
	capture.PointerX, capture.PointerY = float64(cursor.X)+0.5, float64(cursor.Y)+0.5
	theme := render.Light
	if a.dark {
		theme = render.Dark
	}
	if a.frames == 0 {
		// The first frame of an opening: start the animation from here, with the setup behind it.
		a.state.StartClock(a.now())
		frame = a.state.Frame(a.now())
	}
	a.overlay.draw(capture, frame, a.settings.Radius, theme, a.dark, a.state.Target(), a.shortcut.String(), a.settings.ConvexRim)
	// While the zoom is rising (opening, zooming in) the capture covers what is needed; while it falls
	// (closing, zooming out) capture what the lowest zoom will need, once, so no capture competes with
	// the animation for time.
	// While moving fast, capture for the zoom it eases out to, before it gets there.
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
	if a.measuringOpening {
		a.noteOpeningFrame(frame)
	}
	if debugFile != nil && !a.state.IsOpen() {
		a.closingFrames = append(a.closingFrames, time.Now())
	}

}

// finish hides the lens, stops the capture thread and hands the memory back.
func (a *application) finish() {
	if a.capturer == nil {
		return
	}
	if debugFile != nil {
		var stats runtime.MemStats
		runtime.ReadMemStats(&stats)
		debugLog("go memory while open: heap in use %.1f MB, heap from OS %.1f MB, stacks %.1f MB, all from OS %.1f MB",
			float64(stats.HeapInuse)/1e6, float64(stats.HeapSys-stats.HeapReleased)/1e6, float64(stats.StackSys)/1e6, float64(stats.Sys-stats.HeapReleased)/1e6)
	}
	a.capturer.stop()
	a.capturer = nil
	a.overlay.hide()
	a.overlay.release()
	if len(a.closingFrames) > 1 {
		debugLog("closing: %s", frameReport(a.closingFrames))
	}
	if shown := time.Since(a.openedAt); a.frames > 0 {
		debugLog("lens shown %v: %d frames, %.0f fps", shown.Round(time.Millisecond), a.frames, float64(a.frames)/shown.Seconds())
	}
	debug.FreeOSMemory()
	process, _, _ := procGetCurrentProcess.Call()
	procSetProcessWorkingSetSize.Call(process, ^uintptr(0), ^uintptr(0))
}

// registerKeys claims Ctrl +/− only while the lens is open, so they work everywhere but are handed back
// the moment it closes. (Esc is not claimed: taking it from every app while the lens is open got in the
// way; the shortcut opens and closes the lens.)
func (a *application) registerKeys(on bool) {
	if on == a.keysRegistered {
		return
	}
	a.keysRegistered = on
	keys := []struct {
		id, modifiers, key uintptr
	}{
		{hotkeyIn, modControl, vkOemPlus},
		{hotkeyOut, modControl, vkOemMinus},
		{hotkeyPadIn, modControl, vkAdd},
		{hotkeyPadOut, modControl, vkSubtract},
	}
	for _, key := range keys {
		if on {
			procRegisterHotKey.Call(a.window, key.id, key.modifiers, key.key)
		} else {
			procUnregisterHotKey.Call(a.window, key.id)
		}
	}
}

/* ---------- settings ---------- */

func (a *application) applySettings() {
	if a.settings.Enabled {
		if a.watcher == nil {
			a.watcher = startWatcher(a.window, a.instance, a.settings.Loops)
		}
		a.watcher.loops.Store(math.Float64bits(a.settings.Loops))
		a.watcher.gap.Store(math.Float64bits(a.settings.Gap))
		a.watcher.lensOpen.Store(a.state.IsOpen())
	} else {
		a.stopWatching()
	}
	a.state.SetZoomOutWhenFast(a.settings.ZoomOutWhenFast)
	if a.capturer != nil {
		a.capturer.radiusDip.Store(math.Float64bits(a.settings.Radius))
	}
	a.updateTrayIcon()
}

func (a *application) stopWatching() {
	if a.watcher != nil {
		a.watcher.stop()
		a.watcher = nil
		watcher = nil
	}
}

func (a *application) save() {
	if a.settingsPath != "" {
		_ = settings.Save(a.settingsPath, a.settings)
	}
	a.applySettings()
}

func fail(text string) {
	procMessageBox.Call(0, uintptr(unsafe.Pointer(wide(text))), uintptr(unsafe.Pointer(wide("Circle Lens"))), mbIconError)
	os.Exit(1)
}

// noteOpeningFrame records frame times until the lens has opened, then logs how smooth it was.
func (a *application) noteOpeningFrame(frame lens.Frame) {
	a.openingFrames = append(a.openingFrames, time.Now())
	if frame.GlassScale != 1 || frame.Alpha != 1 || len(a.openingFrames) < 2 {
		return
	}
	debugLog("opening: %s", frameReport(a.openingFrames))
	a.measuringOpening = false
}

// handOver asks the copy already running to quit and waits for it. An older copy running as
// administrator cannot hear a copy that is not; then, with the person's say-so, a short-lived
// elevated copy of this exe (-quit) passes the message on.
func handOver(running uintptr) bool {
	if posted, _, _ := procPostMessage.Call(running, messageQuit, 0, 0); posted != 0 && waitUntilGone(3*time.Second) {
		return true
	}
	answer, _, _ := procMessageBox.Call(0,
		uintptr(unsafe.Pointer(wide("Another Circle Lens is running as administrator, so this one can't close it by itself.\n\nReplace it with this one? Windows will ask for permission."))),
		uintptr(unsafe.Pointer(wide("Circle Lens"))), mbYesNo|mbIconQuestion)
	if answer != idYes {
		return false
	}
	executable, err := os.Executable()
	if err != nil {
		return false
	}
	if launched, _, _ := procShellExecute.Call(0, uintptr(unsafe.Pointer(wide("runas"))), uintptr(unsafe.Pointer(wide(executable))),
		uintptr(unsafe.Pointer(wide("-quit"))), 0, swHide); launched <= 32 {
		return false // permission refused
	}
	return waitUntilGone(15 * time.Second)
}

// waitUntilGone waits for the running copy's window to disappear.
func waitUntilGone(limit time.Duration) bool {
	for deadline := time.Now().Add(limit); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if running, _, _ := procFindWindow.Call(uintptr(unsafe.Pointer(wide(instance+"App"))), 0); running == 0 {
			return true
		}
	}
	return false
}

// holdInstanceLock takes the single-instance lock, giving a copy that is just quitting a moment to
// let go of it.
func holdInstanceLock() bool {
	name := uintptr(unsafe.Pointer(wide(`Local\` + instance + ".Desktop")))
	for attempt := 0; attempt < 40; attempt++ {
		handle, _, err := procCreateMutex.Call(0, 0, name)
		if err != syscall.Errno(errorAlreadyExists) {
			return handle != 0
		}
		procCloseHandle.Call(handle)
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

// frameReport says how smooth a run of frames was: how many, at what rate, and the longest gap.
func frameReport(frames []time.Time) string {
	var longest time.Duration
	for i := 1; i < len(frames); i++ {
		longest = max(longest, frames[i].Sub(frames[i-1]))
	}
	span := frames[len(frames)-1].Sub(frames[0])
	return fmt.Sprintf("%d frames in %v (%.0f fps), longest gap %v", len(frames), span.Round(time.Millisecond),
		float64(len(frames)-1)/span.Seconds(), longest.Round(100*time.Microsecond))
}
