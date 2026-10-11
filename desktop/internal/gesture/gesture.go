// Package gesture spots a circle drawn with the mouse. It is the Go twin of shared/circle-gesture.js
// (which the browser extension uses); keep the two in step.
//
// It watches the pointer path, resampled every 5 px, and adds up how far it turns. `loops` turns (90% is
// enough) within the time window is a circle drawn that many times, provided the last turn closes on
// itself, is round (its radius steady) and big enough. Three more things keep ordinary mouse use from
// counting:
//   - a sharp reversal (> 100°) starts the count over, so a side-to-side shake never counts;
//   - so does a pause longer than 250 ms: circles are drawn in one motion, while reaching for things
//     comes in moves with stops between them, which could otherwise add up;
//   - the window is 1.3 s per circle plus 0.4 s, so turning has to be steady, not spread over a minute.
//
// After a circle (or any open or close the app reports with Hold) there is a gap, 1.5 s unless set
// otherwise, in which nothing counts: a circle must be drawn wholly after it. And after a circle the
// gesture is spent until the circling stops: a pause, or 0.6 s of movement that
// turns less than a quarter turn. People keep spinning a moment past the point it opens; without this,
// spinning on would close it again, and open it, and close it. Points with a button held (dragging,
// selecting) reset it.
// Coordinates are DIPs (physical pixels divided by the monitor's scale); times are milliseconds.
package gesture

import "math"

const (
	resamplePx       = 5
	pauseMs          = 250
	windowBaseMs     = 400
	windowPerLoopMs  = 1300
	maxPoints        = 600
	minSize          = 64
	minLengthPerLoop = 200
	// The circling has stopped once the last calmMs of movement turned less than calmTurn.
	calmMs = 600

	LoopsMin = 1
	LoopsMax = 3
)

var (
	sharpTurn = 100 * math.Pi / 180
	fullTurn  = 2 * math.Pi * 0.9
	// needTurn is how much of each circle must be drawn: nearly all of it.
	needTurn = 0.95
	calmTurn = math.Pi / 2
)

type point struct{ x, y, t float64 }

// Detector holds the recent path. It is not safe for concurrent use.
type Detector struct {
	loops      float64
	points     []point
	circleFrom int
	spent      bool      // a circle was just found; nothing counts until the circling stops
	gapMs      float64   // after an open or close, how long before a circle can count again
	holdUntil  float64   // no circle counts that began before this time
	turns      []float64 // scratch: accumulated turn at each point of the circle in progress
}

// New returns a detector that needs `loops` circles (1-3, in halves).
func New(loops float64) *Detector {
	detector := &Detector{points: make([]point, 0, maxPoints+1)}
	detector.SetLoops(loops)
	detector.SetGap(DefaultGap)
	return detector
}

// The gap between an open and a close: at least as long as the animations, at most a few seconds.
const (
	GapMin     = 0.6
	GapMax     = 5
	DefaultGap = 1.5
)

// SetGap sets the gap after an open or close, in seconds.
func (d *Detector) SetGap(seconds float64) {
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) {
		seconds = DefaultGap
	}
	d.gapMs = math.Min(GapMax, math.Max(GapMin, seconds)) * 1000
}

// Hold makes circles count only from `until` (detector time, in milliseconds) on: for an open or close
// that came from somewhere else (a shortcut, the tray), so the gap follows it too.
func (d *Detector) Hold(until float64) { d.holdUntil = math.Max(d.holdUntil, until) }

// SetLoops changes how many circles are needed.
func (d *Detector) SetLoops(loops float64) { d.loops = ClampLoops(loops) }

// Reset forgets the path.
func (d *Detector) Reset() {
	d.points = d.points[:0]
	d.circleFrom = 0
}

// Feed adds one pointer position. It returns true and the circle's centre when it completed a circle.
func (d *Detector) Feed(x, y, t float64, buttonHeld bool) (bool, float64, float64) {
	if buttonHeld {
		d.Reset()
		return false, 0, 0
	}
	if n := len(d.points); n > 0 {
		last := d.points[n-1]
		if math.Hypot(x-last.x, y-last.y) < resamplePx {
			return false, 0, 0
		}
		if t-last.t > pauseMs {
			d.Reset() // a pause: whatever comes next is a new motion
			d.spent = false
		}
	}
	d.points = append(d.points, point{x, y, t})
	if d.spent {
		if !d.calm(t) {
			if len(d.points) > maxPoints {
				d.points = append(d.points[:0], d.points[len(d.points)-maxPoints:]...)
			}
			return false, 0, 0
		}
		d.spent = false
		d.points = append(d.points[:0], point{x, y, t})
		d.circleFrom = 0
		return false, 0, 0
	}
	if t < d.holdUntil {
		// Within the gap: the count starts afresh here, so only circling after the gap counts.
		d.circleFrom = len(d.points) - 1
		return false, 0, 0
	}
	if len(d.points) > maxPoints {
		dropped := len(d.points) - maxPoints
		d.points = append(d.points[:0], d.points[dropped:]...)
		d.circleFrom = max(0, d.circleFrom-dropped)
	}
	n := len(d.points)
	if n < 3 {
		return false, 0, 0
	}
	if math.Abs(bend(d.points[n-3], d.points[n-2], d.points[n-1])) > sharpTurn {
		// A sharp reversal ends any circle in progress; the next one starts at the turn.
		d.circleFrom = n - 2
		return false, 0, 0
	}

	// The circle in progress: everything since the last reversal, within the time window.
	window := windowBaseMs + windowPerLoopMs*d.loops
	for d.circleFrom < n-3 && t-d.points[d.circleFrom].t > window {
		d.circleFrom++
	}
	loop := d.points[d.circleFrom:]
	d.turns = append(d.turns[:0], 0, 0)
	turn, length := 0.0, 0.0
	for i := 2; i < len(loop); i++ {
		turn += bend(loop[i-2], loop[i-1], loop[i])
		length += math.Hypot(loop[i].x-loop[i-1].x, loop[i].y-loop[i-1].y)
		d.turns = append(d.turns, turn)
	}
	if math.Abs(turn) < d.loops*2*math.Pi*needTurn || length < d.loops*minLengthPerLoop {
		return false, 0, 0
	}

	// The last full turn must close on itself and be round and big enough. Judging the last turn, not
	// the whole path, lets circles drift or change size from one turn to the next, as drawn ones do,
	// and lets half-turn settings (1½, 2½) work: the path's start is then across the circle.
	lastTurn := len(loop) - 1
	for lastTurn > 0 && math.Abs(turn-d.turns[lastTurn]) < fullTurn {
		lastTurn--
	}
	minX, maxX, minY, maxY := x, x, y, y
	for _, p := range loop[lastTurn:] {
		minX, maxX, minY, maxY = math.Min(minX, p.x), math.Max(maxX, p.x), math.Min(minY, p.y), math.Max(maxY, p.y)
	}
	width, height := maxX-minX, maxY-minY
	if width < minSize || height < minSize {
		return false, 0, 0
	}
	ratio := width / height
	closed := math.Hypot(x-loop[lastTurn].x, y-loop[lastTurn].y) <= 0.5*math.Max(width, height)
	if ratio <= 0.45 || ratio >= 2.2 || !closed || !round(loop[lastTurn:]) {
		return false, 0, 0
	}

	d.Reset()
	d.points = append(d.points, point{x, y, t})
	d.spent = true
	d.holdUntil = t + d.gapMs
	return true, (minX + maxX) / 2, (minY + maxY) / 2
}

// calm tells whether the circling has stopped: the last calmMs of movement turned less than a quarter
// turn. Until there is calmMs of movement to judge, it has not.
func (d *Detector) calm(now float64) bool {
	n := len(d.points)
	first := n - 1
	for first > 0 && now-d.points[first-1].t <= calmMs {
		first--
	}
	if first == 0 || now-d.points[first].t < calmMs*0.8 {
		return false
	}
	turn := 0.0
	for i := first + 2; i < n; i++ {
		turn += bend(d.points[i-2], d.points[i-1], d.points[i])
	}
	return math.Abs(turn) < calmTurn
}

// maxRadiusSpread is how uneven a drawn circle may be: the spread of its points' distances from their
// centre, relative to the mean distance. Drawn circles (wobbly, a little oval) stay well under it;
// a path that happens to wander round in a loop does not.
const maxRadiusSpread = 0.3

// round tells whether one turn of the path is round: its radius steady about its centre.
func round(turn []point) bool {
	var cx, cy float64
	for _, p := range turn {
		cx, cy = cx+p.x, cy+p.y
	}
	n := float64(len(turn))
	cx, cy = cx/n, cy/n
	var sum, sumSquares float64
	for _, p := range turn {
		r := math.Hypot(p.x-cx, p.y-cy)
		sum, sumSquares = sum+r, sumSquares+r*r
	}
	mean := sum / n
	spread := math.Sqrt(math.Max(0, sumSquares/n-mean*mean))
	return mean > 0 && spread/mean <= maxRadiusSpread
}

// bend is the signed change of heading at b going from a to c, in (-π, π].
func bend(a, b, c point) float64 {
	turn := math.Atan2(c.y-b.y, c.x-b.x) - math.Atan2(b.y-a.y, b.x-a.x)
	for turn > math.Pi {
		turn -= 2 * math.Pi
	}
	for turn <= -math.Pi {
		turn += 2 * math.Pi
	}
	return turn
}

// ClampLoops keeps a loop count between LoopsMin and LoopsMax, in halves.
func ClampLoops(loops float64) float64 {
	if math.IsNaN(loops) || math.IsInf(loops, 0) {
		return 2
	}
	return math.Min(LoopsMax, math.Max(LoopsMin, math.Round(loops*2)/2))
}
