package lens

import (
	"math"
	"testing"

	"circlelens/internal/motion"
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

// moveAt reports the pointer moving right at `speed` DIPs per second from `from` to `to`, a frame
// every 8 ms, and returns the last frame.
func moveAt(state *State, speed, from, to float64, x *float64) Frame {
	var frame Frame
	for now := from; now <= to; now += 8 {
		*x += speed * 8 / 1000
		state.Pointer(now, *x, 500)
		frame = state.Frame(now)
	}
	return frame
}

func openSteady(zoom float64) *State {
	state := New(zoom)
	state.Open(0, false)
	state.Frame(600)
	return state
}

func TestFastMovementZoomsOutThenBackIn(t *testing.T) {
	state := openSteady(4)
	x := 0.0
	moveAt(state, 200, 600, 1000, &x) // reading pace: no change
	if zoom := state.Frame(1000).Zoom; !near(zoom, 4) {
		t.Fatalf("reading pace changed the zoom to %v", zoom)
	}
	fast := moveAt(state, 3000, 1008, 1400, &x)
	if fast.Zoom > 2.2 {
		t.Fatalf("fast movement only zoomed out to %v, want about 2", fast.Zoom)
	}
	if !state.Animating() {
		t.Fatal("should keep drawing while it eases")
	}
	// The pointer stops: no more reports. It eases back in.
	var back Frame
	for now := 1408.0; now <= 2600; now += 8 {
		back = state.Frame(now)
	}
	if !near(back.Zoom, 4) || state.Animating() {
		t.Fatalf("after stopping: zoom %v, animating %v", back.Zoom, state.Animating())
	}
}

func TestZoomingOutIsQuickerThanZoomingBackIn(t *testing.T) {
	state := openSteady(4)
	x := 0.0
	out := moveAt(state, 3000, 600, 680, &x).Zoom // 80 ms of speed
	for now := 688.0; now < 1400; now += 8 {
		moveAt(state, 3000, now, now, &x)
	}
	at := 1400.0
	for ; at <= 1480; at += 8 {
		state.Frame(at) // 80 ms after stopping
	}
	backAfter80 := state.Frame(at).Zoom
	if outBy, backBy := 4-out, backAfter80-2; outBy <= backBy {
		t.Fatalf("in 80 ms it zoomed out by %.2f but back in by %.2f; out should be quicker", outBy, backBy)
	}
}

func TestTheSameMultiplierAtLowZoom(t *testing.T) {
	low := openSteady(1.8)
	x := 0.0
	if zoom := moveAt(low, 3000, 600, 1000, &x).Zoom; zoom > 1.1 {
		t.Fatalf("at 1.8x fast movement only went to %v, want about 1", zoom)
	}
	off := openSteady(4)
	off.SetZoomOutWhenFast(false)
	if zoom := moveAt(off, 3000, 600, 1000, &x).Zoom; !near(zoom, 4) {
		t.Fatalf("with the setting off the zoom changed to %v", zoom)
	}
}

// scrollAt scrolls one wheel notch (100 DIPs) every `every` ms for `forMs` at `zoom`, and returns the
// lowest zoom it reached.
func scrollAt(zoom, every, forMs float64) float64 {
	state := openSteady(zoom)
	state.Pointer(600, 100, 100)
	lowest := zoom
	nextNotch := 610.0
	for now := 610.0; now <= 610+forMs; now += 8 {
		if now >= nextNotch {
			state.Scrolled(now, 100)
			nextNotch += every
		}
		lowest = math.Min(lowest, state.Frame(now).Zoom)
	}
	return lowest
}

func TestFasterScrollingZoomsOutFurther(t *testing.T) {
	slow, brisk, fast := scrollAt(4, 400, 1600), scrollAt(4, 90, 600), scrollAt(4, 30, 600)
	for _, zoom := range []float64{4, 3, 1.8} {
		t.Logf("%vx lens: a notch every 400 ms → %.2fx, every 90 ms → %.2fx, every 30 ms → %.2fx",
			zoom, scrollAt(zoom, 400, 1600), scrollAt(zoom, 90, 600), scrollAt(zoom, 30, 600))
	}
	if slow < 3.95 {
		t.Fatalf("a notch every 400 ms zoomed out to %.2f; reading-pace scrolling should not", slow)
	}
	if !(brisk < 3.8 && fast < brisk-0.3) {
		t.Fatalf("brisk scrolling took it to %.2f and fast to %.2f; faster should go further out", brisk, fast)
	}
	if fast > 2.3 {
		t.Fatalf("fast scrolling only reached %.2f, want about 2", fast)
	}
}

func TestClosingShrinksBeforeItFades(t *testing.T) {
	state := New(2)
	state.Open(0, false)
	state.Frame(1000)
	state.Close(1000)
	early := state.Frame(1000 + 0.4*motion.OutMs)
	if early.Alpha < 0.999 || early.GlassScale > 0.6 {
		t.Fatalf("40%% into closing: alpha %.2f, scale %.2f; want still opaque and well shrunk", early.Alpha, early.GlassScale)
	}
	late := state.Frame(1000 + 0.9*motion.OutMs)
	if late.Alpha > 0.4 || late.GlassScale > 0.35 {
		t.Fatalf("90%% into closing: alpha %.2f, scale %.2f; want mostly faded and small", late.Alpha, late.GlassScale)
	}
}
