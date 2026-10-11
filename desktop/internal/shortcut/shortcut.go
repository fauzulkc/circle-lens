// Package shortcut is the keyboard shortcut that opens and closes the lens: either a key combination
// ("Ctrl+Alt+Z") or a double tap of a modifier ("Shift twice"). It parses and names shortcuts as they
// are saved in settings and shown in menus, and spots double taps. Key codes are Windows virtual-key
// codes, the one table a backend maps its own keys onto.
package shortcut

import (
	"errors"
	"fmt"
	"strings"
)

// Default is the shortcut until someone picks another: one finger, and unlike a double Ctrl it does not
// clash with PowerToys' Find My Mouse.
const Default = "Shift twice"

// Presets are offered in the menus; anything else can be recorded.
var Presets = []string{"Shift twice", "Ctrl twice", "Ctrl+Alt+Z", "Ctrl+Alt+L"}

// Shortcut is a key with modifiers, or a double tap of one modifier.
type Shortcut struct {
	Ctrl, Alt, Shift, Win bool
	Key                   string // a key name from the table below; empty for a double tap
	Double                string // "Ctrl", "Shift" or "Alt" for a double tap
}

// modifierNames is the order modifiers are written in, as Windows writes them (Win+Ctrl+Alt+Shift).
var modifierNames = []string{"Win", "Ctrl", "Alt", "Shift"}

// IsDoubleTap tells the two kinds apart.
func (s Shortcut) IsDoubleTap() bool { return s.Double != "" }

func (s Shortcut) String() string {
	if s.IsDoubleTap() {
		return s.Double + " twice"
	}
	var parts []string
	for i, on := range []bool{s.Win, s.Ctrl, s.Alt, s.Shift} {
		if on {
			parts = append(parts, modifierNames[i])
		}
	}
	return strings.Join(append(parts, s.Key), "+")
}

// Parse reads "Ctrl+Alt+Z" or "Shift twice" (case does not matter).
func Parse(text string) (Shortcut, error) {
	text = strings.TrimSpace(text)
	if name, ok := strings.CutSuffix(strings.ToLower(text), " twice"); ok {
		for _, modifier := range []string{"Ctrl", "Shift", "Alt"} {
			if strings.EqualFold(name, modifier) {
				return Shortcut{Double: modifier}, nil
			}
		}
		return Shortcut{}, fmt.Errorf("%q: only Ctrl, Shift or Alt can be tapped twice", text)
	}
	var s Shortcut
	parts := strings.Split(text, "+")
	for i, part := range parts {
		part = strings.TrimSpace(part)
		last := i == len(parts)-1
		switch strings.ToLower(part) {
		case "ctrl", "control":
			s.Ctrl = true
		case "alt":
			s.Alt = true
		case "shift":
			s.Shift = true
		case "win", "windows":
			s.Win = true
		default:
			if !last {
				return Shortcut{}, fmt.Errorf("%q: %q is not a modifier", text, part)
			}
			name, ok := canonicalName(part)
			if !ok {
				return Shortcut{}, fmt.Errorf("%q: unknown key %q", text, part)
			}
			s.Key = name
		}
	}
	return s, s.Check()
}

// Check says whether a shortcut is usable: a key, with Ctrl, Alt or Win (Shift alone would take a
// capital letter from typing), unless it is a function key, which nothing types.
func (s Shortcut) Check() error {
	if s.IsDoubleTap() {
		return nil
	}
	if s.Key == "" {
		return errors.New("a shortcut needs a key as well as modifiers")
	}
	if !s.Ctrl && !s.Alt && !s.Win && !strings.HasPrefix(s.Key, "F") {
		return fmt.Errorf("%s would get in the way of typing: add Ctrl, Alt or Win", s)
	}
	return nil
}

// ParseOrDefault reads a saved shortcut, falling back to Default.
func ParseOrDefault(text string) Shortcut {
	if s, err := Parse(text); err == nil {
		return s
	}
	s, _ := Parse(Default)
	return s
}

/* ---------- key names ---------- */

// keys maps names to Windows virtual-key codes. Letters and digits are added in init.
var keys = map[string]uint32{
	"Space": 0x20, "PageUp": 0x21, "PageDown": 0x22, "End": 0x23, "Home": 0x24,
	"Left": 0x25, "Up": 0x26, "Right": 0x27, "Down": 0x28, "Insert": 0x2D, "Delete": 0x2E,
	"Pause": 0x13, "ScrollLock": 0x91, "Backspace": 0x08, "Enter": 0x0D, "Tab": 0x09,
	";": 0xBA, "=": 0xBB, ",": 0xBC, "-": 0xBD, ".": 0xBE, "/": 0xBF, "`": 0xC0,
	"[": 0xDB, "\\": 0xDC, "]": 0xDD, "'": 0xDE,
	"Num0": 0x60, "Num1": 0x61, "Num2": 0x62, "Num3": 0x63, "Num4": 0x64,
	"Num5": 0x65, "Num6": 0x66, "Num7": 0x67, "Num8": 0x68, "Num9": 0x69,
	"Num*": 0x6A, "Num+": 0x6B, "Num-": 0x6D, "Num.": 0x6E, "Num/": 0x6F,
}

var names = map[uint32]string{}

func init() {
	for c := 'A'; c <= 'Z'; c++ {
		keys[string(c)] = uint32(c)
	}
	for c := '0'; c <= '9'; c++ {
		keys[string(c)] = uint32(c)
	}
	for n := 1; n <= 24; n++ {
		keys[fmt.Sprintf("F%d", n)] = uint32(0x70 + n - 1)
	}
	for name, code := range keys {
		names[code] = name
	}
}

func canonicalName(name string) (string, bool) {
	for known := range keys {
		if strings.EqualFold(known, name) {
			return known, true
		}
	}
	return "", false
}

// KeyCode is the virtual-key code for a key name.
func KeyCode(name string) (uint32, bool) {
	code, ok := keys[name]
	return code, ok
}

// KeyName is the name for a virtual-key code, if it is one a shortcut can use.
func KeyName(code uint32) (string, bool) {
	name, ok := names[code]
	return name, ok
}
