//go:build windows

package main

// Two threads feed the lens while it is on screen:
//
//   - the ticker waits for each display refresh (DwmFlush returns when DWM composes the next frame,
//     at the monitor's rate: 60, 120, 144 Hz...) and asks the UI thread for a frame whenever something
//     moved. It does nothing else, so a slow capture never costs a frame.
//   - the capturer copies the screen around the pointer (our window is left out of captures) whenever
//     the ticker asks, as fast as it can, keeping the newest copy in a triple buffer.
//
// Each frame draws the glass at the pointer's position at that moment, over the newest capture, which
// is taken a little wider than the glass needs so the pointer can run ahead of it by a frame or two.

import (
	"bytes"
	"math"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"circlelens/internal/render"
)

// A capture is taken when the glass would reach past the newest one, and otherwise only to see whether
// the screen changed: every liveEvery while it is changing (video, scrolling), every quietEvery while
// it is not. Moving the pointer alone needs no capture: the glass just samples a different part of the
// one it has. The pointer is read every refresh, so the glass always moves at the display's rate.
const (
	liveEvery  = 33 * time.Millisecond
	quietEvery = 100 * time.Millisecond
	// leadDip is the margin (in DIPs) a capture takes beyond what the glass needs, so the pointer can
	// move a little before a new one is due.
	leadDip = 32
)

type capturer struct {
	window uintptr // the UI window that receives messageFrame
	// Set by the UI thread: the glass radius in DIPs, and the zoom it last drew (smaller zoom, wider capture).
	radiusDip atomic.Uint64
	zoom      atomic.Uint64
	// animating: the UI wants a frame every refresh (opening, closing, a zoom glide), changed or not.
	animating atomic.Bool
	// once: capture a single still and keep drawing it (when Windows cannot leave our window out of
	// captures, a live capture would see the lens itself).
	once bool

	running atomic.Bool
	pending atomic.Bool   // a frame message is queued and not yet drawn
	fresh   atomic.Bool   // a capture arrived since the last frame
	wanted  chan struct{} // the ticker asks the capturer for a capture
	done    sync.WaitGroup

	mutex        sync.Mutex
	back, ready  *render.Capture
	front        *render.Capture
	readyIsFresh bool
	// last is the newest capture, packed, to tell whether the screen changed.
	last []byte
}

func startCapturer(window uintptr, radiusDip, zoom float64, once bool) *capturer {
	c := &capturer{window: window, once: once, wanted: make(chan struct{}, 1),
		back: &render.Capture{}, ready: &render.Capture{}, front: &render.Capture{}}
	c.radiusDip.Store(math.Float64bits(radiusDip))
	c.zoom.Store(math.Float64bits(zoom))
	c.running.Store(true)
	c.done.Add(2)
	go c.tickLoop()
	go c.captureLoop()
	return c
}

// stop ends both threads and waits for them.
func (c *capturer) stop() {
	c.running.Store(false)
	select {
	case c.wanted <- struct{}{}: // wake the capturer so it sees it should stop
	default:
	}
	c.done.Wait()
}

// take returns the newest capture (the same one again if nothing newer has arrived), or nil before the first.
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

// tickLoop runs once per display refresh.
func (c *capturer) tickLoop() {
	defer c.done.Done()
	runtime.LockOSThread()
	var lastCursor point
	first := true
	for c.running.Load() {
		started := time.Now()
		var cursor point
		procGetCursorPos.Call(uintptr(unsafe.Pointer(&cursor)))
		moved := cursor != lastCursor || first
		lastCursor, first = cursor, false

		// Ask for a capture every refresh; the capturer decides whether one is due, and a request
		// made while it is busy simply waits for it.
		select {
		case c.wanted <- struct{}{}:
		default:
		}
		if moved || c.animating.Load() || c.fresh.Load() {
			c.requestFrame()
		}
		waitForComposition(started)
	}
}

func (c *capturer) requestFrame() {
	if c.pending.CompareAndSwap(false, true) {
		procPostMessage.Call(c.window, messageFrame, 0, 0)
	}
}

// waitForComposition waits for the display's next refresh; if DWM returns at once (composition
// paused), it paces by the clock instead.
func waitForComposition(started time.Time) {
	procDwmFlush.Call()
	if elapsed := time.Since(started); elapsed < 2*time.Millisecond {
		time.Sleep(8*time.Millisecond - elapsed)
	}
}

// captureLoop copies the screen around the pointer each time the ticker asks and a capture is due.
func (c *capturer) captureLoop() {
	defer c.done.Done()
	runtime.LockOSThread()
	screen, _, _ := procGetDC.Call(0)
	defer procReleaseDC.Call(0, screen)
	var surface *dib
	defer func() {
		if surface != nil {
			surface.Close()
		}
	}()
	var (
		captured                 bool
		left, top, right, bottom int
		lastCapture              time.Time
		changing                 bool
	)
	for range c.wanted {
		if !c.running.Load() {
			return
		}
		if c.once && captured {
			continue // a still: drawn again each frame, never re-taken
		}
		started := time.Now()
		var cursor point
		procGetCursorPos.Call(uintptr(unsafe.Pointer(&cursor)))
		scale := monitorScale(cursor)
		radius := math.Float64frombits(c.radiusDip.Load()) * scale
		zoom := math.Float64frombits(c.zoom.Load())
		if c.once {
			zoom = 1.0 / 3 // a still covers room to move around in
		}
		need := int(math.Ceil(render.SourceRadius(radius, zoom, scale)))

		// Only the desktop can be captured: past its edge the glass shows its backdrop.
		virtualLeft, virtualTop := int(systemMetric(smXVirtualScreen)), int(systemMetric(smYVirtualScreen))
		virtualRight, virtualBottom := virtualLeft+int(systemMetric(smCxVirtualScreen)), virtualTop+int(systemMetric(smCyVirtualScreen))
		covered := captured &&
			(int(cursor.X)-need >= left || left == virtualLeft) && (int(cursor.X)+need <= right || right == virtualRight) &&
			(int(cursor.Y)-need >= top || top == virtualTop) && (int(cursor.Y)+need <= bottom || bottom == virtualBottom)
		every := quietEvery
		if changing {
			every = liveEvery
		}
		if covered && started.Sub(lastCapture) < every {
			continue
		}

		half := need + int(math.Ceil(leadDip*scale))
		nextLeft, nextTop := max(virtualLeft, int(cursor.X)-half), max(virtualTop, int(cursor.Y)-half)
		nextRight, nextBottom := min(virtualRight, int(cursor.X)+half), min(virtualBottom, int(cursor.Y)+half)
		width, height := nextRight-nextLeft, nextBottom-nextTop
		if width <= 0 || height <= 0 {
			continue
		}
		if surface == nil || surface.Width < width || surface.Height < height {
			if surface != nil {
				surface.Close()
			}
			if surface, _ = newDIB(width, height); surface == nil {
				continue
			}
		}
		procBitBlt.Call(surface.dc, 0, 0, uintptr(width), uintptr(height), screen, uintptr(nextLeft), uintptr(nextTop), srcCopy|captureBlt)
		procGdiFlush.Call()
		moved := !captured || nextLeft != left || nextTop != top || nextRight != right || nextBottom != bottom
		left, top, right, bottom = nextLeft, nextTop, nextRight, nextBottom
		lastCapture, captured = started, true
		changed := c.remember(surface, width, height)
		changing = changed && !moved
		if moved || changed {
			c.publish(surface, left, top, width, height, scale)
		}
	}
}

// remember keeps a copy of the newest capture and says whether it differs from the one before.
func (c *capturer) remember(surface *dib, width, height int) bool {
	need := width * height * 4
	changed := len(c.last) != need
	if cap(c.last) < need {
		c.last = make([]byte, need)
	}
	c.last = c.last[:need]
	for y := 0; y < height; y++ {
		row := surface.Pixels[y*surface.Width*4 : y*surface.Width*4+width*4]
		kept := c.last[y*width*4 : (y+1)*width*4]
		if !changed && !bytes.Equal(row, kept) {
			changed = true
		}
		if changed {
			copy(kept, row)
		}
	}
	return changed
}

// publish copies the captured rows into the back buffer and swaps it in as the newest capture.
func (c *capturer) publish(surface *dib, left, top, width, height int, scale float64) {
	back := c.back
	need := width * height * 4
	if cap(back.Pixels) < need {
		back.Pixels = make([]byte, need)
	}
	back.Pixels = back.Pixels[:need]
	for y := 0; y < height; y++ {
		copy(back.Pixels[y*width*4:(y+1)*width*4], surface.Pixels[y*surface.Width*4:])
	}
	*back = render.Capture{Image: render.Image{Pixels: back.Pixels, Width: width, Height: height}, Left: left, Top: top, Scale: scale}

	c.mutex.Lock()
	c.back, c.ready = c.ready, back
	c.readyIsFresh = true
	c.mutex.Unlock()
	c.fresh.Store(true)
}
