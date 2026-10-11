package gesture

import (
	"math"
	"testing"
)

type sample struct {
	x, y, t float64
	held    bool
}

// circlePath samples every 16 ms around a circle (or ellipse) drawn `loops` times over durationMs.
func circlePath(cx, cy, rx, ry, loops, durationMs, start float64) []sample {
	steps := int(math.Round(durationMs / 16))
	path := make([]sample, 0, steps+1)
	for i := 0; i <= steps; i++ {
		angle := float64(i) / float64(steps) * loops * 2 * math.Pi
		path = append(path, sample{cx + rx*math.Cos(angle), cy + ry*math.Sin(angle), start + float64(i)*16, false})
	}
	return path
}

func twoCircles() []sample { return circlePath(400, 300, 80, 80, 2, 1600, 1000) }

func run(path []sample, loops float64) (hits int, x, y float64) {
	detector := New(loops)
	for _, s := range path {
		if ok, cx, cy := detector.Feed(s.x, s.y, s.t, s.held); ok {
			hits, x, y = hits+1, cx, cy
		}
	}
	return
}

func TestTwoCirclesNearTheirCentre(t *testing.T) {
	hits, x, y := run(twoCircles(), 2)
	if hits != 1 || math.Abs(x-400) > 10 || math.Abs(y-300) > 10 {
		t.Fatalf("hits=%d centre=(%.0f, %.0f)", hits, x, y)
	}
}

func TestCounterClockwise(t *testing.T) {
	path := twoCircles()
	for i := range path {
		path[i].y = 600 - path[i].y
	}
	if hits, _, _ := run(path, 2); hits != 1 {
		t.Fatalf("hits=%d", hits)
	}
}

func TestOneCircleOnlyWhenOneIsNeeded(t *testing.T) {
	path := circlePath(400, 300, 80, 80, 1.05, 800, 1000)
	if hits, _, _ := run(path, 2); hits != 0 {
		t.Fatalf("two needed: hits=%d", hits)
	}
	if hits, _, _ := run(path, 1); hits != 1 {
		t.Fatalf("one needed: hits=%d", hits)
	}
}

func TestLinesAndShakesNeverCount(t *testing.T) {
	var line, shake []sample
	for i := 0; i < 120; i++ {
		line = append(line, sample{100 + float64(i)*6, 200, 1000 + float64(i)*16, false})
		shake = append(shake, sample{300 + float64(i%2)*120, 200 + float64(i), 1000 + float64(i)*16, false})
	}
	if hits, _, _ := run(line, 2); hits != 0 {
		t.Fatal("a line counted")
	}
	if hits, _, _ := run(shake, 2); hits != 0 {
		t.Fatal("a shake counted")
	}
}

func TestTooSlowTooSmallTooFlat(t *testing.T) {
	cases := map[string][]sample{
		"slow": circlePath(400, 300, 80, 80, 2, 6000, 1000),
		"tiny": circlePath(400, 300, 15, 15, 2, 1600, 1000),
		"flat": circlePath(400, 300, 200, 40, 2, 1600, 1000),
	}
	for name, path := range cases {
		if hits, _, _ := run(path, 2); hits != 0 {
			t.Errorf("%s counted", name)
		}
	}
}

func TestHeldButtonResets(t *testing.T) {
	path := twoCircles()
	for i := range path {
		path[i].held = true
	}
	if hits, _, _ := run(path, 2); hits != 0 {
		t.Fatal("a drag counted")
	}
}

func TestSpinningOnDoesNotToggleAgain(t *testing.T) {
	// Two circles open it; carrying on for four more, without stopping, must not close it.
	path := circlePath(400, 300, 80, 80, 6, 4000, 1000)
	if hits, _, _ := run(path, 2); hits != 1 {
		t.Fatalf("hits=%d, want 1", hits)
	}
}

func TestAPauseReArmsAfterTheGap(t *testing.T) {
	first := twoCircles()
	end := first[len(first)-1].t
	path := append(append([]sample{}, first...), circlePath(400, 300, 80, 80, 2, 1600, end+DefaultGap*1000+100)...)
	if hits, _, _ := run(path, 2); hits != 2 {
		t.Fatalf("hits=%d, want 2", hits)
	}
}

func TestCirclesWithinTheGapDoNotCount(t *testing.T) {
	// Opened; then, after a short pause, two quick circles that start inside the gap: still open.
	first := twoCircles()
	end := first[len(first)-1].t
	path := append(append([]sample{}, first...), circlePath(400, 300, 80, 80, 2.2, 1000, end+300)...)
	if hits, _, _ := run(path, 2); hits != 1 {
		t.Fatalf("hits=%d, want 1", hits)
	}
}

func TestHoldStartsTheGapFromElsewhere(t *testing.T) {
	// Opened by a shortcut at t=1000: circles drawn straight away do not close it.
	detector := New(2)
	detector.Hold(1000 + DefaultGap*1000)
	hits := 0
	for _, s := range circlePath(400, 300, 80, 80, 2.2, 1200, 1000) {
		if ok, _, _ := detector.Feed(s.x, s.y, s.t, false); ok {
			hits++
		}
	}
	if hits != 0 {
		t.Fatalf("hits=%d, want 0", hits)
	}
}

func TestOrdinaryMovementReArms(t *testing.T) {
	first := twoCircles()
	end := first[len(first)-1].t
	path := append([]sample{}, first...)
	// 1.6 s of moving away in a straight line (past the gap), then two circles somewhere else.
	for i := 1; i <= 100; i++ {
		path = append(path, sample{480 + float64(i)*8, 300, end + float64(i)*16, false})
	}
	path = append(path, circlePath(1400, 300, 80, 80, 2, 1600, end+100*16+16)...)
	if hits, _, _ := run(path, 2); hits != 2 {
		t.Fatalf("hits=%d, want 2", hits)
	}
}
