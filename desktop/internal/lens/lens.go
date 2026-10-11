// Package lens is the lens's state machine, shared by every platform backend. For any moment it says
// how the glass looks; a backend only draws the Frame it is given. The motion is the CSS of
// BoundStretch's reading lens (shared/lens.css, shared/lens-view.js), keyframe for keyframe:
//
//	open   560 ms spring: the glass grows from 55%, fades in and unblurs (both done by 60%), while the
//	       content zooms from 1× to the zoom; the label slides down into place 140 ms later (420 ms).
//	zoom   260 ms glide to the new zoom.
//	close  300 ms: the glass shrinks to 62%, fades and blurs out while the zoom falls back to 1×; the
//	       label lifts away in 160 ms.
package lens

import (
	"math"

	"circlelens/internal/motion"
	"circlelens/internal/settings"
)

// BlurMax is the blur, in CSS pixels, the glass opens from and closes to.
const BlurMax = 8

// LabelLiftMax is how far, in CSS pixels, the label sits above its place while it fades.
const LabelLiftMax = 6

// Frame is what to draw right now.
type Frame struct {
	// GlassScale is the glass's size relative to its full radius (0.55-1.02 with the spring).
	GlassScale float64
	// Alpha is the glass's opacity, shadow included (0-1).
	Alpha float64
	// Blur is the glass's blur in CSS pixels (0-BlurMax).
	Blur float64
	// Zoom is the magnification of the content inside the glass (never below 1).
	Zoom float64
	// LabelAlpha and LabelLift are the zoom label's opacity and how far it sits above its place.
	LabelAlpha, LabelLift float64
}

var (
	steadyFrame = func(zoom float64) Frame { return Frame{GlassScale: 1, Alpha: 1, Zoom: zoom, LabelAlpha: 1} }
	// openFrom is where opening starts (lens.css's lens-in `from`); goneFrame where closing ends.
	openFrom  = Frame{GlassScale: 0.55, Alpha: 0, Blur: BlurMax, Zoom: 1, LabelLift: LabelLiftMax}
	goneFrame = Frame{GlassScale: 0.62, Alpha: 0, Blur: BlurMax, Zoom: 1, LabelLift: LabelLiftMax}
)

type phase int

const (
	closed phase = iota
	opening
	steady
	closing
)

// State tracks one lens. Times are milliseconds from any monotonic clock.
type State struct {
	phase   phase
	target  float64
	from    Frame
	started float64
	// A zoom step in progress, independent of opening.
	glideFrom, glideStarted float64
	gliding                 bool
	reduced                 bool
}

// New returns a closed lens that opens at `zoom`.
func New(zoom float64) *State { return &State{target: settings.ClampZoom(zoom)} }

// IsOpen is true from Open until Close begins.
func (s *State) IsOpen() bool { return s.phase == opening || s.phase == steady }

// IsVisible is true from Open until the closing animation has finished.
func (s *State) IsVisible() bool { return s.phase != closed }

// Animating is true while frames change on their own.
func (s *State) Animating() bool { return s.phase == opening || s.phase == closing || s.gliding }

// Target is the zoom the lens is at, or heading to.
func (s *State) Target() float64 { return s.target }

// Open starts opening. If the lens is still closing, it turns round from wherever it is rather than
// jumping back to the start. With reduced motion the lens appears at once.
func (s *State) Open(now float64, reducedMotion bool) {
	s.reduced = reducedMotion
	from := openFrom
	if s.phase == closing {
		from = s.Frame(now) // where the closing animation has got to (it reads s.from, so ask first)
	}
	s.from = from
	s.phase, s.started, s.gliding = opening, now, false
	if reducedMotion {
		s.phase = steady
	}
}

// StartClock restarts an opening from its first frame at `now`. A backend calls it when that frame is
// actually on screen, so whatever it took to get there is not taken out of the animation.
func (s *State) StartClock(now float64) {
	if s.phase == opening {
		s.started = now
	}
}

// Close starts closing from wherever the lens is now.
func (s *State) Close(now float64) {
	if !s.IsOpen() {
		return
	}
	s.from = s.Frame(now)
	s.phase, s.started, s.gliding = closing, now, false
	if s.reduced {
		s.phase = closed
	}
}

// SetZoom changes the zoom; while open it glides there.
func (s *State) SetZoom(now, zoom float64) {
	next := settings.ClampZoom(zoom)
	if next == s.target {
		return
	}
	if s.phase == steady && !s.reduced {
		s.glideFrom, s.glideStarted, s.gliding = s.Frame(now).Zoom, now, true
	}
	s.target = next
}

// Frame returns what to draw at `now`, and moves the state along (opening → steady, closing → closed).
func (s *State) Frame(now float64) Frame {
	elapsed := now - s.started
	switch s.phase {
	case opening:
		if elapsed >= motion.InMs {
			s.phase = steady
			return steadyFrame(s.target)
		}
		t := elapsed / motion.InMs
		spring := motion.Spring(t)
		// Opacity and blur reach their end at the 60% keyframe, on the same spring.
		early := clamp01(motion.Spring(math.Min(1, t/0.6)))
		label := motion.Glide((elapsed - motion.LabelDelayMs) / motion.LabelInMs)
		from := s.from
		return Frame{
			GlassScale: from.GlassScale + (1-from.GlassScale)*spring,
			Alpha:      clamp01(from.Alpha + (1-from.Alpha)*early),
			Blur:       math.Max(0, from.Blur*(1-early)),
			Zoom:       math.Max(1, from.Zoom+(s.target-from.Zoom)*spring),
			LabelAlpha: clamp01(from.LabelAlpha + (1-from.LabelAlpha)*label),
			LabelLift:  from.LabelLift * (1 - label),
		}
	case steady:
		frame := steadyFrame(s.target)
		if s.gliding {
			if t := (now - s.glideStarted) / motion.ZoomMs; t >= 1 {
				s.gliding = false
			} else {
				frame.Zoom = s.glideFrom + (s.target-s.glideFrom)*motion.Glide(t)
			}
		}
		return frame
	case closing:
		if elapsed >= motion.OutMs {
			s.phase = closed
			return goneFrame
		}
		e := motion.Leave(elapsed / motion.OutMs)
		label := motion.Ease(elapsed / motion.LabelOutMs)
		from := s.from
		return Frame{
			GlassScale: from.GlassScale + (goneFrame.GlassScale-from.GlassScale)*e,
			Alpha:      from.Alpha * (1 - e),
			Blur:       from.Blur + (BlurMax-from.Blur)*e,
			Zoom:       from.Zoom + (1-from.Zoom)*e,
			LabelAlpha: from.LabelAlpha * (1 - label),
			LabelLift:  from.LabelLift + (LabelLiftMax-from.LabelLift)*label,
		}
	}
	return goneFrame
}

func clamp01(value float64) float64 { return math.Min(1, math.Max(0, value)) }
