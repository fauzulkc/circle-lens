package shortcut

import "testing"

func TestConflictsAreWrittenAsShortcutsAreNamed(t *testing.T) {
	for text := range usual {
		s, err := Parse(text)
		if err != nil {
			t.Errorf("%q: %v", text, err)
			continue
		}
		if s.String() != text {
			t.Errorf("%q is named %q, so it would never match", text, s.String())
		}
	}
}

func TestConflict(t *testing.T) {
	for text, conflicts := range map[string]bool{
		"Ctrl+C": true, "Alt+F4": true, "Win+L": true, "Win+3": true, "Ctrl twice": true, "F5": true,
		"Shift twice": false, "Ctrl+Alt+Z": false, "Ctrl+Alt+L": false, "Win+Alt+3": false, "F13": false,
	} {
		s, err := Parse(text)
		if err != nil {
			t.Fatalf("%q: %v", text, err)
		}
		if got := Conflict(s) != ""; got != conflicts {
			t.Errorf("%q: conflict %v, want %v", text, got, conflicts)
		}
	}
}
