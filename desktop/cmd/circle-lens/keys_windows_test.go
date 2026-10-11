//go:build windows

package main

import (
	"strings"
	"testing"

	"circlelens/internal/lens"
	"circlelens/internal/settings"
	"circlelens/internal/shortcut"
)

func TestChoosingKeysWhileOpen(t *testing.T) {
	a := &application{settings: settings.Defaults}
	a.settings.Enabled = false // no mouse hook in a test
	a.shortcut = shortcut.ParseOrDefault(shortcut.Default)
	a.state = lens.New(a.settings.Zoom)

	refused := func(target, text, because string) {
		t.Helper()
		before := a.settings
		a.setKeyWhileOpen(target, text)
		if a.settings != before {
			t.Errorf("%s for %s was taken", text, target)
		}
		if !strings.Contains(a.shortcutNote, because) {
			t.Errorf("%s for %s: note %q, want it to say %q", text, target, a.shortcutNote, because)
		}
	}
	refused("zoomIn", "Ctrl+-", "already does zoom out")
	refused("close", "Shift twice", "double tap")
	refused("zoomOut", "A", "typing")

	a.setKeyWhileOpen("close", "Esc")
	if a.settings.CloseKey != "Esc" {
		t.Fatalf("close key %q, want Esc (note %q)", a.settings.CloseKey, a.shortcutNote)
	}
	refused("zoomOut", "Esc", "already does close the lens")

	a.setKeyWhileOpen("zoomIn", "Ctrl+Up")
	if a.settings.ZoomInKeys != "Ctrl+Up" {
		t.Fatalf("zoom in %q, want Ctrl+Up (note %q)", a.settings.ZoomInKeys, a.shortcutNote)
	}
	a.setKeyWhileOpen("close", "")
	a.resetZoomKeys()
	if a.settings.CloseKey != "" || a.settings.ZoomInKeys != shortcut.DefaultZoomIn {
		t.Fatalf("after clearing and resetting: close %q, zoom in %q", a.settings.CloseKey, a.settings.ZoomInKeys)
	}
}
