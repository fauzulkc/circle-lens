//go:build darwin

package main

// macOS. AppKit has no stable way in from Go without cgo, so this backend uses it (macOS only; Windows
// and Linux stay cgo-free). desktop_darwin.m does the AppKit work:
//
//   - The lens is a borderless, click-through, non-activating panel above everything.
//   - ScreenCaptureKit copies the screen around the pointer and leaves this app's windows out, so the
//     lens is live, as on Windows. It needs the Screen Recording permission, which macOS asks for once.
//   - The pointer comes from a global event monitor for mouse events, which needs no permission.
//   - A double-tap shortcut listens to modifier keys through a listen-only event tap, which needs Input
//     Monitoring. It never keeps, logs or sends which other key was pressed: only that one was. A key
//     combination is a Carbon hot key, which needs nothing.
//   - The menu bar item's menu is native.
//
// Global pixels here are points times the backing scale of the display under the pointer, so they
// match that display's pixels.

/*
#cgo CFLAGS: -x objective-c -fobjc-arc -mmacosx-version-min=14.0 -Wno-deprecated-declarations
#cgo LDFLAGS: -mmacosx-version-min=14.0 -framework Cocoa -framework Carbon -framework QuartzCore -framework ScreenCaptureKit -framework CoreMedia -framework CoreText -framework ApplicationServices
#include <stdlib.h>
#include "desktop_darwin.h"
*/
import "C"

import (
	"encoding/json"
	"fmt"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"

	"circlelens/internal/icon"
	"circlelens/internal/render"
)

// AppKit must run on the main thread: keep the main goroutine there from the start.
func init() { runtime.LockOSThread() }

type darwinSystem struct {
	posted     sync.Map // id → func, waiting for the main thread
	nextPosted atomic.Uintptr

	grabbed     []func()
	menuActions map[int]func()
	askedScreen bool
}

var mac *darwinSystem

func newSystem() (system, error) {
	mac = &darwinSystem{menuActions: map[int]func(){}}
	C.clStart()
	return mac, nil
}

func (s *darwinSystem) run(ready func()) {
	s.post(ready)
	C.clRun()
}

func (s *darwinSystem) stop() { C.clStop() }

func (s *darwinSystem) post(f func()) {
	id := s.nextPosted.Add(1)
	s.posted.Store(id, f)
	C.clPost(C.uintptr_t(id))
}

//export goRunPosted
func goRunPosted(id C.uintptr_t) {
	if f, ok := mac.posted.LoadAndDelete(uintptr(id)); ok {
		f.(func())()
	}
}

/* ---------- the screen ---------- */

func points() (float64, float64) {
	var x, y C.double
	C.clPointer(&x, &y)
	return float64(x), float64(y)
}

func scaleAt(x, y float64) float64 { return float64(C.clScaleAt(C.double(x), C.double(y))) }

func (s *darwinSystem) pointer() (int, int, bool) {
	x, y := points()
	scale := scaleAt(x, y)
	return int(math.Round(x * scale)), int(math.Round(y * scale)), true
}

func (s *darwinSystem) scale() float64 { return scaleAt(points()) }

func (s *darwinSystem) bounds() (left, top, right, bottom int) {
	x, y := points()
	scale := scaleAt(x, y)
	var l, t, w, h C.double
	C.clScreenAt(C.double(x), C.double(y), &l, &t, &w, &h)
	left, top = int(math.Round(float64(l)*scale)), int(math.Round(float64(t)*scale))
	return left, top, left + int(math.Round(float64(w)*scale)), top + int(math.Round(float64(h)*scale))
}

func (s *darwinSystem) refreshRate() float64 { return float64(C.clRefreshRate()) }
func (s *darwinSystem) excludesLens() bool   { return true }

func (s *darwinSystem) grab(left, top, width, height int, into []byte) bool {
	scale := s.scale()
	return C.clGrab(C.double(float64(left)/scale), C.double(float64(top)/scale), C.double(float64(width)/scale), C.double(float64(height)/scale),
		C.int(width), C.int(height), (*C.uint8_t)(unsafe.Pointer(&into[0]))) != 0
}

/* ---------- the lens window ---------- */

func (s *darwinSystem) showLens(canvas *render.Image, left, top int) {
	if !s.askedScreen {
		s.askedScreen = true
		if C.clScreenAccess(1) == 0 {
			debugLog("no Screen Recording permission yet: System Settings → Privacy & Security → Screen & System Audio Recording")
		}
	}
	scale := s.scale()
	C.clShowLens((*C.uint8_t)(unsafe.Pointer(&canvas.Pixels[0])), C.int(canvas.Width), C.int(canvas.Height),
		C.double(float64(left)/scale), C.double(float64(top)/scale), C.double(scale))
}

func (s *darwinSystem) hideLens() { C.clHideLens() }

func (s *darwinSystem) text(line string, pixels float64) ([]byte, int, int) {
	text := C.CString(line)
	defer C.free(unsafe.Pointer(text))
	var out *C.uint8_t
	var width, height C.int
	if C.clText(text, C.double(pixels), &out, &width, &height) == 0 {
		return nil, 0, 0
	}
	defer C.clFree(unsafe.Pointer(out))
	return C.GoBytes(unsafe.Pointer(out), width*height), int(width), int(height)
}

func (s *darwinSystem) dark() bool {
	switch os.Getenv("CIRCLE_LENS_THEME") {
	case "light":
		return false
	case "dark":
		return true
	}
	return C.clDark() != 0
}

func (s *darwinSystem) reducedMotion() bool { return C.clReducedMotion() != 0 }

/* ---------- input ---------- */

func (s *darwinSystem) watchPointer(on bool) { C.clWatchPointer(boolInt(on)) }

func (s *darwinSystem) watchModifiers(on bool) {
	if C.clWatchModifiers(boolInt(on)) == 0 {
		debugLog("no Input Monitoring permission yet, so the double-tap shortcut can't be seen")
	}
}

func (s *darwinSystem) grabKeys(grabs []keyGrab) {
	var keycodes, modifiers []C.uint32_t
	s.grabbed = s.grabbed[:0]
	for _, grab := range grabs {
		code, ok := macKeycodes[grab.key.Key]
		if !ok {
			continue
		}
		var mask uint32
		for _, held := range []struct {
			on   bool
			mask uint32
		}{{grab.key.Win, 0x100}, {grab.key.Shift, 0x200}, {grab.key.Alt, 0x800}, {grab.key.Ctrl, 0x1000}} { // cmd, shift, option, control
			if held.on {
				mask |= held.mask
			}
		}
		keycodes = append(keycodes, C.uint32_t(code))
		modifiers = append(modifiers, C.uint32_t(mask))
		s.grabbed = append(s.grabbed, grab.action)
	}
	if len(keycodes) == 0 {
		C.clGrabKeys(0, nil, nil)
		return
	}
	C.clGrabKeys(C.int(len(keycodes)), &keycodes[0], &modifiers[0])
}

// grabZoomWheel: macOS can't take the wheel from other apps without Accessibility, so Ctrl + wheel
// zooms the lens (goScrolled) without being taken.
func (s *darwinSystem) grabZoomWheel(on bool) {}

func boolInt(on bool) C.int {
	if on {
		return 1
	}
	return 0
}

//export goPointerMoved
func goPointerMoved(x, y C.double) {
	scale := scaleAt(float64(x), float64(y))
	app.pointerMoved(int(math.Round(float64(x)*scale)), int(math.Round(float64(y)*scale)))
}

//export goButton
func goButton(down C.int) { app.button(down != 0) }

//export goScrolled
func goScrolled(notches C.double, control C.int) {
	if control != 0 && app.state.IsOpen() {
		step := 0.1
		if notches < 0 {
			step = -0.1
		}
		app.zoomBy(step)
		return
	}
	app.scrolled(float64(notches))
}

//export goModifier
func goModifier(which, down C.int) {
	app.modifier([]string{"", "Shift", "Ctrl", "Alt"}[which], down != 0)
}

//export goHotKey
func goHotKey(index C.int) {
	if i := int(index); i >= 0 && i < len(mac.grabbed) {
		mac.grabbed[i]()
	}
}

// macKeycodes maps shortcut key names (internal/shortcut) to macOS virtual key codes.
var macKeycodes = func() map[string]uint32 {
	codes := map[string]uint32{
		"A": 0x00, "S": 0x01, "D": 0x02, "F": 0x03, "H": 0x04, "G": 0x05, "Z": 0x06, "X": 0x07, "C": 0x08, "V": 0x09,
		"B": 0x0B, "Q": 0x0C, "W": 0x0D, "E": 0x0E, "R": 0x0F, "Y": 0x10, "T": 0x11, "1": 0x12, "2": 0x13, "3": 0x14,
		"4": 0x15, "6": 0x16, "5": 0x17, "=": 0x18, "9": 0x19, "7": 0x1A, "-": 0x1B, "8": 0x1C, "0": 0x1D, "]": 0x1E,
		"O": 0x1F, "U": 0x20, "[": 0x21, "I": 0x22, "P": 0x23, "Enter": 0x24, "L": 0x25, "J": 0x26, "'": 0x27, "K": 0x28,
		";": 0x29, "\\": 0x2A, ",": 0x2B, "/": 0x2C, "N": 0x2D, "M": 0x2E, ".": 0x2F, "Tab": 0x30, "Space": 0x31, "`": 0x32,
		"Backspace": 0x33, "Esc": 0x35, "Home": 0x73, "PageUp": 0x74, "Delete": 0x75, "End": 0x77, "PageDown": 0x79,
		"Left": 0x7B, "Right": 0x7C, "Down": 0x7D, "Up": 0x7E, "Insert": 0x72,
		"Num.": 0x41, "Num*": 0x43, "Num+": 0x45, "Num/": 0x4B, "Num-": 0x4E,
		"Num0": 0x52, "Num1": 0x53, "Num2": 0x54, "Num3": 0x55, "Num4": 0x56, "Num5": 0x57, "Num6": 0x58, "Num7": 0x59, "Num8": 0x5B, "Num9": 0x5C,
	}
	for i, code := range []uint32{0x7A, 0x78, 0x63, 0x76, 0x60, 0x61, 0x62, 0x64, 0x65, 0x6D, 0x67, 0x6F, 0x69, 0x6B, 0x71, 0x6A, 0x40, 0x4F, 0x50, 0x5A} {
		codes[fmt.Sprintf("F%d", i+1)] = code
	}
	return codes
}()

/* ---------- the menu bar item ---------- */

type macMenuItem struct {
	ID        int           `json:"id"`
	Label     string        `json:"label"`
	Enabled   bool          `json:"enabled"`
	Checked   bool          `json:"checked"`
	Separator bool          `json:"separator"`
	Children  []macMenuItem `json:"children,omitempty"`
}

func (s *darwinSystem) setMenu(items []menuItem, open bool) {
	s.menuActions = map[int]func(){}
	next := 1
	var convert func(items []menuItem) []macMenuItem
	convert = func(items []menuItem) []macMenuItem {
		out := []macMenuItem{}
		for _, item := range items {
			label := item.label
			if item.detail != "" {
				label += " (" + item.detail + ")"
			}
			entry := macMenuItem{ID: next, Label: label, Enabled: !item.disabled, Checked: item.checked, Separator: item.separator}
			if item.action != nil {
				s.menuActions[next] = item.action
			}
			next++
			entry.Children = convert(item.children)
			out = append(out, entry)
		}
		return out
	}
	data, _ := json.Marshal(convert(items))
	text := C.CString(string(data))
	defer C.free(unsafe.Pointer(text))
	// A template image: black where the mark is, which the menu bar recolours for light or dark.
	style := icon.Style{Ink: color.NRGBA{A: 0xff}, Paused: !app.settings.Enabled}
	if open {
		style.Glass, style.GlassInk = color.NRGBA{A: 0xff}, color.NRGBA{A: 0x40}
	}
	const side = 36 // 18 points at 2×
	picture := icon.Draw(side, style)
	C.clSetMenu(text, (*C.uint8_t)(unsafe.Pointer(&picture.Pix[0])), side)
}

//export goMenuClicked
func goMenuClicked(id C.int) {
	if action := mac.menuActions[int(id)]; action != nil {
		action()
	}
}

func (s *darwinSystem) openAddress(address string) {
	text := C.CString(address)
	defer C.free(unsafe.Pointer(text))
	C.clOpenAddress(text)
}

/* ---------- start at login, install ---------- */

// The start-up entry is a LaunchAgent: a plist in ~/Library/LaunchAgents, which needs no signing.
func launchAgentLabel() string {
	if instance == "CircleLens" {
		return "com.fauzul.circlelens"
	}
	return "com.fauzul." + strings.ToLower(instance)
}

func launchAgentPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", launchAgentLabel()+".plist")
}

func (s *darwinSystem) startsAtLogin() bool {
	_, err := os.Stat(launchAgentPath())
	return err == nil
}

func (s *darwinSystem) setStartAtLogin(on bool) error {
	if !on {
		err := os.Remove(launchAgentPath())
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	program, err := os.Executable()
	if err != nil {
		return err
	}
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key><string>` + launchAgentLabel() + `</string>
	<key>ProgramArguments</key><array><string>` + xmlEscape(program) + `</string></array>
	<key>RunAtLoad</key><true/>
	<key>ProcessType</key><string>Interactive</string>
</dict>
</plist>
`
	if err := os.MkdirAll(filepath.Dir(launchAgentPath()), 0o755); err != nil {
		return err
	}
	return os.WriteFile(launchAgentPath(), []byte(plist), 0o644)
}

func xmlEscape(text string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(text)
}

// install: a Mac app installs by being moved to Applications, so this only says so.
func install() error {
	fmt.Println("Move Circle Lens.app to your Applications folder, then open it. Its menu has \"Start at login\".")
	return nil
}

func uninstall() error {
	if err := (&darwinSystem{}).setStartAtLogin(false); err != nil {
		return err
	}
	fmt.Println("Circle Lens won't start at login any more. Move Circle Lens.app to the Bin to remove it.")
	return nil
}
