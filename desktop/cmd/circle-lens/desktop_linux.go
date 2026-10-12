//go:build linux

package main

// Linux, in an X11 session (x11_linux.go speaks the protocol):
//
//   - The pointer comes from XInput2 raw events on the root window. They arrive only while the mouse
//     moves, so nothing runs while it's still.
//   - The lens is an override-redirect window, click-through (an empty SHAPE input region).
//   - With a compositing manager running (GNOME, KDE, Cinnamon, Xfce...), the lens has per-pixel alpha
//     and stays live: X11 can't leave one window out of a screen capture, but under a compositor every
//     window keeps its contents offscreen, so a capture reads the windows under the lens one by one,
//     bottom to top, skipping the lens itself.
//   - Without one, the lens is cut to its circle and shows a still taken as it opens.
//
// The double-tap shortcut watches modifier keys through raw key events, and only while that shortcut is
// chosen. It never keeps, logs or sends which other key was pressed: any other key only cancels a tap.

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"circlelens/internal/render"
	"circlelens/internal/shortcut"
)

type x11System struct {
	x      *xConn
	posted chan func()
	quit   chan struct{}

	lens         uint32
	gc           uint32
	argb         bool // the lens window has per-pixel alpha (a compositor blends it)
	live         bool // captures leave the lens out
	lensShown    bool
	lensLeft     int
	lensTop      int
	lensSide     int
	lensShape    []int
	scaleValue   float64
	wallpaper    atomic32
	keysyms      []uint32
	perKeycode   int
	rawEvents    map[int]bool
	grabbed      []grabbedKey
	zoomWheel    bool
	tray         *tray
	fontOnce     sync.Once
	fontFace     *textFace
	appearanceAt time.Time
	darkTheme    bool
	reducedAnim  bool
}

type grabbedKey struct {
	keycode   byte
	modifiers uint16
	action    func()
}

// atomic32 is a uint32 read by the capture thread and set by the main loop.
type atomic32 struct {
	sync.Mutex
	value uint32
}

func (a *atomic32) load() uint32   { a.Lock(); defer a.Unlock(); return a.value }
func (a *atomic32) store(v uint32) { a.Lock(); a.value = v; a.Unlock() }

func newSystem() (system, error) {
	if os.Getenv("XDG_SESSION_TYPE") == "wayland" && os.Getenv("CIRCLE_LENS_X11") == "" {
		return nil, errors.New("this preview works in an X11 session. On GNOME, use the Circle Lens GNOME extension; on other Wayland desktops, log in with an X11 session for now")
	}
	x, err := connectX()
	if err != nil {
		return nil, err
	}
	s := &x11System{x: x, posted: make(chan func(), 64), quit: make(chan struct{}), rawEvents: map[int]bool{}}
	compositing := x.compositing() || os.Getenv("CIRCLE_LENS_X11_LIVE") == "1"
	s.argb = compositing && x.argbVisual != 0
	s.live = s.argb && x.extension("Composite").present
	s.scaleValue = x.dpiScale()
	const keyPress, buttonPress = 0x1, 0x4
	s.lens = x.createWindow(0, 0, 1, 1, s.argb, 0)
	x.setInputShape(s.lens)
	x.changeProperty(s.lens, x.internAtom("WM_CLASS"), 31, 8, []byte(instance+"\x00"+instance+"\x00"))
	s.gc = x.createGC(s.lens)
	s.keysyms, s.perKeycode = x.keyboardMapping()
	if _, value, ok := x.getProperty(x.root, x.internAtom("_XROOTPMAP_ID"), 20, 1); ok && len(value) >= 4 { // PIXMAP
		s.wallpaper.store(order.Uint32(value))
	}
	x.sync()
	debugLog("X11: %dx%d, scale %.2f, compositing %v, live lens %v, refresh %.0f Hz", x.width, x.height, s.scaleValue, compositing, s.live, x.refreshRate())
	return s, nil
}

/* ---------- the main loop ---------- */

func (s *x11System) run(ready func()) {
	s.tray = startTray(s)
	ready()
	for {
		select {
		case <-s.quit:
			return
		case f := <-s.posted:
			f()
		case event := <-s.x.events:
			s.handle(event)
		case <-s.x.closed:
			return
		}
	}
}

func (s *x11System) stop() {
	select {
	case <-s.quit:
	default:
		close(s.quit)
	}
	if s.tray != nil {
		s.tray.close()
	}
}

func (s *x11System) post(f func()) {
	select {
	case s.posted <- f:
	case <-s.quit:
	}
}

// handle reads one event on the main loop.
func (s *x11System) handle(event []byte) {
	switch event[0] & 0x7F {
	case 2: // KeyPress, from a key grab
		keycode, state := event[1], order.Uint16(event[28:])&(shiftMask|controlMask|mod1Mask|mod4Mask)
		for _, grab := range s.grabbed {
			if grab.keycode == keycode && grab.modifiers == state {
				grab.action()
				return
			}
		}
	case 4: // ButtonPress, from the Ctrl + wheel grab
		switch event[1] {
		case 4:
			app.zoomBy(0.1)
		case 5:
			app.zoomBy(-0.1)
		}
	case 35: // GenericEvent: XInput2 raw events
		xi := s.x.extension("XInputExtension")
		if event[1] != xi.opcode || len(event) < 32 {
			return
		}
		detail := order.Uint32(event[16:])
		switch int(order.Uint16(event[8:])) {
		case xiRawMotion:
			s.drainMotion()
		case xiRawButtonPress:
			switch {
			case detail >= 4 && detail <= 7:
				app.scrolled(1)
			default:
				app.button(true)
			}
		case xiRawButtonRelease:
			if detail < 4 || detail > 7 {
				app.button(false)
			}
		case xiRawKeyPress:
			app.modifier(s.modifierName(detail), true)
		case xiRawKeyRelease:
			app.modifier(s.modifierName(detail), false)
		}
	}
}

// drainMotion reads the pointer once for a burst of motion events (a fast mouse sends hundreds a second).
func (s *x11System) drainMotion() {
	var others [][]byte
	for more := true; more; {
		select {
		case event := <-s.x.events:
			if event[0]&0x7F == 35 && len(event) >= 10 && int(order.Uint16(event[8:])) == xiRawMotion {
				continue
			}
			others = append(others, event)
		default:
			more = false
		}
	}
	if x, y, _, ok := s.x.queryPointer(); ok {
		app.pointerMoved(x, y)
	}
	for _, event := range others {
		s.handle(event)
	}
}

// modifierName says which modifier a keycode is, or "" for any other key (which is all the app learns).
func (s *x11System) modifierName(keycode uint32) string {
	index := (int(keycode) - int(s.x.minKeycode)) * s.perKeycode
	if s.perKeycode == 0 || index < 0 || index >= len(s.keysyms) {
		return ""
	}
	switch s.keysyms[index] {
	case 0xFFE1, 0xFFE2:
		return "Shift"
	case 0xFFE3, 0xFFE4:
		return "Ctrl"
	case 0xFFE9, 0xFFEA:
		return "Alt"
	}
	return ""
}

/* ---------- input ---------- */

func (s *x11System) watchPointer(on bool) {
	for _, event := range []int{xiRawMotion, xiRawButtonPress, xiRawButtonRelease} {
		s.rawEvents[event] = on
	}
	s.selectRaw()
}

func (s *x11System) watchModifiers(on bool) {
	s.rawEvents[xiRawKeyPress], s.rawEvents[xiRawKeyRelease] = on, on
	s.selectRaw()
}

func (s *x11System) selectRaw() {
	var events []int
	for event, on := range s.rawEvents {
		if on {
			events = append(events, event)
		}
	}
	if !s.x.selectRawEvents(events...) {
		debugLog("X11: no XInput2, so the circle gesture can't be seen")
	}
}

func (s *x11System) grabKeys(grabs []keyGrab) {
	for _, old := range s.grabbed {
		s.x.ungrabKey(old.keycode, old.modifiers)
	}
	s.grabbed = s.grabbed[:0]
	for _, grab := range grabs {
		keysym, ok := keysymFor(grab.key.Key)
		if !ok {
			continue
		}
		keycode := s.x.keycodeFor(s.keysyms, s.perKeycode, keysym)
		if keycode == 0 {
			continue
		}
		var modifiers uint16
		for _, held := range []struct {
			on   bool
			mask uint16
		}{{grab.key.Ctrl, controlMask}, {grab.key.Alt, mod1Mask}, {grab.key.Shift, shiftMask}, {grab.key.Win, mod4Mask}} {
			if held.on {
				modifiers |= held.mask
			}
		}
		s.x.grabKey(keycode, modifiers)
		s.grabbed = append(s.grabbed, grabbedKey{keycode, modifiers, grab.action})
	}
}

func (s *x11System) grabZoomWheel(on bool) {
	if on == s.zoomWheel {
		return
	}
	s.zoomWheel = on
	for _, button := range []byte{4, 5} {
		if on {
			s.x.grabButton(button, controlMask)
		} else {
			s.x.ungrabButton(button, controlMask)
		}
	}
}

// keysymFor maps a shortcut key name (internal/shortcut) to its X keysym.
func keysymFor(name string) (uint32, bool) {
	if len(name) == 1 {
		c := name[0]
		switch {
		case c >= 'A' && c <= 'Z':
			return uint32(c - 'A' + 'a'), true
		case c >= '0' && c <= '9', strings.ContainsRune(";=,-./`[\\]'", rune(c)):
			return uint32(c), true
		}
	}
	if code, ok := shortcut.KeyCode(name); ok && code >= 0x70 && code <= 0x87 {
		return 0xFFBE + code - 0x70, true // F1-F24
	}
	keysym, ok := map[string]uint32{
		"Space": 0x20, "PageUp": 0xFF55, "PageDown": 0xFF56, "End": 0xFF57, "Home": 0xFF50,
		"Left": 0xFF51, "Up": 0xFF52, "Right": 0xFF53, "Down": 0xFF54, "Insert": 0xFF63, "Delete": 0xFFFF,
		"Pause": 0xFF13, "ScrollLock": 0xFF14, "Backspace": 0xFF08, "Enter": 0xFF0D, "Tab": 0xFF09, "Esc": 0xFF1B,
		"Num0": 0xFFB0, "Num1": 0xFFB1, "Num2": 0xFFB2, "Num3": 0xFFB3, "Num4": 0xFFB4,
		"Num5": 0xFFB5, "Num6": 0xFFB6, "Num7": 0xFFB7, "Num8": 0xFFB8, "Num9": 0xFFB9,
		"Num*": 0xFFAA, "Num+": 0xFFAB, "Num-": 0xFFAD, "Num.": 0xFFAE, "Num/": 0xFFAF,
	}[name]
	return keysym, ok
}

/* ---------- the screen ---------- */

func (s *x11System) pointer() (int, int, bool) {
	x, y, _, ok := s.x.queryPointer()
	return x, y, ok
}

func (s *x11System) scale() float64                         { return s.scaleValue }
func (s *x11System) bounds() (left, top, right, bottom int) { return 0, 0, s.x.width, s.x.height }
func (s *x11System) refreshRate() float64                   { return s.x.refreshRate() }
func (s *x11System) excludesLens() bool                     { return s.live }

// grab copies the screen: the root window when the lens is left out anyway (a still, taken before it
// shows), or each window under the area, bottom to top, when it's live.
func (s *x11System) grab(left, top, width, height int, into []byte) bool {
	if !s.live {
		return s.copyImage(s.x.getImage(s.x.root, left, top, width, height), into, width, 0, 0, width, height, false)
	}
	for i := 0; i < len(into); i += 4 {
		into[i], into[i+1], into[i+2], into[i+3] = 0, 0, 0, 255
	}
	if wallpaper := s.wallpaper.load(); wallpaper != 0 {
		s.copyImage(s.x.getImage(wallpaper, left, top, width, height), into, width, 0, 0, width, height, false)
	}
	children := s.x.queryTree(s.x.root)
	type candidate struct {
		window           uint32
		attributes, size xCookie
	}
	candidates := make([]candidate, 0, len(children))
	for _, child := range children {
		if child == s.lens {
			continue
		}
		candidates = append(candidates, candidate{child, s.x.send(3, 0, u32(child), true), s.x.send(14, 0, u32(child), true)})
	}
	type layer struct {
		image                   xCookie
		atX, atY, width, height int
	}
	var layers []layer
	for _, c := range candidates {
		attributes, err1 := c.attributes.wait()
		geometry, err2 := c.size.wait()
		if err1 != nil || err2 != nil || attributes[26] != 2 || order.Uint16(attributes[12:]) != 1 { // viewable, InputOutput
			continue
		}
		border := int(order.Uint16(geometry[20:]))
		windowX, windowY := int(int16(order.Uint16(geometry[12:])))+border, int(int16(order.Uint16(geometry[14:])))+border
		windowWidth, windowHeight := int(order.Uint16(geometry[16:])), int(order.Uint16(geometry[18:]))
		x0, y0 := max(left, windowX), max(top, windowY)
		x1, y1 := min(left+width, windowX+windowWidth), min(top+height, windowY+windowHeight)
		if x1 <= x0 || y1 <= y0 {
			continue
		}
		layers = append(layers, layer{s.x.getImage(c.window, x0-windowX, y0-windowY, x1-x0, y1-y0), x0 - left, y0 - top, x1 - x0, y1 - y0})
	}
	for _, l := range layers {
		s.copyImage(l.image, into, width, l.atX, l.atY, l.width, l.height, true)
	}
	return true
}

// copyImage puts a GetImage reply into the capture at atX, atY. Depth-32 windows (their own alpha) are
// blended over what is below; others are opaque.
func (s *x11System) copyImage(cookie xCookie, into []byte, stride, atX, atY, width, height int, blend bool) bool {
	reply, err := cookie.wait()
	if err != nil || len(reply) < 32+width*height*4 {
		return false
	}
	depth, pixels := reply[1], reply[32:]
	if depth != 24 && depth != 32 {
		return false
	}
	for y := 0; y < height; y++ {
		source := pixels[y*width*4 : (y+1)*width*4]
		target := into[((atY+y)*stride+atX)*4:]
		if depth == 24 || !blend {
			for x := 0; x < width; x++ {
				target[x*4], target[x*4+1], target[x*4+2], target[x*4+3] = source[x*4], source[x*4+1], source[x*4+2], 255
			}
			continue
		}
		for x := 0; x < width; x++ {
			alpha := int(source[x*4+3])
			for c := 0; c < 3; c++ {
				target[x*4+c] = byte(int(source[x*4+c]) + int(target[x*4+c])*(255-alpha)/255)
			}
		}
	}
	return true
}

/* ---------- the lens window ---------- */

func (s *x11System) showLens(canvas *render.Image, left, top int) {
	side := canvas.Width
	if side != s.lensSide || left != s.lensLeft || top != s.lensTop || !s.lensShown {
		s.x.configureWindow(s.lens, left, top, side, canvas.Height)
		s.lensSide, s.lensLeft, s.lensTop = side, left, top
	}
	if !s.argb {
		s.cutToShape(canvas)
	}
	depth := byte(32)
	if !s.argb {
		depth = s.x.rootDepth
	}
	s.x.putImage(s.lens, s.gc, depth, canvas.Pixels, canvas.Width, canvas.Height, 0, 0)
	if !s.lensShown {
		s.x.mapWindow(s.lens)
		s.lensShown = true
	}
}

// cutToShape, without a compositor, trims the window to the canvas's opaque part, row by row.
func (s *x11System) cutToShape(canvas *render.Image) {
	var rectangles []int
	for y := 0; y < canvas.Height; y++ {
		start := -1
		for x := 0; x <= canvas.Width; x++ {
			solid := x < canvas.Width && canvas.Pixels[(y*canvas.Width+x)*4+3] >= 128
			if solid && start < 0 {
				start = x
			} else if !solid && start >= 0 {
				rectangles = append(rectangles, start, y, x-start, 1)
				start = -1
			}
		}
	}
	if !equalInts(rectangles, s.lensShape) {
		s.x.setBoundingShape(s.lens, rectangles)
		s.lensShape = rectangles
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (s *x11System) hideLens() {
	if s.lensShown {
		s.x.unmapWindow(s.lens)
		s.lensShown = false
	}
	s.lensShape = nil
}

/* ---------- appearance ---------- */

func (s *x11System) dark() bool {
	switch os.Getenv("CIRCLE_LENS_THEME") {
	case "light":
		return false
	case "dark":
		return true
	}
	s.readAppearance()
	return s.darkTheme
}

func (s *x11System) reducedMotion() bool {
	s.readAppearance()
	return s.reducedAnim
}

// readAppearance asks GNOME's settings (also set by Cinnamon, Budgie and others) for dark mode and
// animations, at most once every few seconds.
func (s *x11System) readAppearance() {
	if time.Since(s.appearanceAt) < 5*time.Second {
		return
	}
	s.appearanceAt = time.Now()
	read := func(key string) string {
		out, err := exec.Command("gsettings", "get", "org.gnome.desktop.interface", key).Output()
		if err != nil {
			return ""
		}
		return strings.Trim(strings.TrimSpace(string(out)), "'")
	}
	s.darkTheme = read("color-scheme") == "prefer-dark" || strings.Contains(strings.ToLower(read("gtk-theme")), "dark")
	s.reducedAnim = read("enable-animations") == "false"
}

func (s *x11System) openAddress(address string) {
	command := exec.Command("xdg-open", address)
	if command.Start() == nil {
		go command.Wait()
	}
}
