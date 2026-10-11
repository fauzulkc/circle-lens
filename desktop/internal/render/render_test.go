package render

import (
	"testing"

	"circlelens/internal/lens"
)

// gradientCapture is a capture whose pixel colour encodes its position, so a test can tell which
// screen point landed where.
func gradientCapture(radius, scale float64) *Capture {
	side := int(2*SourceRadius(radius, 1, scale)) + 2
	capture := &Capture{Image: *NewImage(side, side), Left: 1000, Top: 500, Scale: scale}
	capture.PointerX, capture.PointerY = float64(capture.Left)+float64(side)/2, float64(capture.Top)+float64(side)/2
	for y := 0; y < side; y++ {
		for x := 0; x < side; x++ {
			i := (y*side + x) * 4
			capture.Pixels[i], capture.Pixels[i+1], capture.Pixels[i+2], capture.Pixels[i+3] = byte(x), byte(y), 128, 255
		}
	}
	return capture
}

func pixel(image *Image, x, y int) []byte { return image.Pixels[(y*image.Width+x)*4:][:4] }

func TestSteadyGlassShowsTheScreenUnderThePointerAtTheCentre(t *testing.T) {
	const radius, scale = 150, 1.0
	capture := gradientCapture(radius, scale)
	canvas := NewImage(Side(radius, scale), Side(radius, scale))
	Compose(canvas, capture, Scene{Frame: lens.Frame{GlassScale: 1, Alpha: 1, Zoom: 2}, Radius: radius, Theme: Light})

	centre := Pad(scale) + radius
	got := pixel(canvas, centre, centre)
	wantX, wantY := byte(capture.PointerX-float64(capture.Left)), byte(capture.PointerY-float64(capture.Top))
	if got[3] != 255 || absDiff(got[0], wantX) > 1 || absDiff(got[1], wantY) > 1 {
		t.Fatalf("centre pixel %v, want about [%d %d 128 255]", got, wantX, wantY)
	}
	// 40px right of centre at 2× is 20 screen pixels right of the pointer.
	if got := pixel(canvas, centre+40, centre); absDiff(got[0], wantX+20) > 1 {
		t.Fatalf("40px right shows x=%d, want %d", got[0], wantX+20)
	}
	if got := pixel(canvas, 0, 0); got[3] != 0 {
		t.Fatalf("corner should be clear, got %v", got)
	}
	if got := pixel(canvas, centre+radius-1, centre); got[3] != 255 || got[2] != Light.Border[0] {
		t.Fatalf("edge should be the opaque border, got %v", got)
	}
}

func TestInvisibleFrameDrawsNothing(t *testing.T) {
	const radius, scale = 150, 1.0
	canvas := NewImage(Side(radius, scale), Side(radius, scale))
	Compose(canvas, gradientCapture(radius, scale), Scene{Frame: lens.Frame{GlassScale: 0.62, Zoom: 1}, Radius: radius, Theme: Dark})
	for i := 3; i < len(canvas.Pixels); i += 4 {
		if canvas.Pixels[i] != 0 {
			t.Fatal("found a visible pixel")
		}
	}
}

func TestOpeningGlassIsSmallerAndFaded(t *testing.T) {
	const radius, scale = 150, 1.0
	canvas := NewImage(Side(radius, scale), Side(radius, scale))
	Compose(canvas, gradientCapture(radius, scale), Scene{Frame: lens.Frame{GlassScale: 0.6, Alpha: 0.5, Blur: 4, Zoom: 1.2}, Radius: radius, Theme: Light})
	centre := Pad(scale) + radius
	if a := pixel(canvas, centre, centre)[3]; a < 120 || a > 135 {
		t.Fatalf("half-faded centre alpha %d", a)
	}
	if a := pixel(canvas, centre+int(radius*0.8), centre)[3]; a > 60 {
		t.Fatalf("outside the 60%% glass should be only faint shadow, alpha %d", a)
	}
}

func BenchmarkComposeSteady200Percent(b *testing.B) {
	benchmarkCompose(b, lens.Frame{GlassScale: 1, Alpha: 1, Zoom: 1.8})
}

func BenchmarkComposeSteadyEdgeRamp200Percent(b *testing.B) {
	edgeRampForBenchmark = true
	defer func() { edgeRampForBenchmark = false }()
	benchmarkCompose(b, lens.Frame{GlassScale: 1, Alpha: 1, Zoom: 1.8})
}

var edgeRampForBenchmark bool

func BenchmarkComposeOpening200Percent(b *testing.B) {
	benchmarkCompose(b, lens.Frame{GlassScale: 0.8, Alpha: 0.6, Blur: 3, Zoom: 1.4, LabelAlpha: 0.3})
}

// benchmarkCompose times one frame: steady frames reuse the layer, opening frames rebuild it (each
// opening frame has a different glass size).
func benchmarkCompose(b *testing.B, frame lens.Frame) {
	const radius, scale = 300, 2.0
	capture := gradientCapture(radius, scale)
	canvas := NewImage(Side(radius, scale), Side(radius, scale))
	renderer := &Renderer{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		capture.blurred = nil // a fresh capture every frame
		if frame.Alpha < 1 {
			frame.GlassScale += 1e-9 // a new glass size, as while opening
		}
		renderer.Compose(canvas, capture, Scene{Frame: frame, Radius: radius, Theme: Light, EdgeRamp: edgeRampForBenchmark})
	}
}

func absDiff(a, b byte) byte {
	if a > b {
		return a - b
	}
	return b - a
}
