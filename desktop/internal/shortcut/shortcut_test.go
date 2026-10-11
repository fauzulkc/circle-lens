package shortcut

import "testing"

func TestParseAndName(t *testing.T) {
	for _, text := range []string{"Shift twice", "Ctrl twice", "Ctrl+Alt+Z", "Ctrl+Shift+F5", "Win+Alt+Space", "F13", "Alt+`"} {
		s, err := Parse(text)
		if err != nil {
			t.Fatalf("%q: %v", text, err)
		}
		if s.String() != text {
			t.Errorf("%q came back as %q", text, s.String())
		}
	}
	if s, err := Parse(" ctrl + alt + z "); err != nil || s.String() != "Ctrl+Alt+Z" {
		t.Errorf("loose spelling: %v %v", s, err)
	}
}

func TestRejectsShortcutsThatGetInTheWay(t *testing.T) {
	for _, text := range []string{"Z", "Shift+Z", "Ctrl+Alt", "Ctrl+Nope", "Win twice", "Ctrl+Z+X"} {
		if _, err := Parse(text); err == nil {
			t.Errorf("%q should be refused", text)
		}
	}
	if s := ParseOrDefault("nonsense"); s.String() != Default {
		t.Errorf("fallback is %q", s)
	}
}

func TestKeyCodes(t *testing.T) {
	if code, _ := KeyCode("Z"); code != 'Z' {
		t.Fatalf("Z is %x", code)
	}
	if name, _ := KeyName(0x74); name != "F5" {
		t.Fatalf("0x74 is %q", name)
	}
}

func TestDoubleTap(t *testing.T) {
	type key struct {
		modifier string
		down     bool
		at       float64
	}
	run := func(only string, keys []key) string {
		d := DoubleTap{Only: only}
		found := ""
		for _, k := range keys {
			if k.down {
				d.Down(k.modifier, k.at)
			} else if got := d.Up(k.modifier, k.at); got != "" {
				found = got
			}
		}
		return found
	}
	tap := func(modifier string, at float64) []key {
		return []key{{modifier, true, at}, {modifier, false, at + 80}}
	}
	twice := append(tap("Shift", 0), tap("Shift", 250)...)
	if got := run("Shift", twice); got != "Shift" {
		t.Fatalf("two quick taps gave %q", got)
	}
	if got := run("Ctrl", twice); got != "" {
		t.Fatalf("watching Ctrl, Shift taps gave %q", got)
	}
	if got := run("", twice); got != "Shift" {
		t.Fatalf("recording, two Shift taps gave %q", got)
	}
	slow := append(tap("Shift", 0), tap("Shift", 1200)...)
	if got := run("Shift", slow); got != "" {
		t.Fatal("taps a second apart counted")
	}
	// Typing a capital, then another: Shift is held with a letter each time.
	capitals := []key{{"Shift", true, 0}, {"", true, 40}, {"Shift", false, 90}, {"Shift", true, 200}, {"", true, 240}, {"Shift", false, 290}}
	if got := run("Shift", capitals); got != "" {
		t.Fatal("typing capitals counted")
	}
	held := []key{{"Shift", true, 0}, {"Shift", false, 600}, {"Shift", true, 700}, {"Shift", false, 780}}
	if got := run("Shift", held); got != "" {
		t.Fatal("a long press counted as a tap")
	}
	// The keyboard repeats a held key as more downs: still one press.
	repeated := []key{{"Shift", true, 0}, {"Shift", true, 30}, {"Shift", false, 100}, {"Shift", true, 250}, {"Shift", false, 320}}
	if got := run("Shift", repeated); got != "Shift" {
		t.Fatal("key repeat broke the tap")
	}
}

func TestKeysWhileOpen(t *testing.T) {
	allowed := map[string]string{
		"Ctrl+=": "Ctrl+=", "ctrl+-": "Ctrl+-", "Esc": "Esc", "F9": "F9", "Up": "Up", "Alt+Z": "Alt+Z", "Ctrl+Num+": "Ctrl+Num+", "Win+Esc": "Win+Esc",
	}
	for text, want := range allowed {
		got, err := ParseWhileOpen(text)
		if err != nil || got.String() != want {
			t.Errorf("ParseWhileOpen(%q) = %v, %v; want %s", text, got, err, want)
		}
	}
	for _, text := range []string{"Shift twice", "Ctrl twice", "=", "A", "Shift+A", "Space", "Num5", "Ctrl", "", "Ctrl+Nope"} {
		if got, err := ParseWhileOpen(text); err == nil {
			t.Errorf("ParseWhileOpen(%q) = %v; want a refusal", text, got)
		}
	}
	for _, text := range []string{DefaultZoomIn, DefaultZoomOut, Esc} {
		if _, err := ParseWhileOpen(text); err != nil {
			t.Errorf("the default %q is refused: %v", text, err)
		}
	}
}
