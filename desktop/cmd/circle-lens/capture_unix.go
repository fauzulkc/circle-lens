//go:build linux || darwin

package main

// The two threads that feed the lens while it is on screen, as on Windows (capture_windows.go):
//
//   - the ticker wakes once per display refresh and asks for a frame whenever something moved. It does
//     nothing else, so a slow capture never costs a frame.
//   - the capturer copies the screen around the pointer whenever the ticker asks and a capture is due,
//     keeping the newest copy in a triple buffer.
//
// When the system can't leave the lens out of a capture, it takes one still of the whole screen as
// the lens opens and draws that until it closes.

import (
	"bytes"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"circlelens/internal/render"
)

const (
	liveEvery  = 33 * time.Millisecond
	quietEvery = 100 * time.Millisecond
	leadDip    = 32
)

type capturer struct {
	sys          system
	requestFrame func()
	radiusDip    atomic.Uint64
	zoom         atomic.Uint64
	animating    atomic.Bool
	once         bool

	running atomic.Bool
	fresh   atomic.Bool
	wanted  chan struct{}
	done    sync.WaitGroup

	mutex        sync.Mutex
	back, ready  *render.Capture
	front        *render.Capture
	readyIsFresh bool
	last         []byte
}

func startCapturer(sys system, requestFrame func(), radiusDip, zoom float64, once bool) *capturer {
	c := &capturer{sys: sys, requestFrame: requestFrame, once: once, wanted: make(chan struct{}, 1),
		back: &render.Capture{}, ready: &render.Capture{}, front: &render.Capture{}}
	c.radiusDip.Store(math.Float64bits(radiusDip))
	c.zoom.Store(math.Float64bits(zoom))
	c.running.Store(true)
	c.done.Add(2)
	go c.tickLoop()
	go c.captureLoop()
	return c
}

func (c *capturer) stop() {
	c.running.Store(false)
	select {
	case c.wanted <- struct{}{}:
	default:
	}
	c.done.Wait()
}

func (c *capturer) take() *render.Capture {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.readyIsFresh {
		c.front, c.ready = c.ready, c.front
		c.readyIsFresh = false
	}
	if c.front.Width == 0 {
		return nil
	}
	return c.front
}

func (c *capturer) tickLoop() {
	defer c.done.Done()
	interval := time.Duration(float64(time.Second) / c.sys.refreshRate())
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	lastX, lastY, first := 0, 0, true
	for c.running.Load() {
		x, y, _ := c.sys.pointer()
		moved := first || x != lastX || y != lastY
		lastX, lastY, first = x, y, false
		select {
		case c.wanted <- struct{}{}:
		default:
		}
		if moved || c.animating.Load() || c.fresh.Load() {
			c.requestFrame()
		}
		<-ticker.C
	}
}

func (c *capturer) captureLoop() {
	defer c.done.Done()
	var (
		captured                 bool
		left, top, right, bottom int
		lastCapture              time.Time
		changing                 bool
		pixels                   []byte
	)
	for range c.wanted {
		if !c.running.Load() {
			return
		}
		if c.once && captured {
			continue
		}
		started := time.Now()
		x, y, ok := c.sys.pointer()
		if !ok {
			continue
		}
		scale := c.sys.scale()
		radius := math.Float64frombits(c.radiusDip.Load()) * scale
		zoom := math.Float64frombits(c.zoom.Load())
		need := int(math.Ceil(render.SourceRadius(radius, zoom, scale)))
		screenLeft, screenTop, screenRight, screenBottom := c.sys.bounds()
		covered := captured &&
			(x-need >= left || left == screenLeft) && (x+need <= right || right == screenRight) &&
			(y-need >= top || top == screenTop) && (y+need <= bottom || bottom == screenBottom)
		every := quietEvery
		if changing {
			every = liveEvery
		}
		if covered && started.Sub(lastCapture) < every {
			continue
		}
		half := need + int(math.Ceil(leadDip*scale))
		nextLeft, nextTop := max(screenLeft, x-half), max(screenTop, y-half)
		nextRight, nextBottom := min(screenRight, x+half), min(screenBottom, y+half)
		if c.once {
			// A still is all the glass will ever show, so it takes the whole screen.
			nextLeft, nextTop, nextRight, nextBottom = screenLeft, screenTop, screenRight, screenBottom
		}
		width, height := nextRight-nextLeft, nextBottom-nextTop
		if width <= 0 || height <= 0 {
			continue
		}
		if cap(pixels) < width*height*4 {
			pixels = make([]byte, width*height*4)
		}
		pixels = pixels[:width*height*4]
		if !c.sys.grab(nextLeft, nextTop, width, height, pixels) {
			continue
		}
		moved := !captured || nextLeft != left || nextTop != top || nextRight != right || nextBottom != bottom
		left, top, right, bottom = nextLeft, nextTop, nextRight, nextBottom
		lastCapture, captured = started, true
		if c.once {
			c.publish(pixels, left, top, width, height, scale) // never compared, so never kept twice
			continue
		}
		changed := c.remember(pixels)
		changing = changed && !moved
		if moved || changed {
			c.publish(pixels, left, top, width, height, scale)
		}
	}
}

func (c *capturer) remember(pixels []byte) bool {
	if bytes.Equal(pixels, c.last) {
		return false
	}
	c.last = append(c.last[:0], pixels...)
	return true
}

func (c *capturer) publish(pixels []byte, left, top, width, height int, scale float64) {
	back := c.back
	back.Pixels = append(back.Pixels[:0], pixels...)
	*back = render.Capture{Image: render.Image{Pixels: back.Pixels, Width: width, Height: height}, Left: left, Top: top, Scale: scale}
	c.mutex.Lock()
	c.back, c.ready = c.ready, back
	c.readyIsFresh = true
	c.mutex.Unlock()
	c.fresh.Store(true)
}
