package gesture

import (
	"fmt"
	"math"
	"math/rand"
	"testing"
)

// These tests draw the way people do, not the way geometry does: circles that wobble, drift, are not
// quite round and speed up and slow down, sampled like a real mouse. They check two things at once:
// that drawing the gesture works nearly every time, and that ordinary mouse use never sets it off.

const sampleMs = 8 // a 125 Hz mouse

// handCircle is a circle drawn by hand: `loops` turns at `loopsPerSecond`, radius `radius`.
func handCircle(random *rand.Rand, loops, loopsPerSecond, radius float64, clockwise bool, start float64) []sample {
	cx, cy := 600+random.Float64()*400, 400+random.Float64()*300
	aspect := 0.8 + random.Float64()*0.4                               // not quite round
	tilt := random.Float64() * math.Pi                                 // and not axis-aligned
	driftX, driftY := random.NormFloat64()*12, random.NormFloat64()*12 // the centre wanders, px per loop
	phase := random.Float64() * 2 * math.Pi
	durationMs := loops / loopsPerSecond * 1000
	var path []sample
	angle := 0.0
	for t := 0.0; t <= durationMs; t += sampleMs {
		progress := t / durationMs
		// Speed varies through each turn (people slow at the top and bottom).
		speed := 1 + 0.3*math.Sin(2*angle+phase)
		angle += speed * loops * 2 * math.Pi * sampleMs / durationMs
		r := radius * (1 + 0.12*math.Sin(angle*1.7+phase)) // the radius breathes
		x, y := r*math.Cos(angle), r*aspect*math.Sin(angle)
		if !clockwise {
			y = -y
		}
		x, y = x*math.Cos(tilt)-y*math.Sin(tilt), x*math.Sin(tilt)+y*math.Cos(tilt)
		x += cx + driftX*progress*loops + random.NormFloat64()*0.8
		y += cy + driftY*progress*loops + random.NormFloat64()*0.8
		path = append(path, sample{x, y, start + t, false})
	}
	return path
}

// Ordinary use, none of which should ever open the lens.

func readingSweeps(random *rand.Rand) []sample { // following lines of text, then back to the start
	var path []sample
	t := 0.0
	y := 200.0
	for line := 0; line < 12; line++ {
		for x := 200.0; x < 1100; x += 6 + random.Float64()*4 {
			path = append(path, sample{x, y + random.NormFloat64()*1.5, t, false})
			t += sampleMs
		}
		for x := 1100.0; x > 200; x -= 40 {
			y += 2
			path = append(path, sample{x, y, t, false})
			t += sampleMs
		}
	}
	return path
}

func curvedReaches(random *rand.Rand) []sample { // pointing at things along curved paths
	var path []sample
	t := 0.0
	x, y := 500.0, 500.0
	for reach := 0; reach < 60; reach++ {
		tx, ty := 100+random.Float64()*1700, 100+random.Float64()*900
		// A quadratic Bézier with a control point off to one side: a natural arc.
		mx, my := (x+tx)/2+random.NormFloat64()*250, (y+ty)/2+random.NormFloat64()*250
		steps := 25 + random.Intn(40)
		for i := 1; i <= steps; i++ {
			u := float64(i) / float64(steps)
			px := (1-u)*(1-u)*x + 2*(1-u)*u*mx + u*u*tx
			py := (1-u)*(1-u)*y + 2*(1-u)*u*my + u*u*ty
			path = append(path, sample{px, py, t, false})
			t += sampleMs
		}
		x, y = tx, ty
		t += 100 + random.Float64()*600 // a pause on the target
	}
	return path
}

// wandering is idle drifting: smooth, slowly turning moves of 0.4-2.5 s with short stops between them,
// as a hand on a mouse moves when not aiming at anything.
func wandering(random *rand.Rand) []sample { return wander(random, true) }

// nonstopWandering is the same without ever stopping for 24 s: not how people move, but the hardest
// case for the detector, since a long enough smooth curve can come round in a circle.
func nonstopWandering(random *rand.Rand) []sample { return wander(random, false) }

func wander(random *rand.Rand, stops bool) []sample {
	var path []sample
	t, x, y, heading := 0.0, 900.0, 500.0, 0.0
	turnRate := 0.0
	moveEnds := 400 + random.Float64()*2100
	for i := 0; i < 3000; i++ {
		turnRate = 0.9*turnRate + random.NormFloat64()*0.015
		heading += turnRate
		x, y = x+3*math.Cos(heading), y+3*math.Sin(heading)
		x, y = math.Max(0, math.Min(1900, x)), math.Max(0, math.Min(1000, y))
		path = append(path, sample{x, y, t, false})
		t += sampleMs
		if stops && t > moveEnds {
			t += 200 + random.Float64()*800
			moveEnds = t + 400 + random.Float64()*2100
		}
	}
	return path
}

func hoverJiggle(random *rand.Rand) []sample { // small fidgets around one spot
	var path []sample
	t := 0.0
	for i := 0; i < 2000; i++ {
		path = append(path, sample{800 + random.NormFloat64()*15, 500 + random.NormFloat64()*15, t, false})
		t += sampleMs
	}
	return path
}

func countHits(path []sample, loops float64) int {
	detector := New(loops)
	hits := 0
	for _, s := range path {
		if ok, _, _ := detector.Feed(s.x, s.y, s.t, s.held); ok {
			hits++
		}
	}
	return hits
}

func TestHandDrawnCirclesOpenTheLens(t *testing.T) {
	random := rand.New(rand.NewSource(1))
	type band struct {
		name                 string
		loopsPerSecond       [2]float64
		radius               [2]float64
		minimumDetectionRate float64
	}
	bands := []band{
		{"relaxed (1-1.5 turns/s, 50-150 px)", [2]float64{1, 1.5}, [2]float64{50, 150}, 0.97},
		{"brisk (1.5-2.5 turns/s, 40-120 px)", [2]float64{1.5, 2.5}, [2]float64{40, 120}, 0.97},
		{"slow (0.8-1 turns/s, 60-150 px)", [2]float64{0.8, 1}, [2]float64{60, 150}, 0.9},
	}
	for _, needed := range []float64{1, 1.5, 2, 2.5, 3} {
		for _, b := range bands {
			const trials = 400
			hits := 0
			for i := 0; i < trials; i++ {
				// People keep going until it opens; give them a quarter turn past what is needed.
				speed := b.loopsPerSecond[0] + random.Float64()*(b.loopsPerSecond[1]-b.loopsPerSecond[0])
				radius := b.radius[0] + random.Float64()*(b.radius[1]-b.radius[0])
				if countHits(handCircle(random, needed+0.25, speed, radius, random.Intn(2) == 0, 1000), needed) > 0 {
					hits++
				}
			}
			rate := float64(hits) / trials
			t.Logf("%-4s circles needed, %-38s detected %5.1f%%", fmt.Sprint(needed), b.name, rate*100)
			if rate < b.minimumDetectionRate {
				t.Errorf("%v circles, %s: detected %.1f%%, want at least %.0f%%", needed, b.name, rate*100, b.minimumDetectionRate*100)
			}
		}
	}
}

func TestOrdinaryMouseUseNeverOpensTheLens(t *testing.T) {
	random := rand.New(rand.NewSource(2))
	activities := map[string]func(*rand.Rand) []sample{
		"reading sweeps": readingSweeps, "curved reaches": curvedReaches, "wandering": wandering, "hover jiggle": hoverJiggle,
	}
	for _, needed := range []float64{1.5, 2, 2.5, 3} {
		for name, activity := range activities {
			hits := 0
			for i := 0; i < 40; i++ {
				hits += countHits(activity(random), needed)
			}
			if hits > 0 {
				t.Errorf("%v circles needed: %s opened the lens %d times in 40 runs", needed, name, hits)
			}
		}
	}
	// Reported, not failed: one circle is a light gesture by design, and nonstop wandering is the
	// hardest case. These show how often each would open the lens in 40 runs of 24 s.
	for _, needed := range []float64{1.5, 2, 3} {
		hits := 0
		for i := 0; i < 40; i++ {
			hits += countHits(nonstopWandering(random), needed)
		}
		t.Logf("%v circles needed: nonstop wandering opened the lens %d times in 40 runs", needed, hits)
	}
	for name, activity := range activities {
		hits := 0
		for i := 0; i < 40; i++ {
			hits += countHits(activity(random), 1)
		}
		t.Logf("1 circle needed: %s opened the lens %d times in 40 runs", name, hits)
	}
}

func TestOneCircleIsNotEnoughWhenTwoAreNeeded(t *testing.T) {
	random := rand.New(rand.NewSource(3))
	hits := 0
	for i := 0; i < 400; i++ {
		hits += countHits(handCircle(random, 1.1, 1.3, 50+random.Float64()*100, random.Intn(2) == 0, 1000), 2)
	}
	if hits > 0 {
		t.Fatalf("a single hand-drawn circle opened a two-circle lens %d times in 400", hits)
	}
}
