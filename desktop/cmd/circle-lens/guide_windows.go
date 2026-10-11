//go:build windows

package main

// Two more screens in the setup window's look, opened from the panel and the menu:
//
//   - How to use: a four-page tour (open it, read and zoom, close it, make it yours), with the keys as
//     they are actually set;
//   - What's new: this version's release notes, from CHANGELOG.md (desktop/build.mjs copies the section
//     into whatsnew.md, embedded here; a test checks the two match).

import (
	_ "embed"
	"math"
	"strings"
	"time"

	"circlelens/internal/settings"
	"circlelens/internal/shortcut"
)

//go:embed whatsnew.md
var releaseNotes string

const guidePages = 4

// runGuideWindow opens the tour; runNewsWindow the release notes. One of these windows at a time.
func runGuideWindow() { openInfoWindow(screenGuide) }
func runNewsWindow()  { openInfoWindow(screenNews) }

func openInfoWindow(screen setupScreen) {
	if theSetup != nil && theSetup.window != 0 && !theSetup.finished {
		procSetForegroundWindow.Call(theSetup.window)
		return
	}
	w := newSetupWindow()
	w.screen = screen
	w.run()
}

// guideKeys are the keys the tour names: the ones set now, or the defaults.
func guideKeys() (open, zoomIn, zoomOut, close string) {
	s := settings.Defaults
	if app != nil {
		s = app.settings
	}
	open = shortcut.ParseOrDefault(s.Shortcut).String()
	if app != nil && app.shortcut.String() != "" {
		open = app.shortcut.String()
	}
	return open, s.ZoomInKeys, s.ZoomOutKeys, s.CloseKey
}

// keyText writes a shortcut for richLine: "Shift twice" → "tap [Shift] twice", "Ctrl+Alt+Z" → "[Ctrl+Alt+Z]".
func keyText(shortcutText string) string {
	if name, ok := strings.CutSuffix(shortcutText, " twice"); ok {
		return "tap [" + name + "] twice"
	}
	return "press [" + shortcutText + "]"
}

// guidePage is one page of the tour: its label, title (two lines, the second in the gradient) and lede.
func guidePage(page int) (label, first, second string, lede []string) {
	open, zoomIn, zoomOut, closeKey := guideKeys()
	switch page {
	case 0:
		return "OPEN IT", "Draw a circle", "twice.", []string{
			"Anywhere on screen, about the size of a coin,",
			"in one smooth motion. Or " + keyText(open) + ".",
		}
	case 1:
		return "READ", "Move to read.", "Zoom to taste.", []string{
			"The lens follows the pointer. [Ctrl] + wheel,",
			"or [" + zoomIn + "] and [" + zoomOut + "], change the zoom.",
		}
	case 2:
		second := "Close it the same way."
		lines := []string{"Draw two more circles, or " + keyText(open) + "."}
		if closeKey != "" {
			lines = append(lines, "Or press ["+closeKey+"], your close key.")
		} else {
			lines = append(lines, "You can set a close key, like [Esc], too.")
		}
		return "CLOSE IT", "Done reading?", second, lines
	default:
		return "MAKE IT YOURS", "Your lens,", "your way.", []string{
			"Click the magnifier by the clock for the zoom, size,",
			"keys, and how many circles open it.",
		}
	}
}

/* ---------- release notes ---------- */

// noteLine is one line of the release notes, wrapped: its depth (0 a point, 1 a sub-point), whether it
// starts its point (and gets the bullet), its bold lead-in if it has one, and the rest.
type noteLine struct {
	depth  int
	first  bool
	lead   string
	text   string
	indent float64
}

// plainMarkdown strips the little Markdown the changelog uses: **bold**, `code`, [text](address).
func plainMarkdown(text string) string {
	text = strings.ReplaceAll(text, "**", "")
	text = strings.ReplaceAll(text, "`", "")
	for {
		open := strings.Index(text, "](")
		if open < 0 {
			break
		}
		closeAt := strings.Index(text[open:], ")")
		start := strings.LastIndex(text[:open], "[")
		if closeAt < 0 || start < 0 {
			break
		}
		text = text[:start] + text[start+1:open] + text[open+closeAt+1:]
	}
	return text
}

// noteLines lays the release notes out to fit `width` pixels.
func (w *setupWindow) noteLines(width float64) []noteLine {
	s := w.scale
	var out []noteLine
	for _, raw := range strings.Split(strings.TrimSpace(releaseNotes), "\n") {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		depth := 0
		if strings.HasPrefix(raw, "  ") {
			depth = 1
		}
		item := strings.TrimSpace(raw)
		item = strings.TrimPrefix(item, "- ")
		lead, rest := "", item
		if strings.HasPrefix(item, "**") {
			if end := strings.Index(item[2:], "**"); end >= 0 {
				lead, rest = item[2:2+end], strings.TrimSpace(item[2+end+2:])
			}
		}
		lead, rest = plainMarkdown(lead), plainMarkdown(rest)
		indent := (18 + float64(depth)*18) * s
		available := width - indent
		leadWidth := 0.0
		if lead != "" {
			leadWidth = w.measure(lead+" ", styleNoteLead)
		}
		lines := w.wrapFirst(rest, styleNote, available-leadWidth, available)
		if len(lines) == 0 {
			lines = []string{""}
		}
		for i, line := range lines {
			l := noteLine{depth: depth, first: i == 0, text: line, indent: indent}
			if i == 0 {
				l.lead = lead
			}
			out = append(out, l)
		}
	}
	return out
}

var (
	styleNote     = textStyle{face: faceText, size: 13, weight: fwNormal}
	styleNoteLead = textStyle{face: faceText, size: 13, weight: fwSemibold}
)

// wrapFirst wraps text with a narrower first line (it follows a bold lead-in).
func (w *setupWindow) wrapFirst(text string, style textStyle, firstWidth, width float64) []string {
	var lines []string
	current := ""
	limit := firstWidth
	for _, word := range strings.Fields(text) {
		candidate := strings.TrimSpace(current + " " + word)
		if w.measure(candidate, style) > limit && (current != "" || limit < width) {
			lines = append(lines, current)
			current, limit = word, width
			continue
		}
		current = candidate
	}
	if current != "" || len(lines) == 0 {
		lines = append(lines, current)
	}
	return lines
}

// newsBox is where the notes are drawn (and scroll), in window pixels.
func (w *setupWindow) newsBox() (x0, y0, x1, y1 float64) {
	s := w.scale
	left, top := setupMargin*s, setupMargin*s
	return left + 40*s, top + 132*s, left + (setupWidth-40)*s, top + 448*s
}

// drawNews draws the notes in their box, scrolled, faded at the edges where more is hidden.
func (w *setupWindow) drawNews() {
	s := w.scale
	x0, y0, x1, y1 := w.newsBox()
	lines := w.noteLines(x1 - x0)
	lineHeight := 21 * s
	gap := 7 * s
	total := 0.0
	for i, l := range lines {
		if l.first && i > 0 {
			total += gap
		}
		total += lineHeight
	}
	w.newsHeight = total
	w.scroll = math.Max(0, math.Min(w.scroll, total-(y1-y0)))
	y := y0 - w.scroll
	for i, l := range lines {
		if l.first && i > 0 {
			y += gap
		}
		if y+lineHeight > y0-1 && y < y1+1 {
			x := x0 + l.indent
			if l.first {
				if l.depth == 0 {
					w.circle(x-10*s, y+lineHeight/2, 2.4*s, brand.muted, 1)
				} else {
					w.line(x-12*s, y+lineHeight/2, x-6*s, y+lineHeight/2, 1.4*s, brand.dim, 1)
				}
			}
			if l.lead != "" {
				x += w.write(l.lead+" ", x, y, lineHeight, styleNoteLead, brand.text, alignLeft)
			}
			w.write(l.text, x, y, lineHeight, styleNote, brand.body, alignLeft)
		}
		y += lineHeight
	}
	// Cover what spills past the box with the panel's own background, faded, so the edges read as "more".
	fadeLength := 26 * s
	for _, edge := range []struct {
		from, to float64
		show     bool
	}{{y0 - lineHeight - gap, y0, w.scroll > 0}, {y1, y1 + lineHeight + gap, w.scroll < total-(y1-y0)-1}} {
		for py := max(0, int(edge.from)); py < min(w.canvas.Height, int(math.Ceil(edge.to))); py++ {
			copy(w.canvas.Pixels[(py*w.canvas.Width+int(x0-20*s))*4:(py*w.canvas.Width+int(x1+4*s))*4],
				w.layer.background[(py*w.canvas.Width+int(x0-20*s))*4:(py*w.canvas.Width+int(x1+4*s))*4])
		}
		if !edge.show {
			continue
		}
		for i := 0; i < int(fadeLength); i++ {
			py := int(y0) + i
			amount := 1 - float64(i)/fadeLength
			if edge.from >= y1-1 {
				py = int(y1) - 1 - i
			}
			for px := int(x0 - 20*s); px < int(x1+4*s); px++ {
				j := (py*w.canvas.Width + px) * 4
				for c := 0; c < 3; c++ {
					w.canvas.Pixels[j+c] = byte(float64(w.canvas.Pixels[j+c])*(1-amount*0.85) + float64(w.layer.background[j+c])*amount*0.85 + 0.5)
				}
			}
		}
	}
}

// guideDots marks which page of the tour is showing.
func (w *setupWindow) guideDots(cx, y float64) {
	s := w.scale
	spacing := 14 * s
	left := cx - spacing*float64(guidePages-1)/2
	for i := 0; i < guidePages; i++ {
		x := left + float64(i)*spacing
		if i == w.page {
			w.fill(x-8*s, y-2.5*s, x+8*s, y+2.5*s, 2.5*s, brand.text, 1)
			continue
		}
		w.circle(x, y, 2.5*s, brand.switchOff, 1)
	}
}

// guideSpin is the ring drawn around the lens on the tour's first page: a circle being drawn, over and over.
func (w *setupWindow) guideSpin(cx, cy, r float64) {
	t := math.Mod(time.Since(w.screenFrom).Seconds(), 2.2) / 2.2
	sweep := smooth(math.Min(1, t/0.75))
	fade := 1.0
	if t > 0.8 {
		fade = 1 - (t-0.8)/0.2
	}
	if sweep > 0 && fade > 0 {
		w.arc(cx, cy, r+9*w.scale, 2.5*w.scale, -0.6, sweep*2*math.Pi*0.98, func(float64) [3]byte { return brand.text }, fade)
	}
}
