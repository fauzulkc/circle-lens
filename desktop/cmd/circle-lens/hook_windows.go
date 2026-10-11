//go:build windows

package main

// The gesture runs on its own OS thread. A low-level mouse hook holds up every mouse event on the
// system until it returns, so it must never wait behind the UI thread, which can stall for half a
// second while Direct3D starts. The hook thread only feeds the detector and posts messages to the UI.

import (
	"math"
	"runtime"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"circlelens/internal/gesture"
)

type mouseWatcher struct {
	window   uintptr // the UI window that receives toggle and zoom messages
	threadID atomic.Uint32
	loops    atomic.Uint64 // float64 bits of the circles needed
	gap      atomic.Uint64 // float64 bits of the gap after an open or close, in seconds
	// holdUntil (milliseconds on this watcher's clock, as float64 bits): set by the UI thread when the
	// lens opens or closes some other way, so the gap follows that too.
	holdUntil atomic.Uint64
	// Set by the UI thread: Ctrl + wheel zooms only while the lens is open.
	lensOpen atomic.Bool
	started  time.Time

	// Owned by the hook thread.
	detector    *gesture.Detector
	buttonsHeld int
}

var (
	watcher           *mouseWatcher
	mouseHookCallback = syscall.NewCallback(mouseHook) // made once: Windows callbacks are finite
)

// startWatcher installs the mouse hook on a new locked thread and returns once it is in place.
func startWatcher(window, module uintptr, loops float64) *mouseWatcher {
	w := &mouseWatcher{window: window, started: time.Now(), detector: gesture.New(loops)}
	w.loops.Store(math.Float64bits(loops))
	watcher = w
	ready := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		id, _, _ := procGetCurrentThreadId.Call()
		w.threadID.Store(uint32(id))
		hook, _, _ := procSetWindowsHookEx.Call(whMouseLL, mouseHookCallback, module, 0)
		close(ready)
		var msg message
		for {
			if result, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0); int32(result) <= 0 {
				break
			}
		}
		procUnhookWindowsHookEx.Call(hook)
	}()
	<-ready
	return w
}

func (w *mouseWatcher) stop() { procPostThreadMessage.Call(uintptr(w.threadID.Load()), wmQuit, 0, 0) }

func mouseHook(code, wParam, lParam uintptr) uintptr {
	// The hook hands a pointer to its MSLLHOOKSTRUCT in lParam; reading it is the documented contract.
	if int32(code) >= 0 && watcher != nil && watcher.onMouse(uint32(wParam), (*mouseHookInfo)(unsafe.Pointer(lParam))) {
		return 1 // swallowed (Ctrl + wheel while the lens is open)
	}
	next, _, _ := procCallNextHookEx.Call(0, code, wParam, lParam)
	return next
}

// onMouse handles one mouse event. It must return quickly: anything slow is posted to the UI thread.
func (w *mouseWatcher) onMouse(event uint32, info *mouseHookInfo) bool {
	switch event {
	case wmLButtonDown, wmRButtonDown, wmMButtonDown, wmXButtonDown:
		w.buttonsHeld++
		w.detector.Reset()
	case wmLButtonUp, wmRButtonUp, wmMButtonUp, wmXButtonUp:
		w.buttonsHeld = max(0, w.buttonsHeld-1)
	case wmMouseMove:
		w.detector.SetLoops(math.Float64frombits(w.loops.Load()))
		w.detector.SetGap(math.Float64frombits(w.gap.Load()))
		w.detector.Hold(math.Float64frombits(w.holdUntil.Load()))
		scale := monitorScale(info.Pt)
		now := float64(time.Since(w.started).Milliseconds())
		if found, _, _ := w.detector.Feed(float64(info.Pt.X)/scale, float64(info.Pt.Y)/scale, now, w.buttonsHeld > 0); found {
			procPostMessage.Call(w.window, messageToggle, toggleFromGesture, 0)
		}
	case wmMouseWheel:
		if w.lensOpen.Load() && keyHeld(vkControl) {
			step := 1
			if int16(info.MouseData>>16) < 0 {
				step = -1
			}
			procPostMessage.Call(w.window, messageZoom, uintptr(step), 0)
			return true
		}
		if w.lensOpen.Load() {
			// How far it scrolled (120 a notch; finer for smooth wheels), and let the scroll through.
			procPostMessage.Call(w.window, messageScrolled, uintptr(abs16(info.MouseData>>16)), 0)
		}
	case wmMouseHWheel:
		if w.lensOpen.Load() {
			procPostMessage.Call(w.window, messageScrolled, uintptr(abs16(info.MouseData>>16)), 0)
		}
	}
	return false
}

func keyHeld(key int) bool {
	state, _, _ := procGetAsyncKeyState.Call(uintptr(key))
	return state&0x8000 != 0
}

// holdForGap starts the gap now: no circle counts until it has passed. Called from the UI thread.
func (w *mouseWatcher) holdForGap() {
	now := float64(time.Since(w.started).Milliseconds())
	w.holdUntil.Store(math.Float64bits(now + math.Float64frombits(w.gap.Load())*1000))
}

// abs16 is the size of a signed 16-bit wheel delta.
func abs16(value uint32) int {
	delta := int(int16(value))
	if delta < 0 {
		return -delta
	}
	return delta
}
