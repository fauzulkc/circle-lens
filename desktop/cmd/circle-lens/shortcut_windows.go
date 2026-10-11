//go:build windows

package main

// The shortcut that opens and closes the lens, and recording a new one.

import (
	"fmt"
	"time"

	"circlelens/internal/shortcut"
)

// startShortcut puts the saved shortcut in place, falling back to the default if another app has it.
func (a *application) startShortcut() {
	saved := shortcut.ParseOrDefault(a.settings.Shortcut)
	if err := a.applyShortcut(saved); err != nil {
		a.noteShortcut(err.Error() + "; using " + shortcut.Default + " instead")
		_ = a.applyShortcut(shortcut.ParseOrDefault(shortcut.Default))
	}
}

// applyShortcut makes `s` the shortcut: a Windows hotkey for a key combination, the keyboard watcher
// for a double tap. It fails, changing nothing, if another app already has the combination.
func (a *application) applyShortcut(s shortcut.Shortcut) error {
	procUnregisterHotKey.Call(a.window, hotkeyToggle)
	if !s.IsDoubleTap() {
		code, _ := shortcut.KeyCode(s.Key)
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
		if ok, _, _ := procRegisterHotKey.Call(a.window, hotkeyToggle, modifiers, uintptr(code)); ok == 0 {
			return fmt.Errorf("%s is taken by another app", s) // the caller puts the old one back
		}
	}
	a.shortcut = s
	a.syncKeyboard()
	return nil
}

// syncKeyboard runs the keyboard hook only while something needs it: a double-tap shortcut, or a
// recording.
func (a *application) syncKeyboard() {
	needed := a.shortcut.IsDoubleTap() || a.recording
	if needed && a.keyboard == nil {
		a.keyboard = startKeyWatcher(a.window, a.instance)
	}
	if a.keyboard == nil {
		return
	}
	if !needed {
		a.stopKeyboard()
		return
	}
	double := ""
	if a.shortcut.IsDoubleTap() {
		double = a.shortcut.Double
	}
	a.keyboard.doubleOf.Store(double)
	a.keyboard.recording.Store(a.recording)
}

func (a *application) stopKeyboard() {
	if a.keyboard != nil {
		a.keyboard.stop()
		a.keyboard = nil
		keyboard = nil
	}
}

// startRecording takes the next key combination or double tap as the new shortcut.
func (a *application) startRecording() {
	if a.recording {
		return
	}
	defer a.flyout.refresh()
	a.recording = true
	a.shortcutNote = ""
	// So that pressing the current shortcut records it rather than opening the lens.
	procUnregisterHotKey.Call(a.window, hotkeyToggle)
	a.syncKeyboard()
}

// cancelRecording stops recording and keeps the shortcut as it was.
func (a *application) cancelRecording() {
	if !a.recording {
		return
	}
	defer a.flyout.refresh()
	a.recording = false
	current := a.shortcut
	a.shortcut = shortcut.Shortcut{}
	_ = a.applyShortcut(current)
}

// finishRecording takes what the keyboard hook recorded: checks it, puts it in place and saves it, or
// says why not and keeps the old one.
func (a *application) finishRecording() {
	if !a.recording || a.keyboard == nil {
		return
	}
	result, _ := a.keyboard.recorded.Load().(string)
	a.recording = false
	previous := a.shortcut
	a.shortcut = shortcut.Shortcut{}
	switch recorded, err := shortcut.Parse(result); {
	case result == "":
		_ = a.applyShortcut(previous) // cancelled
	case err != nil:
		_ = a.applyShortcut(previous)
		a.noteShortcut("Add Ctrl, Alt or Win to " + result + ": it would get in the way of typing")
	default:
		if err := a.applyShortcut(recorded); err != nil {
			_ = a.applyShortcut(previous)
			a.noteShortcut(recorded.String() + " is taken by another app: try another")
			break
		}
		a.settings.Shortcut = recorded.String()
		a.save()
		a.noteShortcut("Set: " + recorded.String() + " now opens and closes the lens")
	}
	a.flyout.refresh()
}

// chooseShortcut puts a preset in place from the menu.
func (a *application) chooseShortcut(text string) {
	chosen, err := shortcut.Parse(text)
	if err != nil {
		return
	}
	previous := a.shortcut
	if err := a.applyShortcut(chosen); err != nil {
		_ = a.applyShortcut(previous)
		a.noteShortcut(err.Error())
		return
	}
	a.settings.Shortcut = chosen.String()
	a.save()
}

// noteShortcut shows a short message about the shortcut in the flyout for a few seconds.
func (a *application) noteShortcut(note string) {
	a.shortcutNote, a.shortcutNoteUntil = note, time.Now().Add(4*time.Second)
	debugLog("shortcut: %s", note)
	if a.flyout != nil {
		a.flyout.showNoteTimer()
	}
}

// currentShortcutNote is the message to show, while it lasts.
func (a *application) currentShortcutNote() string {
	if time.Now().After(a.shortcutNoteUntil) {
		return ""
	}
	return a.shortcutNote
}
