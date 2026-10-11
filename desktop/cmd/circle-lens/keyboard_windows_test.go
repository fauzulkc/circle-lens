//go:build windows

package main

import "testing"

// keyScript feeds key events to a watcher without a hook: (virtual key, down, time in ms).
type keyEvent struct {
	vk   uint32
	down bool
	at   float64
}

func feed(k *keyWatcher, events []keyEvent) {
	for _, e := range events {
		kind := uint32(wmKeyUp)
		if e.down {
			kind = wmKeyDown
		}
		k.onKeyAt(kind, &keyboardHookInfo{VkCode: e.vk}, e.at)
	}
}

func testWatcher() (*keyWatcher, *int, *string) {
	k := newKeyWatcher(0)
	toggles, recorded := 0, "(none)"
	k.toggled = func() { toggles++ }
	k.finished = func() { recorded, _ = k.recorded.Load().(string) }
	return k, &toggles, &recorded
}

func TestShiftTwiceToggles(t *testing.T) {
	k, toggles, _ := testWatcher()
	k.doubleOf.Store("Shift")
	feed(k, []keyEvent{{0xA0, true, 0}, {0xA0, false, 70}, {0xA0, true, 200}, {0xA0, false, 260}})
	if *toggles != 1 {
		t.Fatalf("toggles=%d, want 1", *toggles)
	}
	// Capitals typed quickly: Shift with a letter each time.
	feed(k, []keyEvent{{0xA0, true, 1000}, {'T', true, 1030}, {'T', false, 1060}, {0xA0, false, 1080}, {0xA0, true, 1200}, {'H', true, 1230}, {'H', false, 1260}, {0xA0, false, 1280}})
	if *toggles != 1 {
		t.Fatalf("typing capitals toggled: %d", *toggles)
	}
}

func TestRecordingTakesTheNextShortcut(t *testing.T) {
	k, toggles, recorded := testWatcher()
	k.doubleOf.Store("Shift")
	k.recording.Store(true)
	swallowed := k.onKeyAt(wmKeyDown, &keyboardHookInfo{VkCode: 0xA2}, 0) // Ctrl
	k.onKeyAt(wmSysKeyDown, &keyboardHookInfo{VkCode: 0xA4}, 10)          // Alt
	swallowedKey := k.onKeyAt(wmSysKeyDown, &keyboardHookInfo{VkCode: 'Z'}, 20)
	if *recorded != "Ctrl+Alt+Z" || !swallowed || !swallowedKey || *toggles != 0 {
		t.Fatalf("recorded %q, swallowed %v/%v, toggles %d", *recorded, swallowed, swallowedKey, *toggles)
	}
	// Recording a double tap: Ctrl twice.
	k, _, recorded = testWatcher()
	k.recording.Store(true)
	feed(k, []keyEvent{{0xA2, true, 0}, {0xA2, false, 60}, {0xA2, true, 180}, {0xA2, false, 240}})
	if *recorded != "Ctrl twice" {
		t.Fatalf("recorded %q, want Ctrl twice", *recorded)
	}
	// Esc alone cancels.
	k, _, recorded = testWatcher()
	k.recording.Store(true)
	feed(k, []keyEvent{{vkEscape, true, 0}})
	if *recorded != "" {
		t.Fatalf("Esc recorded %q, want a cancel", *recorded)
	}
}

func TestRecordingTheCloseKey(t *testing.T) {
	// Recording the close key, Esc on its own is the key, not a cancel.
	k, _, recorded := testWatcher()
	k.recording.Store(true)
	k.escapeRecords.Store(true)
	feed(k, []keyEvent{{vkEscape, true, 0}})
	if *recorded != "Esc" {
		t.Fatalf("recorded %q, want Esc", *recorded)
	}
	// Combinations still record; zoom keys, for one.
	k, _, recorded = testWatcher()
	k.recording.Store(true)
	feed(k, []keyEvent{{0xA2, true, 0}, {0x26, true, 20}}) // Ctrl + Up
	if *recorded != "Ctrl+Up" {
		t.Fatalf("recorded %q, want Ctrl+Up", *recorded)
	}
}
