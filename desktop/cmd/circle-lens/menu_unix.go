//go:build linux || darwin

package main

import (
	"fmt"

	"circlelens/internal/settings"
	"circlelens/internal/shortcut"
)

// websiteAddress is the project's page.
const websiteAddress = "https://fauzul.com/circle-lens"

// rebuildMenu gives the system the tray menu as it stands: the same choices as the Windows menu
// (tray_windows.go), less the ones a preview doesn't have yet (recording a shortcut, updates).
func (a *application) rebuildMenu() {
	open := a.state.IsOpen()
	openLabel := "Open the lens"
	if open {
		openLabel = "Close the lens"
	}
	status := fmt.Sprintf("Draw %s anywhere to open it", loopsLabel(a.settings.Loops))
	if !a.settings.Enabled {
		status = "Not watching for circles"
	}
	var loops, zooms, gaps, sizes, shortcuts []menuItem
	for _, choice := range loopChoices {
		loops = append(loops, a.choice(choice.label, map[float64]string{1: "quickest, may open by accident", 2: "default", 3: "fewest accidents"}[choice.loops],
			choice.loops == a.settings.Loops, func(s *settings.Settings) { s.Loops = choice.loops }))
	}
	for _, zoom := range zoomChoices {
		zooms = append(zooms, a.choice(fmt.Sprintf("%.1f×", zoom), map[bool]string{true: "default"}[zoom == 1.8],
			zoom == a.settings.Zoom, func(s *settings.Settings) { s.Zoom = zoom; a.state.SetZoom(a.now(), zoom) }))
	}
	for _, gap := range gapChoices {
		gaps = append(gaps, a.choice(trimZoom(gap)+" s", map[bool]string{true: "default"}[gap == 1.5],
			gap == a.settings.Gap, func(s *settings.Settings) { s.Gap = gap }))
	}
	for _, choice := range sizeChoices {
		sizes = append(sizes, a.choice(choice.label, fmt.Sprintf("%.0f px", choice.radius*2),
			choice.radius == a.settings.Radius, func(s *settings.Settings) { s.Radius = choice.radius }))
	}
	current := a.shortcut.String()
	listed := false
	for _, preset := range shortcut.Presets {
		shortcuts = append(shortcuts, a.choice(preset, "", preset == current, func(s *settings.Settings) { s.Shortcut = preset }))
		listed = listed || preset == current
	}
	if !listed {
		shortcuts = append(shortcuts, menuItem{label: current, radio: true, checked: true})
	}
	closeKeys := []menuItem{
		a.choice("No close key", "", a.settings.CloseKey == "", func(s *settings.Settings) { s.CloseKey = "" }),
		a.choice("Esc closes it", "", a.settings.CloseKey == shortcut.Esc, func(s *settings.Settings) { s.CloseKey = shortcut.Esc }),
	}
	items := []menuItem{
		{label: openLabel, detail: current, action: a.toggle},
		{label: status, disabled: true},
		{separator: true},
		a.check("Watch for circles", a.settings.Enabled, func(s *settings.Settings) { s.Enabled = !s.Enabled }),
		{label: "Circles to draw", children: loops},
		{label: "Starting zoom", children: zooms},
		{label: "Wait between open and close", children: gaps},
		{label: "Lens size", children: sizes},
		{separator: true},
		{label: "Shortcut", children: shortcuts},
		{label: "Keys while the lens is open", children: append([]menuItem{
			{label: "Zoom in", detail: a.settings.ZoomInKeys, disabled: true},
			{label: "Zoom out", detail: a.settings.ZoomOutKeys, disabled: true},
			{separator: true},
		}, closeKeys...)},
		a.check("Zoom out when moving fast", a.settings.ZoomOutWhenFast, func(s *settings.Settings) { s.ZoomOutWhenFast = !s.ZoomOutWhenFast }),
		a.check("Convex rim", a.settings.ConvexRim, func(s *settings.Settings) { s.ConvexRim = !s.ConvexRim }),
		{label: "Start at login", checkable: true, checked: a.sys.startsAtLogin(), action: func() {
			if err := a.sys.setStartAtLogin(!a.sys.startsAtLogin()); err != nil {
				debugLog("start at login: %v", err)
			}
			a.rebuildMenu()
		}},
		{separator: true},
		{label: "Circle Lens website", detail: "preview " + version, action: func() { a.sys.openAddress(websiteAddress) }},
		{label: "Quit Circle Lens", action: a.quit},
	}
	a.sys.setMenu(items, open)
}

func (a *application) choice(label, detail string, chosen bool, edit func(*settings.Settings)) menuItem {
	return menuItem{label: label, detail: detail, radio: true, checked: chosen, action: a.change(edit)}
}

func (a *application) check(label string, on bool, edit func(*settings.Settings)) menuItem {
	return menuItem{label: label, checkable: true, checked: on, action: a.change(edit)}
}
