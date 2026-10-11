// Package motion holds the lens's timing curves, the same ones shared/lens.css and shared/lens-view.js
// use (from BoundStretch): the glass springs open in 560 ms, closes in 300 ms, and zoom steps glide in
// 260 ms. The CSS cubic-bezier curves are solved exactly rather than approximated.
package motion

import "math"

const (
	InMs   = 560
	OutMs  = 300
	ZoomMs = 260

	// The label under the glass fades in after the glass has started (420 ms, 140 ms late) and fades out
	// faster than the glass closes (160 ms).
	LabelInMs    = 420
	LabelDelayMs = 140
	LabelOutMs   = 160
)

// springStops is the CSS `linear()` spring from BoundStretch (a gentle overshoot that settles).
// Each stop is {progress, value}.
var springStops = [][2]float64{
	{0, 0}, {0.014, 0.006}, {0.028, 0.025}, {0.061, 0.101}, {0.189, 0.539}, {0.253, 0.721},
	{0.315, 0.849}, {0.381, 0.937}, {0.418, 0.968}, {0.457, 0.991}, {0.501, 1.006},
	{0.55, 1.015}, {0.639, 1.017}, {0.859, 1.001}, {1, 1},
}

// Spring maps progress t (0-1) along the spring, which overshoots 1 slightly before settling.
func Spring(t float64) float64 {
	t = clamp01(t)
	for i := 1; i < len(springStops); i++ {
		if t <= springStops[i][0] {
			from, to := springStops[i-1], springStops[i]
			return from[1] + (to[1]-from[1])*(t-from[0])/(to[0]-from[0])
		}
	}
	return 1
}

var (
	// Glide is cubic-bezier(.2, .8, .2, 1): quick to start, soft to land (zoom steps, the label arriving).
	Glide = CubicBezier(0.2, 0.8, 0.2, 1)
	// Leave is cubic-bezier(.4, 0, .7, .2): slow to go, then quick (closing).
	Leave = CubicBezier(0.4, 0, 0.7, 0.2)
	// Ease is CSS `ease` (the label leaving).
	Ease = CubicBezier(0.25, 0.1, 0.25, 1)
)

// CubicBezier returns the CSS timing function cubic-bezier(x1, y1, x2, y2).
func CubicBezier(x1, y1, x2, y2 float64) func(float64) float64 {
	curve := func(u, p1, p2 float64) float64 {
		v := 1 - u
		return 3*v*v*u*p1 + 3*v*u*u*p2 + u*u*u
	}
	return func(t float64) float64 {
		t = clamp01(t)
		if t == 0 || t == 1 {
			return t
		}
		// x(u) rises monotonically for CSS curves (x1, x2 in [0, 1]), so bisection always finds u.
		low, high := 0.0, 1.0
		for i := 0; i < 24; i++ {
			middle := (low + high) / 2
			if curve(middle, x1, x2) < t {
				low = middle
			} else {
				high = middle
			}
		}
		return curve((low+high)/2, y1, y2)
	}
}

func clamp01(t float64) float64 { return math.Min(1, math.Max(0, t)) }
