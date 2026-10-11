package settings

import (
	"path/filepath"
	"testing"
)

func TestParseKeepsKnownKeysInRange(t *testing.T) {
	got := Parse([]byte(`{"enabled": false, "loops": 2.3, "zoom": 9, "radius": 12, "shortcut": "Shift+Z", "extra": "ignored"}`))
	want := Settings{Enabled: false, Loops: 2.5, Zoom: ZoomMax, Radius: RadiusMin, Gap: 1.5, ZoomOutWhenFast: true, ConvexRim: true, Shortcut: "Shift twice", AutoUpdate: true}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestParseFallsBackToDefaults(t *testing.T) {
	for _, input := range []string{``, `not json`, `[]`, `{"loops": "two", "zoom": null}`} {
		if got := Parse([]byte(input)); got != Defaults {
			t.Errorf("%q gave %+v", input, got)
		}
	}
}

func TestSaveThenLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "settings.json")
	want := Settings{Enabled: true, Loops: 1.5, Zoom: 2.5, Radius: 200, Gap: 2, ZoomOutWhenFast: false, ConvexRim: false, Shortcut: "Ctrl+Alt+Z", AutoUpdate: false, LastUpdateCheck: 1760184000}
	if err := Save(path, want); err != nil {
		t.Fatal(err)
	}
	if got := Load(path); got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestUpdatesAreOnUnlessTurnedOff(t *testing.T) {
	if !Parse([]byte(`{"zoom": 2}`)).AutoUpdate {
		t.Error("a settings file from before updates existed should get them")
	}
	if Parse([]byte(`{"autoUpdate": false}`)).AutoUpdate {
		t.Error("turning updates off did not stick")
	}
	if got := Parse([]byte(`{"lastUpdateCheck": -5}`)).LastUpdateCheck; got != 0 {
		t.Errorf("a negative check time was kept: %d", got)
	}
}
