package lens

import "math"

// While the pointer moves fast at a high zoom, the lens zooms out, so what rushes past stays readable
// and you can see where you are going; as the pointer slows it zooms back in. Scrolling under the lens
// counts the same way: the faster the content scrolls past, the further out. Out is quick, back in is
// gentler, like a camera settling. shared/lens-fast.js is the browser's twin; keep the two in step.
//
// How far out is the same at every zoom: to half of it, but not below 1× (no magnification). So 4× eases
// to 2×, 3× to 1.5×, and 1.8× to 1×.
const (
	// FastFrom and FastFull are pointer speeds in DIPs per second: below FastFrom the zoom is left
	// alone, at FastFull it is all the way out. Reading along a line is well under FastFrom.
	FastFrom = 600
	FastFull = 2000

	fastLowest    = 1
	fastOutMs     = 80  // how quickly it zooms out as the pointer speeds up
	fastBackMs    = 200 // how gently it zooms back in as it slows
	speedSmoothMs = 40  // smooths the measured speed, so one jumpy reading does not count
	speedFadeMs   = 60  // once the pointer stops reporting, its speed fades out at this rate
	stillAfterMs  = 40  // no pointer report for this long means it has stopped
	// Scrolling: a burst after a pause is timed as if over scrollFirstMs, so one notch on its own does
	// not zoom out; the scroll speed is smoothed over scrollSmoothMs and fades over scrollFadeMs once
	// the scrolling stops (after scrollStillMs).
	scrollFirstMs  = 250
	scrollSmoothMs = 80
	scrollFadeMs   = 100
	scrollStillMs  = 100
	settledAmount  = 0.01 // under this the zoom is within 0.02× of where it belongs: snap the rest
	settledSpeedPx = 20
)

// fastMotion tracks the pointer's speed and how far the lens has zoomed out for it (0 to 1).
type fastMotion struct {
	enabled        bool
	speed          float64 // DIPs per second, smoothed
	scrollSpeed    float64 // how fast content scrolls past, DIPs per second, smoothed
	lastScroll     float64
	hasScroll      bool
	amount         float64 // 0 at the set zoom, 1 all the way out
	lastX, lastY   float64
	lastPointer    float64
	lastAdvance    float64
	hasPointer     bool
	advancedBefore bool
}

// FastZoomLow is the zoom the lens eases out to while moving fast, for a set zoom of `zoom`.
func FastZoomLow(zoom float64) float64 { return math.Min(zoom, math.Max(fastLowest, zoom/2)) }

// SetZoomOutWhenFast turns the behaviour on or off.
func (s *State) SetZoomOutWhenFast(on bool) {
	s.fast.enabled = on
	if !on {
		s.fast.amount, s.fast.speed = 0, 0
	}
}

// Pointer reports where the pointer is (in DIPs) at `now`; call it once per frame.
func (s *State) Pointer(now, x, y float64) {
	f := &s.fast
	if f.hasPointer {
		if dt := now - f.lastPointer; dt >= 1 {
			if moved := math.Hypot(x-f.lastX, y-f.lastY); moved > 0 || dt < 200 {
				instant := moved / dt * 1000
				f.speed += (instant - f.speed) * (1 - math.Exp(-dt/speedSmoothMs))
			}
		} else {
			return
		}
	}
	f.lastX, f.lastY, f.lastPointer, f.hasPointer = x, y, now, true
}

// Scrolled reports a scroll under the lens of `distance` DIPs of content (a wheel notch is about 100).
// The faster scrolls come, the faster the content moves past, and the further the lens zooms out.
func (s *State) Scrolled(now, distance float64) {
	f := &s.fast
	dt := now - f.lastScroll
	if !f.hasScroll || dt > scrollFirstMs {
		dt = scrollFirstMs
	}
	dt = math.Max(dt, 8)
	f.scrollSpeed += (math.Abs(distance)/dt*1000 - f.scrollSpeed) * (1 - math.Exp(-dt/scrollSmoothMs))
	f.lastScroll, f.hasScroll = now, true
}

// Speeding is true while the pointer moves fast enough to start zooming out.
func (s *State) Speeding() bool {
	return s.fast.enabled && math.Max(s.fast.speed, s.fast.scrollSpeed) > FastFrom
}

// Fast is how far the lens is zoomed out for speed right now (0 to 1).
func (s *State) Fast() float64 { return s.fast.amount }

// advanceFast moves the zoom-out along to `now`.
func (s *State) advanceFast(now float64) {
	f := &s.fast
	if !f.advancedBefore {
		f.lastAdvance, f.advancedBefore = now, true
		return
	}
	dt := now - f.lastAdvance
	if dt <= 0 {
		return
	}
	f.lastAdvance = now
	if !f.enabled || s.reduced {
		f.amount, f.speed, f.scrollSpeed = 0, 0, 0
		return
	}
	if now-f.lastPointer > stillAfterMs {
		f.speed *= math.Exp(-dt / speedFadeMs)
	}
	if now-f.lastScroll > scrollStillMs {
		f.scrollSpeed *= math.Exp(-dt / scrollFadeMs)
	}
	want := smoothstep((math.Max(f.speed, f.scrollSpeed) - FastFrom) / (FastFull - FastFrom))
	rate := fastBackMs
	if want > f.amount {
		rate = fastOutMs
	}
	f.amount += (want - f.amount) * (1 - math.Exp(-dt/float64(rate)))
	if f.amount < settledAmount && want == 0 {
		f.amount = 0
	}
}

// settled is true once there is nothing left to ease.
func (f *fastMotion) settled() bool {
	return f.amount == 0 && f.speed < settledSpeedPx && f.scrollSpeed < settledSpeedPx
}

func smoothstep(t float64) float64 {
	t = clamp01(t)
	return t * t * (3 - 2*t)
}
