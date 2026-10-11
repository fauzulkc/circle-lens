// Package settings reads and writes the desktop app's settings file. The file is untrusted input: Load
// keeps only known keys with values in range (the same ranges as shared/lens-settings.js).
package settings

import (
	"encoding/json"
	"errors"
	"io/fs"
	"math"
	"os"
	"path/filepath"

	"circlelens/internal/shortcut"
)

const (
	ZoomMin   = 1.2
	ZoomMax   = 4
	RadiusMin = 100
	RadiusMax = 300
)

// Settings are what the tray menu changes.
type Settings struct {
	// Enabled watches for the circle gesture at all.
	Enabled bool `json:"enabled"`
	// Loops is the number of circles to draw (1-3, in halves).
	Loops float64 `json:"loops"`
	// Zoom is the zoom the lens opens at.
	Zoom float64 `json:"zoom"`
	// Radius is the glass radius in DIPs.
	Radius float64 `json:"radius"`
	// Gap is how long, in seconds, after the lens opens or closes before a circle counts again.
	Gap float64 `json:"gap"`
	// ZoomOutWhenFast eases the zoom out while the pointer moves fast, and back in as it slows.
	ZoomOutWhenFast bool `json:"zoomOutWhenFast"`
	// ConvexRim eases the zoom off in a thin band at the glass's rim, like a convex lens.
	ConvexRim bool `json:"convexRim"`
	// Shortcut opens and closes the lens: "Shift twice", "Ctrl+Alt+Z"... (see internal/shortcut).
	Shortcut string `json:"shortcut"`
	// ZoomInKeys and ZoomOutKeys step the zoom while the lens is open ("Ctrl+=", "Ctrl+-").
	ZoomInKeys  string `json:"zoomInKeys"`
	ZoomOutKeys string `json:"zoomOutKeys"`
	// CloseKey closes the lens while it is open ("Esc", "Ctrl+W"…), or "" for none.
	CloseKey string `json:"closeKey"`
	// DeclinedInstall is set when someone chose to just run it rather than install it.
	DeclinedInstall bool `json:"declinedInstall"`
	// Welcomed is set once the first-run notification has been shown.
	Welcomed bool `json:"welcomed"`
	// AutoUpdate lets the installed app check GitHub for a new release once a day and install it.
	// Off, it never checks on its own; "Check for updates" still works.
	AutoUpdate bool `json:"autoUpdate"`
	// LastUpdateCheck is when it last checked for an update (Unix seconds), so restarts don't check again.
	LastUpdateCheck int64 `json:"lastUpdateCheck"`
}

// Defaults match the browser extension and BoundStretch's reading lens.
var Defaults = Settings{Enabled: true, Loops: 2, Zoom: 1.8, Radius: 150, Gap: 1.5, ZoomOutWhenFast: true, ConvexRim: true, Shortcut: shortcut.Default, AutoUpdate: true,
	ZoomInKeys: shortcut.DefaultZoomIn, ZoomOutKeys: shortcut.DefaultZoomOut}

const (
	GapMin = 0.6
	GapMax = 5
)

// ClampZoom rounds to a tenth and keeps the zoom between ZoomMin and ZoomMax.
func ClampZoom(zoom float64) float64 {
	return math.Min(ZoomMax, math.Max(ZoomMin, math.Round(zoom*10)/10))
}

type raw struct {
	Enabled  *bool    `json:"enabled"`
	Loops    *float64 `json:"loops"`
	Zoom     *float64 `json:"zoom"`
	Radius   *float64 `json:"radius"`
	Gap      *float64 `json:"gap"`
	Fast     *bool    `json:"zoomOutWhenFast"`
	Shortcut *string  `json:"shortcut"`
	Rim      *bool    `json:"convexRim"`
	Welcomed *bool    `json:"welcomed"`
	Declined *bool    `json:"declinedInstall"`
	Update   *bool    `json:"autoUpdate"`
	ZoomIn   *string  `json:"zoomInKeys"`
	ZoomOut  *string  `json:"zoomOutKeys"`
	Close    *string  `json:"closeKey"`
	Checked  *int64   `json:"lastUpdateCheck"`
}

func within(value *float64, low, high float64) (float64, bool) {
	if value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) {
		return 0, false
	}
	return math.Min(high, math.Max(low, *value)), true
}

// Clean turns decoded JSON into settings: anything missing or invalid falls back to the default.
func (r raw) clean() Settings {
	out := Defaults
	if r.Enabled != nil {
		out.Enabled = *r.Enabled
	}
	if loops, ok := within(r.Loops, 1, 3); ok {
		out.Loops = math.Round(loops*2) / 2
	}
	if zoom, ok := within(r.Zoom, ZoomMin, ZoomMax); ok {
		out.Zoom = ClampZoom(zoom)
	}
	if radius, ok := within(r.Radius, RadiusMin, RadiusMax); ok {
		out.Radius = math.Round(radius)
	}
	if gap, ok := within(r.Gap, GapMin, GapMax); ok {
		out.Gap = math.Round(gap*10) / 10
	}
	if r.Fast != nil {
		out.ZoomOutWhenFast = *r.Fast
	}
	if r.Rim != nil {
		out.ConvexRim = *r.Rim
	}
	if r.Shortcut != nil {
		if parsed, err := shortcut.Parse(*r.Shortcut); err == nil {
			out.Shortcut = parsed.String()
		}
	}
	out.Welcomed = r.Welcomed != nil && *r.Welcomed
	out.DeclinedInstall = r.Declined != nil && *r.Declined
	if r.Update != nil {
		out.AutoUpdate = *r.Update
	}
	whileOpen := func(saved *string, fallback string) string {
		if saved != nil {
			if parsed, err := shortcut.ParseWhileOpen(*saved); err == nil {
				return parsed.String()
			}
		}
		return fallback
	}
	out.ZoomInKeys = whileOpen(r.ZoomIn, shortcut.DefaultZoomIn)
	out.ZoomOutKeys = whileOpen(r.ZoomOut, shortcut.DefaultZoomOut)
	out.CloseKey = whileOpen(r.Close, "")
	if r.Checked != nil && *r.Checked > 0 {
		out.LastUpdateCheck = *r.Checked
	}
	return out
}

// Parse cleans settings from JSON bytes; bad JSON gives the defaults.
func Parse(data []byte) Settings {
	var decoded raw
	if json.Unmarshal(data, &decoded) != nil {
		return Defaults
	}
	return decoded.clean()
}

// Path is where the settings live: <user config>/<app>/settings.json, app being "CircleLens" (a test
// build passes its own name, so it never touches the settings in daily use).
func Path(app string) (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, app, "settings.json"), nil
}

// Load reads the settings file, or the defaults if there is none.
func Load(path string) Settings {
	data, err := os.ReadFile(path)
	if err != nil {
		return Defaults
	}
	return Parse(data)
}

// Save writes the settings (cleaned) through a temporary file, so a crash never leaves half a file.
func Save(path string, value Settings) error {
	enabled, welcomed, fast, rim, keys, declined := value.Enabled, value.Welcomed, value.ZoomOutWhenFast, value.ConvexRim, value.Shortcut, value.DeclinedInstall
	update, checked := value.AutoUpdate, value.LastUpdateCheck
	zoomIn, zoomOut, closeKey := value.ZoomInKeys, value.ZoomOutKeys, value.CloseKey
	cleaned := raw{Enabled: &enabled, Loops: &value.Loops, Zoom: &value.Zoom, Radius: &value.Radius, Gap: &value.Gap, Fast: &fast, Rim: &rim, Shortcut: &keys,
		Welcomed: &welcomed, Declined: &declined, Update: &update, Checked: &checked, ZoomIn: &zoomIn, ZoomOut: &zoomOut, Close: &closeKey}.clean()
	data, err := json.MarshalIndent(cleaned, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}
