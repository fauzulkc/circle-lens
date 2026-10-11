//go:build windows

package main

// The keys that work while the lens is open: zoom in, zoom out, and (if one is set) close. They are
// claimed only while it is open, so they work over every app but are handed back the moment it closes.
// Each can be recorded from the flyout or the menu, like the open/close shortcut.

import (
	"fmt"
	"strings"

	"circlelens/internal/shortcut"
)

// keyWhileOpen is one of them: what it is called, its setting, and its hotkey.
type keyWhileOpen struct {
	target, name string
	setting      *string
	hotkey       uintptr
	fallback     string
}

func (a *application) keysWhileOpen() []keyWhileOpen {
	return []keyWhileOpen{
		{"zoomIn", "zoom in", &a.settings.ZoomInKeys, hotkeyIn, shortcut.DefaultZoomIn},
		{"zoomOut", "zoom out", &a.settings.ZoomOutKeys, hotkeyOut, shortcut.DefaultZoomOut},
		{"close", "close the lens", &a.settings.CloseKey, hotkeyClose, ""},
	}
}

// hotkeyModifiers is RegisterHotKey's modifiers for a combination.
func hotkeyModifiers(s shortcut.Shortcut) uintptr {
	modifiers := uintptr(modNoRepeat)
	if s.Ctrl {
		modifiers |= modControl
	}
	if s.Alt {
		modifiers |= modAlt
	}
	if s.Shift {
		modifiers |= modShift
	}
	if s.Win {
		modifiers |= modWin
	}
	return modifiers
}

// registerKeys claims the keys while the lens is open (on) or hands them back. With the default zoom
// keys, the keypad's + and − work too.
func (a *application) registerKeys(on bool) {
	if on == a.keysRegistered {
		return
	}
	a.keysRegistered = on
	for _, id := range []uintptr{hotkeyIn, hotkeyOut, hotkeyPadIn, hotkeyPadOut, hotkeyClose} {
		procUnregisterHotKey.Call(a.window, id)
	}
	if !on {
		return
	}
	register := func(id uintptr, text string) {
		if text == "" {
			return
		}
		s, err := shortcut.ParseWhileOpen(text)
		if err != nil {
			return
		}
		code, _ := shortcut.KeyCode(s.Key)
		// Zoom keys repeat while held; closing doesn't need to.
		modifiers := hotkeyModifiers(s)
		if id != hotkeyClose {
			modifiers &^= modNoRepeat
		}
		procRegisterHotKey.Call(a.window, id, modifiers, uintptr(code))
	}
	register(hotkeyIn, a.settings.ZoomInKeys)
	register(hotkeyOut, a.settings.ZoomOutKeys)
	if a.settings.ZoomInKeys == shortcut.DefaultZoomIn {
		register(hotkeyPadIn, "Ctrl+Num+")
	}
	if a.settings.ZoomOutKeys == shortcut.DefaultZoomOut {
		register(hotkeyPadOut, "Ctrl+Num-")
	}
	register(hotkeyClose, a.settings.CloseKey)
}

// reregisterKeys puts changed keys in place now if the lens is open.
func (a *application) reregisterKeys() {
	if a.keysRegistered {
		a.registerKeys(false)
		a.registerKeys(true)
	}
}

// setKeyWhileOpen checks a recorded or chosen key for `target` and saves it, or says why not.
func (a *application) setKeyWhileOpen(target, text string) {
	var key keyWhileOpen
	for _, k := range a.keysWhileOpen() {
		if k.target == target {
			key = k
		}
	}
	if key.setting == nil {
		return
	}
	if text == "" { // no close key
		*key.setting = ""
		a.save()
		a.reregisterKeys()
		a.noteShortcut("No close key: " + a.shortcut.String() + " still closes the lens")
		return
	}
	chosen, err := shortcut.ParseWhileOpen(text)
	if err != nil {
		a.noteShortcut(fmt.Sprintf("%s can't %s: %s", text, key.name, plainShortcutProblem(err)))
		return
	}
	// One combination, one job.
	if chosen.String() == a.shortcut.String() {
		a.noteShortcut(chosen.String() + " already opens and closes the lens")
		return
	}
	for _, other := range a.keysWhileOpen() {
		if other.target != target && *other.setting == chosen.String() {
			a.noteShortcut(fmt.Sprintf("%s already does %s", chosen.String(), other.name))
			return
		}
	}
	// Another app may hold it: try it (the lens's own keys are let go first, if it is open).
	wasOpen := a.keysRegistered
	a.registerKeys(false)
	code, _ := shortcut.KeyCode(chosen.Key)
	ok, _, _ := procRegisterHotKey.Call(a.window, hotkeyProbe, hotkeyModifiers(chosen), uintptr(code))
	procUnregisterHotKey.Call(a.window, hotkeyProbe)
	if wasOpen {
		a.registerKeys(true)
	}
	if ok == 0 {
		a.noteShortcut(chosen.String() + " is taken by another app: try another")
		return
	}
	*key.setting = chosen.String()
	a.save()
	a.reregisterKeys()
	a.noteShortcut(fmt.Sprintf("Set: %s now %s", chosen.String(), map[string]string{"zoomIn": "zooms in", "zoomOut": "zooms out", "close": "closes the lens"}[target]))
}

// resetZoomKeys goes back to Ctrl + = and Ctrl + −.
func (a *application) resetZoomKeys() {
	a.settings.ZoomInKeys, a.settings.ZoomOutKeys = shortcut.DefaultZoomIn, shortcut.DefaultZoomOut
	a.save()
	a.reregisterKeys()
	a.noteShortcut("Zoom keys: Ctrl + = and Ctrl + −")
}

// plainShortcutProblem is the reason in a refusal, without the shortcut's own name in front.
func plainShortcutProblem(err error) string {
	switch text := err.Error(); {
	case strings.Contains(text, "double tap"):
		return "these keys need a key, not a double tap"
	case strings.Contains(text, "typing"):
		return "it would get in the way of typing; add Ctrl, Alt or Win"
	default:
		return text
	}
}
