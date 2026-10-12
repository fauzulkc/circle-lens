//go:build linux

package main

import (
	"bufio"
	"bytes"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

func TestDisplayNumber(t *testing.T) {
	for display, want := range map[string][2]string{
		":0":                  {"0", ""},
		":1.0":                {"1", ""},
		"localhost:12":        {"12", ""},
		"/tmp/launch-x/org:0": {"0", "/tmp/launch-x/org"},
	} {
		number, path := displayNumber(display)
		if number != want[0] || path != want[1] {
			t.Errorf("%q: got %q %q, want %q %q", display, number, path, want[0], want[1])
		}
	}
}

func TestKeysymFor(t *testing.T) {
	for name, want := range map[string]uint32{"Z": 'z', "5": '5', "=": '=', "-": '-', "F1": 0xFFBE, "F12": 0xFFC9, "Esc": 0xFF1B, "Num+": 0xFFAB} {
		if got, ok := keysymFor(name); !ok || got != want {
			t.Errorf("%s: got %#x, want %#x", name, got, want)
		}
	}
	if _, ok := keysymFor("Nonsense"); ok {
		t.Error("an unknown key got a keysym")
	}
}

// TestDBusRoundTrip: what the encoder writes, the decoder reads back the same.
func TestDBusRoundTrip(t *testing.T) {
	message := &dbusMessage{kind: 2, serial: 7, replySerial: 3, destination: ":1.5", signature: "u(ia{sv}av)a(iiay)",
		body: []any{uint32(9),
			[]any{int32(0), []any{[]any{"label", dbusVariant{"s", "Quit_now"}}, []any{"enabled", dbusVariant{"b", true}}},
				[]any{dbusVariant{"(ia{sv}av)", []any{int32(1), []any{}, []any{}}}}},
			[]any{[]any{int32(1), int32(1), []any{byte(255), byte(1), byte(2), byte(3)}}}}}
	data, err := encodeMessage(message)
	if err != nil {
		t.Fatal(err)
	}
	read, err := readMessage(bufio.NewReader(bytes.NewReader(data)))
	if err != nil {
		t.Fatal(err)
	}
	if read.replySerial != 3 || read.destination != ":1.5" || read.signature != message.signature {
		t.Fatalf("header came back as %+v", read)
	}
	layout := read.body[1].([]any)
	label := layout[1].([]any)[0].([]any)[1].(dbusVariant).value
	if label != "Quit_now" || read.body[0] != uint32(9) {
		t.Errorf("body came back as %#v", read.body)
	}
	pixels := read.body[2].([]any)[0].([]any)[2].([]any)
	if len(pixels) != 4 || pixels[0] != byte(255) {
		t.Errorf("bytes came back as %v", pixels)
	}
}

/* ---------- on a real X server ---------- */

// The tests below need a virtual X server and change what it shows, so they run only when asked:
//
//	CIRCLE_LENS_X11_TEST=1 xvfb-run -a -s "-screen 0 1280x800x24" go test -run X11 ./cmd/circle-lens
//
// They move the virtual display's pointer with XTEST. Never point DISPLAY at a desktop someone is using.

func needVirtualDisplay(t *testing.T) *xConn {
	if os.Getenv("CIRCLE_LENS_X11_TEST") != "1" {
		t.Skip("set CIRCLE_LENS_X11_TEST=1 and run under xvfb-run")
	}
	x, err := connectX()
	if err != nil {
		t.Fatal(err)
	}
	return x
}

func TestX11LensStill(t *testing.T) { lensTest(t, false) }

func TestX11LensLive(t *testing.T) { lensTest(t, true) }

// lensTest shows a checkerboard window, draws two circles with the pointer, and checks that the lens
// opened and magnified the checkerboard (not black, not itself). With compositing, a stand-in
// compositing manager redirects every window offscreen, as GNOME or KDE would.
func lensTest(t *testing.T, compositing bool) {
	x := needVirtualDisplay(t)
	defer x.close()
	if compositing {
		composite := x.extension("Composite")
		if !composite.present || x.argbVisual == 0 {
			t.Skip("no Composite extension or ARGB visual")
		}
		x.call(composite.opcode, 0, u32(0, 4))
		x.send(composite.opcode, 2, join(u32(x.root), []byte{1, 0, 0, 0}), false) // RedirectSubwindows, manual
		owner := x.createWindow(0, 0, 1, 1, false, 0)
		x.send(22, 0, u32(owner, x.internAtom("_NET_WM_CM_S0"), 0), false) // SetSelectionOwner
		defer x.send(composite.opcode, 4, join(u32(x.root), []byte{1, 0, 0, 0}), false)
	}
	board := checkerboard(x, 340, 200, 600, 400)
	defer x.destroyWindow(board)
	x.sync()

	program := filepath.Join(t.TempDir(), "circle-lens-test")
	build := exec.Command("go", "build", "-ldflags", "-X main.instance=CircleLensX11Test", "-o", program, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	frames := t.TempDir()
	run := exec.Command(program)
	run.Env = append(os.Environ(), "CIRCLE_LENS_FRAMES="+frames, "XDG_RUNTIME_DIR="+t.TempDir(), "XDG_CONFIG_HOME="+t.TempDir())
	if err := run.Start(); err != nil {
		t.Fatal(err)
	}
	defer run.Process.Kill()
	time.Sleep(time.Second)
	drawCircles(t, x, 640, 400, 120)
	time.Sleep(time.Second)
	run.Process.Signal(os.Interrupt)
	run.Process.Kill()
	run.Wait()

	names, _ := filepath.Glob(filepath.Join(frames, "*.png"))
	if len(names) == 0 {
		t.Fatal("the lens never opened")
	}
	sort.Strings(names)
	file, err := os.Open(names[len(names)-1])
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	picture, err := png.Decode(file)
	if err != nil {
		t.Fatal(err)
	}
	bounds := picture.Bounds()
	red, light := 0, 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y += 4 {
		for x := bounds.Min.X; x < bounds.Max.X; x += 4 {
			r, g, b, a := picture.At(x, y).RGBA()
			switch {
			case a < 0xF000:
			case r > 0xA000 && g < 0x6000 && b < 0x6000:
				red++
			case r > 0xE000 && g > 0xE000 && b > 0xE000:
				light++
			}
		}
	}
	if red < 200 || light < 200 {
		t.Errorf("the lens didn't show the checkerboard (%d red, %d light samples) in %s", red, light, names[len(names)-1])
	}
}

// checkerboard shows a window of red and light squares.
func checkerboard(x *xConn, left, top, width, height int) uint32 {
	id := x.newID()
	x.send(1, x.rootDepth, join(u32(id, x.root), u16(left, top, width, height, 0, 1), u32(x.rootVisual, cwBackPixel|cwOverrideRedirect), u32(0xffffff, 1)), false)
	x.mapWindow(id)
	pixels := make([]byte, width*height*4)
	for y := 0; y < height; y++ {
		for i := 0; i < width; i++ {
			p := (y*width + i) * 4
			if (i/20+y/20)%2 == 0 {
				pixels[p], pixels[p+1], pixels[p+2] = 0x30, 0x30, 0xc0
			} else {
				pixels[p], pixels[p+1], pixels[p+2] = 0xf0, 0xf0, 0xf0
			}
		}
	}
	x.putImage(id, x.createGC(id), x.rootDepth, pixels, width, height, 0, 0)
	return id
}

// drawCircles moves the virtual pointer twice round a circle (XTEST).
func drawCircles(t *testing.T, x *xConn, centreX, centreY int, radius float64) {
	xtest := x.extension("XTEST")
	if !xtest.present {
		t.Skip("no XTEST")
	}
	for i := 0; i <= 144; i++ {
		angle := float64(i) / 72 * 2 * math.Pi
		body := make([]byte, 32)
		body[0] = 6 // MotionNotify, absolute
		order.PutUint32(body[8:], x.root)
		order.PutUint16(body[20:], uint16(centreX+int(radius*math.Cos(angle))))
		order.PutUint16(body[22:], uint16(centreY+int(radius*math.Sin(angle))))
		x.send(xtest.opcode, 2, body, false)
		x.sync()
		time.Sleep(8 * time.Millisecond)
	}
}
