package lens

import (
	"math"
	"testing"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestOpensFromOneTimesAndSettlesAtTheZoom(t *testing.T) {
	state := New(1.8)
	state.Open(0, false)
	start := state.Frame(0)
	if !near(start.Zoom, 1) || !near(start.GlassScale, 0.55) || !near(start.Alpha, 0) {
		t.Fatalf("start %+v", start)
	}
	if end := state.Frame(600); !near(end.Zoom, 1.8) || !near(end.GlassScale, 1) || state.Animating() {
		t.Fatalf("end %+v animating=%v", end, state.Animating())
	}
}

func TestZoomStepsGlideAndClamp(t *testing.T) {
	state := New(1.8)
	state.Open(0, false)
	state.Frame(1000)
	state.SetZoom(1000, 2.0)
	mid := state.Frame(1100).Zoom
	if !(mid > 1.8 && mid < 2.0) {
		t.Fatalf("mid-glide zoom %v", mid)
	}
	if got := state.Frame(1400).Zoom; !near(got, 2.0) {
		t.Fatalf("after glide %v", got)
	}
	state.SetZoom(1400, 99)
	if !near(state.Target(), 4) {
		t.Fatalf("clamped target %v", state.Target())
	}
}

func TestClosesBackToOneTimes(t *testing.T) {
	state := New(2)
	state.Open(0, false)
	state.Frame(1000)
	state.Close(1000)
	if state.IsOpen() || !state.IsVisible() {
		t.Fatal("should be closing")
	}
	if end := state.Frame(1400); !near(end.Zoom, 1) || !near(end.Alpha, 0) || state.IsVisible() {
		t.Fatalf("end %+v visible=%v", end, state.IsVisible())
	}
}

func TestReducedMotionSkipsAnimation(t *testing.T) {
	state := New(2)
	state.Open(0, true)
	if frame := state.Frame(0); !near(frame.Zoom, 2) || state.Animating() {
		t.Fatalf("frame %+v", frame)
	}
	state.Close(10)
	if state.IsVisible() {
		t.Fatal("should close at once")
	}
}

func TestReopeningWhileClosingTurnsRoundSmoothly(t *testing.T) {
	state := New(2)
	state.Open(0, false)
	state.Frame(1000)
	state.Close(1000)
	midClose := state.Frame(1150)
	state.Open(1150, false)
	start := state.Frame(1150)
	if math.Abs(start.GlassScale-midClose.GlassScale) > 1e-6 || math.Abs(start.Alpha-midClose.Alpha) > 1e-6 || math.Abs(start.Zoom-midClose.Zoom) > 1e-6 {
		t.Fatalf("reopening jumped: closing was %+v, opening starts %+v", midClose, start)
	}
	if end := state.Frame(1800); !near(end.Zoom, 2) || !near(end.Alpha, 1) || !near(end.GlassScale, 1) {
		t.Fatalf("did not settle open: %+v", end)
	}
}
