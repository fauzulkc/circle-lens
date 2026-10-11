//go:build windows

package main

// The keyboard hook, on its own thread like the mouse hook, runs only when it is needed:
//
//   - for a double-tap shortcut ("Shift twice"), which Windows hotkeys cannot express;
//   - while a shortcut is being recorded from the flyout or menu: the next key combination or double
//     tap is taken as the new shortcut, and kept from reaching any app (Esc alone cancels).
//
// It looks only at which keys go down and up; nothing typed is kept or logged. A key combination
// shortcut does not use it: that is a plain Windows hotkey (RegisterHotKey).

import (
	"runtime"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"circlelens/internal/shortcut"
)

type keyWatcher struct {
	window   uintptr
	threadID atomic.Uint32
	// Set by the UI thread.
	doubleOf  atomic.Value // string: the modifier whose double tap toggles the lens ("" for none)
	recording atomic.Bool
	// Set by the hook thread when a recording ends: the shortcut, or "" if it was cancelled.
	recorded atomic.Value
	started  time.Time
	// toggled and finished tell the UI thread (the tests swap them for their own).
	toggled  func()
	finished func()

	// Owned by the hook thread.
	tap                   shortcut.DoubleTap
	ctrl, alt, shift, win bool
}

type keyboardHookInfo struct {
	VkCode    uint32
	ScanCode  uint32
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

var (
	keyboard             *keyWatcher
	keyboardHookCallback = syscall.NewCallback(keyboardHook) // made once: Windows callbacks are finite
)

// startKeyWatcher installs the keyboard hook on a new locked thread and returns once it is in place.
func startKeyWatcher(window, module uintptr) *keyWatcher {
	k := newKeyWatcher(window)
	keyboard = k
	ready := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		id, _, _ := procGetCurrentThreadId.Call()
		k.threadID.Store(uint32(id))
		hook, _, _ := procSetWindowsHookEx.Call(whKeyboardLL, keyboardHookCallback, module, 0)
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
	return k
}

func newKeyWatcher(window uintptr) *keyWatcher {
	k := &keyWatcher{window: window, started: time.Now()}
	k.doubleOf.Store("")
	k.recorded.Store("")
	k.toggled = func() { procPostMessage.Call(window, messageToggle, toggleFromShortcut, 0) }
	k.finished = func() { procPostMessage.Call(window, messageRecorded, 0, 0) }
	return k
}

func (k *keyWatcher) stop() { procPostThreadMessage.Call(uintptr(k.threadID.Load()), wmQuit, 0, 0) }

func keyboardHook(code, wParam, lParam uintptr) uintptr {
	// The hook hands a pointer to its KBDLLHOOKSTRUCT in lParam; reading it is the documented contract.
	if int32(code) >= 0 && keyboard != nil && keyboard.onKey(uint32(wParam), (*keyboardHookInfo)(unsafe.Pointer(lParam))) {
		return 1 // swallowed (a key pressed while recording)
	}
	next, _, _ := procCallNextHookEx.Call(0, code, wParam, lParam)
	return next
}

// modifierOf names the modifier a virtual key is ("Ctrl", "Shift", "Alt", "Win"), or "" for others.
func modifierOf(vk uint32) string {
	switch vk {
	case 0x10, 0xA0, 0xA1:
		return "Shift"
	case 0x11, 0xA2, 0xA3:
		return "Ctrl"
	case 0x12, 0xA4, 0xA5:
		return "Alt"
	case 0x5B, 0x5C:
		return "Win"
	}
	return ""
}

// onKey handles one key event; it must return quickly. It returns true to keep the key from apps.
func (k *keyWatcher) onKey(event uint32, info *keyboardHookInfo) bool {
	return k.onKeyAt(event, info, float64(time.Since(k.started).Milliseconds()))
}

// onKeyAt is onKey at a given time (ms), for tests.
func (k *keyWatcher) onKeyAt(event uint32, info *keyboardHookInfo, now float64) bool {
	down := event == wmKeyDown || event == wmSysKeyDown
	up := event == wmKeyUp || event == wmSysKeyUp
	if !down && !up {
		return false
	}
	modifier := modifierOf(info.VkCode)
	switch modifier {
	case "Ctrl":
		k.ctrl = down
	case "Alt":
		k.alt = down
	case "Shift":
		k.shift = down
	case "Win":
		k.win = down
	}
	// Win is never tapped twice (it opens Start); for the tap detector it counts as another key.
	tapKey := modifier
	if modifier == "Win" {
		tapKey = ""
	}

	recording := k.recording.Load()
	only := ""
	if !recording {
		only = k.doubleOf.Load().(string)
	}
	if k.tap.Only != only {
		k.tap = shortcut.DoubleTap{Only: only}
	}

	if down {
		k.tap.Down(tapKey, now)
		if !recording {
			return false
		}
		if modifier != "" {
			return true // held while choosing the key; let go, it may be a double tap
		}
		if info.VkCode == vkEscape && !k.ctrl && !k.alt && !k.shift && !k.win {
			k.finishRecording("")
			return true
		}
		if name, ok := shortcut.KeyName(info.VkCode); ok {
			k.finishRecording(shortcut.Shortcut{Ctrl: k.ctrl, Alt: k.alt, Shift: k.shift, Win: k.win, Key: name}.String())
		}
		return true
	}
	// Key ups always go through, so no app is left thinking a key is still held.
	if tapped := k.tap.Up(tapKey, now); tapped != "" {
		if recording {
			k.finishRecording(shortcut.Shortcut{Double: tapped}.String())
		} else {
			k.toggled()
		}
	}
	return false
}

func (k *keyWatcher) finishRecording(result string) {
	k.recording.Store(false)
	k.recorded.Store(result)
	k.finished()
}
