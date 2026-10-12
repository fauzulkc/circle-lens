//go:build linux

package main

// A small X11 client, written against the wire protocol so the app stays standard-library Go with no
// cgo: it speaks to the X server over its Unix socket. It has only what the lens needs: windows,
// properties, pointer and keyboard grabs, GetImage/PutImage, and the SHAPE, XInputExtension (2.2),
// Composite, RANDR and XTEST extensions.
//
// One goroutine reads everything the server sends. Replies (and errors) go to whoever sent the request
// they answer; events go to the events channel. Requests may come from any goroutine.

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

var order = binary.LittleEndian

type xConn struct {
	conn   net.Conn
	writer sync.Mutex // one request at a time on the wire, numbered in the order sent
	seq    uint16

	waiting sync.Mutex
	replies map[uint16]chan xReply
	events  chan []byte
	closed  chan struct{}

	idBase, idMask, nextID uint32
	maxRequestBytes        int
	minKeycode, maxKeycode byte

	root           uint32
	rootVisual     uint32
	rootDepth      byte
	width, height  int
	argbVisual     uint32 // a 32-bit TrueColor visual, for a window with per-pixel alpha (0 if none)
	extensions     map[string]xExtension
	extensionsLock sync.Mutex
}

type xReply struct {
	data []byte
	err  error
}

type xExtension struct {
	present            bool
	opcode, firstEvent byte
	firstError         byte
}

// xError is an error the server sent for a request.
type xError struct{ code, major, minor byte }

func (e xError) Error() string {
	return fmt.Sprintf("X error %d (request %d.%d)", e.code, e.major, e.minor)
}

// connectX opens the display in DISPLAY (":0", ":1.0", or a socket path).
func connectX() (*xConn, error) {
	display := os.Getenv("DISPLAY")
	if display == "" {
		return nil, errors.New("no DISPLAY: not an X11 session")
	}
	number, path := displayNumber(display)
	var conn net.Conn
	var err error
	if path != "" {
		conn, err = net.Dial("unix", path)
	} else {
		conn, err = net.Dial("unix", "/tmp/.X11-unix/X"+number)
		if err != nil {
			conn, err = net.Dial("unix", "@/tmp/.X11-unix/X"+number)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("can't reach the X server at %s: %w", display, err)
	}
	c := &xConn{conn: conn, replies: map[uint16]chan xReply{}, events: make(chan []byte, 256), closed: make(chan struct{}), extensions: map[string]xExtension{}}
	name, data := xAuthority(number)
	if err := c.setup(name, data); err != nil {
		conn.Close()
		return nil, err
	}
	go c.read()
	return c, nil
}

// displayNumber picks the display number out of DISPLAY, or a socket path for the "/path:0" form.
func displayNumber(display string) (number, path string) {
	if strings.HasPrefix(display, "/") {
		if i := strings.LastIndex(display, ":"); i > 0 {
			return display[i+1:], display[:i]
		}
		return "0", display
	}
	after := display[strings.LastIndex(display, ":")+1:]
	number, _, _ = strings.Cut(after, ".")
	return number, ""
}

// xAuthority finds the MIT-MAGIC-COOKIE-1 for this display in the Xauthority file, if there is one.
func xAuthority(number string) (name string, data []byte) {
	path := os.Getenv("XAUTHORITY")
	if path == "" {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, ".Xauthority")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", nil
	}
	defer file.Close()
	host, _ := os.Hostname()
	reader := bufio.NewReader(file)
	field := func() ([]byte, error) {
		var length uint16
		if err := binary.Read(reader, binary.BigEndian, &length); err != nil {
			return nil, err
		}
		value := make([]byte, length)
		_, err := io.ReadFull(reader, value)
		return value, err
	}
	for {
		var family uint16
		if binary.Read(reader, binary.BigEndian, &family) != nil {
			return "", nil
		}
		address, err1 := field()
		display, err2 := field()
		authName, err3 := field()
		authData, err4 := field()
		if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
			return "", nil
		}
		local := family == 256 && string(address) == host // FamilyLocal
		wild := family == 0xFFFF
		if (local || wild) && (len(display) == 0 || string(display) == number) && string(authName) == "MIT-MAGIC-COOKIE-1" {
			return string(authName), authData
		}
	}
}

func pad4(n int) int { return (n + 3) &^ 3 }

func (c *xConn) setup(authName string, authData []byte) error {
	request := make([]byte, 12+pad4(len(authName))+pad4(len(authData)))
	request[0] = 'l'
	order.PutUint16(request[2:], 11)
	order.PutUint16(request[6:], uint16(len(authName)))
	order.PutUint16(request[8:], uint16(len(authData)))
	copy(request[12:], authName)
	copy(request[12+pad4(len(authName)):], authData)
	if _, err := c.conn.Write(request); err != nil {
		return err
	}
	head := make([]byte, 8)
	if _, err := io.ReadFull(c.conn, head); err != nil {
		return err
	}
	body := make([]byte, int(order.Uint16(head[6:]))*4)
	if _, err := io.ReadFull(c.conn, body); err != nil {
		return err
	}
	if head[0] != 1 {
		reason := string(body)
		if head[0] == 0 && int(head[1]) <= len(body) {
			reason = string(body[:head[1]])
		}
		return fmt.Errorf("the X server refused the connection: %s", strings.TrimSpace(reason))
	}
	c.idBase, c.idMask = order.Uint32(body[4:]), order.Uint32(body[8:])
	vendorLength := int(order.Uint16(body[16:]))
	c.maxRequestBytes = int(order.Uint16(body[18:])) * 4
	screens, formats := int(body[20]), int(body[21])
	c.minKeycode, c.maxKeycode = body[26], body[27]
	at := 32 + pad4(vendorLength) + 8*formats
	if screens < 1 || at+40 > len(body) {
		return errors.New("the X server sent no screen")
	}
	screen := body[at:]
	c.root = order.Uint32(screen[0:])
	c.width, c.height = int(order.Uint16(screen[20:])), int(order.Uint16(screen[22:]))
	c.rootVisual = order.Uint32(screen[32:])
	c.rootDepth = screen[38]
	depths := int(screen[39])
	at = 40
	for i := 0; i < depths && at+8 <= len(screen); i++ {
		depth, visuals := screen[at], int(order.Uint16(screen[at+2:]))
		at += 8
		for v := 0; v < visuals && at+24 <= len(screen); v++ {
			visual := screen[at:]
			if depth == 32 && visual[4] == 4 && c.argbVisual == 0 { // TrueColor
				c.argbVisual = order.Uint32(visual)
			}
			at += 24
		}
	}
	return nil
}

func (c *xConn) newID() uint32 {
	c.writer.Lock()
	defer c.writer.Unlock()
	id := c.idBase | (c.nextID & c.idMask)
	c.nextID++
	return id
}

// read runs on its own goroutine for the life of the connection.
func (c *xConn) read() {
	defer close(c.closed)
	reader := bufio.NewReaderSize(c.conn, 64<<10)
	head := make([]byte, 32)
	for {
		if _, err := io.ReadFull(reader, head); err != nil {
			c.failAll(err)
			return
		}
		switch head[0] {
		case 0: // an error
			seq := order.Uint16(head[2:])
			err := xError{code: head[1], major: head[10], minor: byte(order.Uint16(head[8:]))}
			if waiting := c.take(seq); waiting != nil {
				waiting <- xReply{err: err}
			} else {
				debugLog("%v", err)
			}
		case 1: // a reply
			seq := order.Uint16(head[2:])
			data := make([]byte, 32+int(order.Uint32(head[4:]))*4)
			copy(data, head)
			if _, err := io.ReadFull(reader, data[32:]); err != nil {
				c.failAll(err)
				return
			}
			if waiting := c.take(seq); waiting != nil {
				waiting <- xReply{data: data}
			}
		default: // an event; GenericEvent (35) carries more after the first 32 bytes
			length := 32
			if head[0]&0x7F == 35 {
				length += int(order.Uint32(head[4:])) * 4
			}
			event := make([]byte, length)
			copy(event, head)
			if _, err := io.ReadFull(reader, event[32:]); err != nil {
				c.failAll(err)
				return
			}
			select {
			case c.events <- event:
			default: // nobody is keeping up: drop it rather than stall replies
			}
		}
	}
}

func (c *xConn) take(seq uint16) chan xReply {
	c.waiting.Lock()
	defer c.waiting.Unlock()
	waiting := c.replies[seq]
	delete(c.replies, seq)
	return waiting
}

func (c *xConn) failAll(err error) {
	c.waiting.Lock()
	defer c.waiting.Unlock()
	for seq, waiting := range c.replies {
		waiting <- xReply{err: err}
		delete(c.replies, seq)
	}
}

// xCookie is a reply still to come.
type xCookie chan xReply

// wait returns the reply, or the error the server sent for the request.
func (cookie xCookie) wait() ([]byte, error) {
	if cookie == nil {
		return nil, errors.New("the request was not sent")
	}
	reply := <-cookie
	return reply.data, reply.err
}

// send writes one request: opcode, its data byte, and the body after the 4-byte header (padded here).
// With wantReply, the cookie gets the reply; otherwise errors are only logged.
func (c *xConn) send(opcode, data byte, body []byte, wantReply bool) xCookie {
	length := 4 + pad4(len(body))
	request := make([]byte, length)
	request[0], request[1] = opcode, data
	order.PutUint16(request[2:], uint16(length/4))
	copy(request[4:], body)
	c.writer.Lock()
	defer c.writer.Unlock()
	c.seq++
	var cookie xCookie
	if wantReply {
		cookie = make(xCookie, 1)
		c.waiting.Lock()
		c.replies[c.seq] = cookie
		c.waiting.Unlock()
	}
	if _, err := c.conn.Write(request); err != nil {
		if cookie != nil {
			c.take(c.seq)
			cookie <- xReply{err: err}
		}
	}
	return cookie
}

// call sends a request and waits for its reply.
func (c *xConn) call(opcode, data byte, body []byte) ([]byte, error) {
	return c.send(opcode, data, body, true).wait()
}

// sync waits until the server has handled everything sent so far (GetInputFocus as a round trip).
func (c *xConn) sync() { c.call(43, 0, nil) }

func (c *xConn) close() { c.conn.Close() }

/* ---------- requests ---------- */

func u16(values ...int) []byte {
	out := make([]byte, 2*len(values))
	for i, v := range values {
		order.PutUint16(out[2*i:], uint16(v))
	}
	return out
}

func u32(values ...uint32) []byte {
	out := make([]byte, 4*len(values))
	for i, v := range values {
		order.PutUint32(out[4*i:], v)
	}
	return out
}

func join(parts ...[]byte) []byte {
	var out []byte
	for _, part := range parts {
		out = append(out, part...)
	}
	return out
}

func (c *xConn) internAtom(name string) uint32 {
	reply, err := c.call(16, 0, join(u16(len(name), 0), []byte(name)))
	if err != nil {
		return 0
	}
	return order.Uint32(reply[8:])
}

// extension asks for an extension once and remembers the answer.
func (c *xConn) extension(name string) xExtension {
	c.extensionsLock.Lock()
	defer c.extensionsLock.Unlock()
	if known, ok := c.extensions[name]; ok {
		return known
	}
	var found xExtension
	if reply, err := c.call(98, 0, join(u16(len(name), 0), []byte(name))); err == nil {
		found = xExtension{present: reply[8] != 0, opcode: reply[9], firstEvent: reply[10], firstError: reply[11]}
	}
	c.extensions[name] = found
	return found
}

// Window attributes for createWindow, in the order of their value-mask bits.
const (
	cwBackPixel        = 0x2
	cwBorderPixel      = 0x8
	cwOverrideRedirect = 0x200
	cwEventMask        = 0x800
	cwColormap         = 0x2000
)

// createWindow makes an override-redirect window (the window manager leaves it alone) at depth 32
// with the ARGB visual when argb, or the root's depth and visual otherwise.
func (c *xConn) createWindow(x, y, width, height int, argb bool, eventMask uint32) uint32 {
	id := c.newID()
	depth, visual := c.rootDepth, c.rootVisual
	mask := uint32(cwBackPixel | cwBorderPixel | cwOverrideRedirect | cwEventMask)
	values := []uint32{0, 0, 1, eventMask}
	if argb {
		depth, visual = 32, c.argbVisual
		colormap := c.newID()
		c.send(78, 0, u32(colormap, c.root, visual), false) // CreateColormap, AllocNone
		mask |= cwColormap
		values = append(values, colormap)
	}
	body := join(u32(id, c.root), u16(x, y, width, height, 0, 1), u32(visual, mask), u32(values...))
	c.send(1, depth, body, false)
	return id
}

func (c *xConn) mapWindow(window uint32)     { c.send(8, 0, u32(window), false) }
func (c *xConn) unmapWindow(window uint32)   { c.send(10, 0, u32(window), false) }
func (c *xConn) destroyWindow(window uint32) { c.send(4, 0, u32(window), false) }

// configureWindow moves and sizes a window and raises it above its siblings.
func (c *xConn) configureWindow(window uint32, x, y, width, height int) {
	const xyWidthHeightStack = 0x1 | 0x2 | 0x4 | 0x8 | 0x40
	c.send(12, 0, join(u32(window), u16(xyWidthHeightStack, 0), u32(uint32(int32(x)), uint32(int32(y)), uint32(width), uint32(height), 0)), false)
}

func (c *xConn) createGC(drawable uint32) uint32 {
	id := c.newID()
	c.send(55, 0, u32(id, drawable, 0), false)
	return id
}

// putImage draws BGRA pixels (rows packed) into a drawable, in bands that fit the request limit.
func (c *xConn) putImage(drawable, gc uint32, depth byte, pixels []byte, width, height, x, y int) {
	if width <= 0 || height <= 0 {
		return
	}
	rows := max(1, (c.maxRequestBytes-28)/(width*4))
	for top := 0; top < height; top += rows {
		band := min(rows, height-top)
		header := join(u32(drawable, gc), u16(width, band, x, y+top), []byte{0, depth, 0, 0})
		c.send(72, 2, join(header, pixels[top*width*4:(top+band)*width*4]), false) // ZPixmap
	}
}

// getImage starts reading a rectangle of a drawable as ZPixmap; the reply's pixels start at byte 32.
func (c *xConn) getImage(drawable uint32, x, y, width, height int) xCookie {
	return c.send(73, 2, join(u32(drawable), u16(x, y, width, height), u32(0xFFFFFFFF)), true)
}

// queryPointer gives the pointer's position on the root window and the buttons and modifiers held.
func (c *xConn) queryPointer() (x, y int, mask uint16, ok bool) {
	reply, err := c.call(38, 0, u32(c.root))
	if err != nil {
		return 0, 0, 0, false
	}
	return int(int16(order.Uint16(reply[16:]))), int(int16(order.Uint16(reply[18:]))), order.Uint16(reply[24:]), true
}

// queryTree lists a window's children, bottom of the stack first.
func (c *xConn) queryTree(window uint32) []uint32 {
	reply, err := c.call(15, 0, u32(window))
	if err != nil {
		return nil
	}
	count := int(order.Uint16(reply[16:]))
	children := make([]uint32, 0, count)
	for i := 0; i < count && 32+4*i+4 <= len(reply); i++ {
		children = append(children, order.Uint32(reply[32+4*i:]))
	}
	return children
}

func (c *xConn) getProperty(window, property, kind uint32, longs int) (format byte, value []byte, ok bool) {
	reply, err := c.call(20, 0, u32(window, property, kind, 0, uint32(longs)))
	if err != nil || order.Uint32(reply[8:]) == 0 {
		return 0, nil, false
	}
	format = reply[1]
	count := int(order.Uint32(reply[16:])) * int(format) / 8
	if 32+count > len(reply) {
		return 0, nil, false
	}
	return format, reply[32 : 32+count], true
}

func (c *xConn) changeProperty(window, property, kind uint32, format byte, data []byte) {
	units := len(data) / (int(format) / 8)
	c.send(18, 0, join(u32(window, property, kind), []byte{format, 0, 0, 0}, u32(uint32(units)), data), false) // Replace
}

func (c *xConn) selectionOwner(selection uint32) uint32 {
	reply, err := c.call(23, 0, u32(selection))
	if err != nil {
		return 0
	}
	return order.Uint32(reply[8:])
}

// Modifier masks, as X numbers them.
const (
	shiftMask   = 0x1
	lockMask    = 0x2
	controlMask = 0x4
	mod1Mask    = 0x8  // Alt
	mod2Mask    = 0x10 // Num Lock
	mod4Mask    = 0x40 // Super
	anyModifier = 0x8000
)

// lockVariants are the Caps Lock and Num Lock combinations a grab must also cover, or it stops working
// whenever one of them is on.
var lockVariants = []uint16{0, lockMask, mod2Mask, lockMask | mod2Mask}

func (c *xConn) grabKey(keycode byte, modifiers uint16) {
	for _, extra := range lockVariants {
		c.send(33, 1, join(u32(c.root), u16(int(modifiers|extra)), []byte{keycode, 1, 1, 0, 0, 0}), false) // async, async
	}
}

func (c *xConn) ungrabKey(keycode byte, modifiers uint16) {
	for _, extra := range lockVariants {
		c.send(34, keycode, join(u32(c.root), u16(int(modifiers|extra), 0)), false)
	}
}

// grabButton takes a button (with modifiers) from every app: the wheel, while the lens is open.
func (c *xConn) grabButton(button byte, modifiers uint16) {
	const buttonPressMask = 0x4
	for _, extra := range lockVariants {
		c.send(28, 0, join(u32(c.root), u16(buttonPressMask), []byte{1, 1}, u32(0, 0), []byte{button, 0}, u16(int(modifiers|extra))), false)
	}
}

func (c *xConn) ungrabButton(button byte, modifiers uint16) {
	for _, extra := range lockVariants {
		c.send(29, button, join(u32(c.root), u16(int(modifiers|extra), 0)), false)
	}
}

// keyboardMapping returns the keysyms of every keycode, keysymsPerKeycode to a keycode.
func (c *xConn) keyboardMapping() (keysyms []uint32, perKeycode int) {
	count := int(c.maxKeycode) - int(c.minKeycode) + 1
	reply, err := c.call(101, 0, []byte{c.minKeycode, byte(count), 0, 0})
	if err != nil {
		return nil, 0
	}
	perKeycode = int(reply[1])
	for at := 32; at+4 <= len(reply); at += 4 {
		keysyms = append(keysyms, order.Uint32(reply[at:]))
	}
	return keysyms, perKeycode
}

// keycodeFor finds the first keycode that types a keysym.
func (c *xConn) keycodeFor(keysyms []uint32, perKeycode int, keysym uint32) byte {
	for i, sym := range keysyms {
		if sym == keysym && perKeycode > 0 {
			return c.minKeycode + byte(i/perKeycode)
		}
	}
	return 0
}

/* ---------- extensions ---------- */

// setInputShape makes a window click-through (an empty input region; SHAPE 1.1).
func (c *xConn) setInputShape(window uint32) bool {
	shape := c.extension("SHAPE")
	if !shape.present {
		return false
	}
	c.send(shape.opcode, 1, join([]byte{0, 2, 0, 0}, u32(window), u16(0, 0)), false) // Rectangles: Set, Input, none
	return true
}

// setBoundingShape cuts a window to a set of rectangles (x, y, width, height each), or to nothing.
func (c *xConn) setBoundingShape(window uint32, rectangles []int) {
	shape := c.extension("SHAPE")
	if !shape.present {
		return
	}
	c.send(shape.opcode, 1, join([]byte{0, 0, 0, 0}, u32(window), u16(0, 0), u16(rectangles...)), false) // Set, Bounding
}

// XInput2 raw events: they report every device's motion, buttons and keys wherever the pointer is.
const (
	xiRawKeyPress      = 13
	xiRawKeyRelease    = 14
	xiRawButtonPress   = 15
	xiRawButtonRelease = 16
	xiRawMotion        = 17
	xiAllMasterDevices = 1
)

// selectRawEvents asks for the given XInput2 raw events on the root window (0 events stops them).
func (c *xConn) selectRawEvents(events ...int) bool {
	xi := c.extension("XInputExtension")
	if !xi.present {
		return false
	}
	if reply, err := c.call(xi.opcode, 47, u16(2, 2)); err != nil || order.Uint16(reply[8:]) < 2 { // XIQueryVersion
		return false
	}
	var mask uint32
	for _, event := range events {
		mask |= 1 << event
	}
	c.send(xi.opcode, 46, join(u32(c.root), u16(1, 0), u16(xiAllMasterDevices, 1), u32(mask)), false) // XISelectEvents
	return true
}

// compositing tells whether a compositing manager runs (it owns _NET_WM_CM_S0): then windows keep
// their contents offscreen, and a window with per-pixel alpha is blended.
func (c *xConn) compositing() bool {
	return c.selectionOwner(c.internAtom("_NET_WM_CM_S0")) != 0
}

// refreshRate is the fastest refresh rate of the active outputs (RANDR), or 60.
func (c *xConn) refreshRate() float64 {
	randr := c.extension("RANDR")
	if !randr.present {
		return 60
	}
	if reply, err := c.call(randr.opcode, 0, u32(1, 5)); err != nil || (order.Uint32(reply[8:]) == 1 && order.Uint32(reply[12:]) < 3) {
		return 60
	}
	reply, err := c.call(randr.opcode, 25, u32(c.root)) // GetScreenResourcesCurrent
	if err != nil {
		return 60
	}
	crtcs, outputs, modes := int(order.Uint16(reply[16:])), int(order.Uint16(reply[18:])), int(order.Uint16(reply[20:]))
	rates := map[uint32]float64{}
	at := 32 + 4*crtcs + 4*outputs
	for i := 0; i < modes && at+32 <= len(reply); i, at = i+1, at+32 {
		mode := reply[at:]
		clock, horizontal, vertical := float64(order.Uint32(mode[8:])), float64(order.Uint16(mode[16:])), float64(order.Uint16(mode[24:]))
		if horizontal > 0 && vertical > 0 {
			rates[order.Uint32(mode)] = clock / (horizontal * vertical)
		}
	}
	best := 0.0
	for i := 0; i < crtcs; i++ {
		crtc := order.Uint32(reply[32+4*i:])
		info, err := c.call(randr.opcode, 20, u32(crtc, 0)) // GetCrtcInfo
		if err != nil || len(info) < 24 {
			continue
		}
		if rate := rates[order.Uint32(info[20:])]; rate > best {
			best = rate
		}
	}
	if best < 20 || best > 500 {
		return 60
	}
	return best
}

// dpiScale reads Xft.dpi from the root window's resources (what desktops set for display scaling).
func (c *xConn) dpiScale() float64 {
	_, value, ok := c.getProperty(c.root, c.internAtom("RESOURCE_MANAGER"), 31, 1<<16) // STRING
	if !ok {
		return 1
	}
	for _, line := range strings.Split(string(value), "\n") {
		if name, setting, found := strings.Cut(line, ":"); found && strings.TrimSpace(name) == "Xft.dpi" {
			if dpi, err := strconv.ParseFloat(strings.TrimSpace(setting), 64); err == nil && dpi >= 48 && dpi <= 480 {
				return dpi / 96
			}
		}
	}
	return 1
}
