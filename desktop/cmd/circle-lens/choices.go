package main

import "fmt"

// The choices the menus offer, the same on every system.
var (
	loopChoices = []struct {
		loops float64
		label string
	}{
		{1, "1 circle"},
		{1.5, "1½ circles"},
		{2, "2 circles"},
		{2.5, "2½ circles"},
		{3, "3 circles"},
	}
	zoomChoices = []float64{1.5, 1.8, 2, 2.5, 3, 4}
	gapChoices  = []float64{1, 1.5, 2, 3}
	sizeChoices = []struct {
		radius float64
		label  string
	}{{100, "Small"}, {150, "Medium"}, {200, "Large"}, {300, "Extra large"}}
)

func loopsLabel(loops float64) string {
	for _, choice := range loopChoices {
		if choice.loops == loops {
			return "a circle " + map[float64]string{1: "once", 1.5: "one and a half times", 2: "twice", 2.5: "two and a half times", 3: "three times"}[loops]
		}
	}
	return "a circle twice"
}

func trimZoom(zoom float64) string {
	text := fmt.Sprintf("%.1f", zoom)
	if text[len(text)-2:] == ".0" {
		return text[:len(text)-2]
	}
	return text
}
