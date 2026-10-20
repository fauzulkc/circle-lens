//go:build windows

package main

// The shortcut that opens and closes the lens, the key that turns Circle Lens on and off, and recording
// a new one (or one of the keys that work while the lens is open: keys_windows.go). While Circle Lens is
// off, the open/close shortcut is let go; only the on/off key stays.

import (
	"fmt"
	"time"
	"unsafe"

	"circlelens/internal/shortcut"
)

// startShortcut puts the saved shortcut in place, falling back to the default if another app has it.
func (a *application) startShortcut() {
	saved := shortcut.ParseOrDefault(a.settings.Shortcut)
	if err := a.applyShortcut(saved); err != nil {
		a.noteShortcut(err.Error() + "; using " + shortcut.Default + " instead")
		_ = a.applyShortcut(shortcut.ParseOrDefault(shortcut.Default))
	}
	if !a.claimOnOffKey() {
		debugLog("shortcut: the on/off key %s is taken by another app", a.settings.OnOffKey)
	}
}

// registerHotkey claims a key combination as Windows hotkey `id`; false if another app has it.
func (a *application) registerHotkey(id uintptr, s shortcut.Shortcut) bool {
	code, _ := shortcut.KeyCode(s.Key)
	ok, _, _ := procRegisterHotKey.Call(a.window, id, hotkeyModifiers(s), uintptr(code))
	return ok != 0
}

// applyShortcut makes `s` the shortcut: a Windows hotkey for a key combination, the keyboard watcher
// for a double tap. It fails, changing nothing, if another app already has the combination. While
// Circle Lens is off, a combination is only checked, then let go.
func (a *application) applyShortcut(s shortcut.Shortcut) error {
	procUnregisterHotKey.Call(a.window, hotkeyToggle)
	if !s.IsDoubleTap() {
		if !a.registerHotkey(hotkeyToggle, s) {
			return fmt.Errorf("%s is taken by another app", s) // the caller puts the old one back
		}
		if !a.settings.Enabled {
			procUnregisterHotKey.Call(a.window, hotkeyToggle)
		}
	}
	a.shortcut = s
	a.syncKeyboard()
	return nil
}

// syncKeyboard runs the keyboard hook only while something needs it: a double-tap shortcut while
// Circle Lens is on, or a recording.
func (a *application) syncKeyboard() {
	tapping := a.shortcut.IsDoubleTap() && a.settings.Enabled
	needed := tapping || a.recording
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
	if tapping {
		double = a.shortcut.Double
	}
	a.keyboard.doubleOf.Store(double)
	a.keyboard.escapeRecords.Store(a.recording && a.recordingFor == "close")
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
func (a *application) startRecording() { a.startRecordingFor("") }

// startRecordingFor records the open/close shortcut (""), the on/off key ("onOff"), or one of the keys
// while the lens is open ("zoomIn", "zoomOut", "close").
func (a *application) startRecordingFor(target string) {
	if a.recording {
		if a.recordingFor == target {
			return
		}
		a.cancelRecording()
	}
	defer a.flyout.refresh()
	a.recording, a.recordingFor = true, target
	a.shortcutNote = ""
	// So that pressing the current shortcut records it rather than opening the lens.
	procUnregisterHotKey.Call(a.window, hotkeyToggle)
	procUnregisterHotKey.Call(a.window, hotkeyOnOff)
	a.syncKeyboard()
}

// cancelRecording stops recording and keeps the shortcut as it was.
func (a *application) cancelRecording() {
	if !a.recording {
		return
	}
	defer a.flyout.refresh()
	a.recording, a.recordingFor = false, ""
	current := a.shortcut
	a.shortcut = shortcut.Shortcut{}
	_ = a.applyShortcut(current)
	a.claimOnOffKey()
}

// finishRecording takes what the keyboard hook recorded: checks it, puts it in place and saves it, or
// says why not and keeps the old one.
func (a *application) finishRecording() {
	if !a.recording || a.keyboard == nil {
		return
	}
	result, _ := a.keyboard.recorded.Load().(string)
	target := a.recordingFor
	a.recording, a.recordingFor = false, ""
	a.claimOnOffKey()
	if target != "" {
		// The on/off key or one of the keys while the lens is open; the open/close shortcut goes back as it was.
		current := a.shortcut
		a.shortcut = shortcut.Shortcut{}
		_ = a.applyShortcut(current)
		switch {
		case result == "":
		case target == "onOff" && result == "Backspace":
			a.setOnOffKey("") // Backspace: no on/off key
		case target == "onOff":
			a.setOnOffKey(result)
		case target == "close" && result == "Backspace":
			a.setKeyWhileOpen(target, "") // Backspace: no close key
		default:
			a.setKeyWhileOpen(target, result)
		}
		a.flyout.refresh()
		return
	}
	previous := a.shortcut
	a.shortcut = shortcut.Shortcut{}
	switch recorded, err := shortcut.Parse(result); {
	case result == "":
		_ = a.applyShortcut(previous) // cancelled
	case err != nil:
		_ = a.applyShortcut(previous)
		a.noteShortcut("Add Ctrl, Alt or Win to " + result + ": it would get in the way of typing")
	case recorded.String() == a.settings.OnOffKey:
		_ = a.applyShortcut(previous)
		a.noteShortcut(recorded.String() + " already turns Circle Lens on and off")
	case !confirmShortcut(recorded):
		_ = a.applyShortcut(previous)
		a.noteShortcut("Kept " + previous.String())
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
	if chosen.String() == a.settings.OnOffKey {
		a.noteShortcut(chosen.String() + " already turns Circle Lens on and off")
		return
	}
	if !confirmShortcut(chosen) {
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

// setEnabled turns Circle Lens on or off. Off, nothing opens the lens: not a circle, the shortcut, the
// menu or -toggle. Only the on/off key stays, to turn it back on. From that key, a notification says
// what happened, since nothing else on screen would.
func (a *application) setEnabled(on, fromKey bool) {
	a.cancelRecording()
	a.settings.Enabled = on
	if !on {
		a.close()
	}
	a.save()
	if err := a.applyShortcut(a.shortcut); err != nil {
		a.noteShortcut(err.Error() + ": try another")
	}
	a.flyout.refresh()
	switch {
	case !fromKey:
	case on:
		a.notifyUnlessShown("Circle Lens is on", fmt.Sprintf("Draw %s or press %s to magnify.", loopsLabel(a.settings.Loops), a.shortcut), "")
	default:
		a.notifyUnlessShown("Circle Lens is off", "Nothing opens the lens until you press "+a.settings.OnOffKey+" again.", "")
	}
}

// claimOnOffKey holds the on/off key, if there is one, whether Circle Lens is on or off (but not while
// recording, so pressing it records it). False if another app has it.
func (a *application) claimOnOffKey() bool {
	procUnregisterHotKey.Call(a.window, hotkeyOnOff)
	if a.settings.OnOffKey == "" || a.recording {
		return true
	}
	s, err := shortcut.Parse(a.settings.OnOffKey)
	return err == nil && a.registerHotkey(hotkeyOnOff, s)
}

// setOnOffKey checks a recorded key combination for turning Circle Lens on and off and saves it, or says
// why not. "" is none.
func (a *application) setOnOffKey(text string) {
	if text == "" {
		a.settings.OnOffKey = ""
		a.save()
		a.claimOnOffKey()
		a.noteShortcut("No on/off key: use the switch at the top")
		return
	}
	chosen, err := shortcut.Parse(text)
	switch {
	case err != nil:
		a.noteShortcut(text + " can't turn it on and off: " + plainShortcutProblem(err))
		return
	case chosen.IsDoubleTap():
		a.noteShortcut("Use a key combination to turn it on and off, not a double tap")
		return
	case chosen.String() == a.shortcut.String():
		a.noteShortcut(chosen.String() + " already opens and closes the lens")
		return
	}
	for _, other := range a.keysWhileOpen() {
		if *other.setting == chosen.String() {
			a.noteShortcut(fmt.Sprintf("%s already does %s", chosen, other.name))
			return
		}
	}
	if !confirmShortcut(chosen) {
		return
	}
	previous := a.settings.OnOffKey
	a.settings.OnOffKey = chosen.String()
	if !a.claimOnOffKey() {
		a.settings.OnOffKey = previous
		a.claimOnOffKey()
		a.noteShortcut(chosen.String() + " is taken by another app: try another")
		return
	}
	a.save()
	a.noteShortcut("Set: " + chosen.String() + " turns Circle Lens on and off")
}

// confirmShortcut asks before taking a shortcut that already does something common (shortcut.Conflict);
// true to go ahead.
func confirmShortcut(s shortcut.Shortcut) bool {
	usual := shortcut.Conflict(s)
	if usual == "" {
		return true
	}
	effect := "While Circle Lens runs, it takes " + s.String() + " from every app."
	if s.IsDoubleTap() {
		effect = "Both would respond each time you do it."
	}
	question := fmt.Sprintf("%s %s.\n\n%s\n\nUse %s anyway?", s, usual, effect, s)
	answer, _, _ := procMessageBox.Call(0, uintptr(unsafe.Pointer(wide(question))), uintptr(unsafe.Pointer(wide(displayName()))),
		mbYesNo|mbIconWarning|mbDefButton2|mbSetForeground|mbTopmost)
	return answer == idYes
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
