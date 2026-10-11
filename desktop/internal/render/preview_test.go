package render

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"strconv"
	"testing"

	"circlelens/internal/lens"
	"circlelens/internal/motion"
)

// TestPreview draws the lens over a screenshot, for judging it by eye (and for pictures of it), from the
// real state machine and renderer. Nothing runs unless PREVIEW_SOURCE and PREVIEW_OUT are set:
//
//	PREVIEW_SOURCE=page.png PREVIEW_OUT=out.png go test -run Preview ./internal/render
//
// PREVIEW_MODE: still (default), open or close (a strip of frames), close-before (the closing of 0.3.0,
// which faded as it shrank, for comparison). PREVIEW_ZOOM (1.8), PREVIEW_RIM (1 for the convex rim),
// PREVIEW_X / PREVIEW_Y (the pointer, in screenshot pixels; the middle by default), PREVIEW_FRAMES (8).
func TestPreview(t *testing.T) {
	sourcePath, outPath := os.Getenv("PREVIEW_SOURCE"), os.Getenv("PREVIEW_OUT")
	if sourcePath == "" || outPath == "" {
		t.Skip("set PREVIEW_SOURCE and PREVIEW_OUT")
	}
	page := loadRGBA(t, sourcePath)
	width, height := page.Bounds().Dx(), page.Bounds().Dy()
	capture := &Capture{Image: *NewImage(width, height), Scale: 2, PointerX: envFloat("PREVIEW_X", float64(width)/2), PointerY: envFloat("PREVIEW_Y", float64(height)/2)}
	for i := 0; i < width*height; i++ {
		capture.Pixels[i*4], capture.Pixels[i*4+1], capture.Pixels[i*4+2], capture.Pixels[i*4+3] = page.Pix[i*4+2], page.Pix[i*4+1], page.Pix[i*4], 255
	}
	const radius = 300
	zoom := envFloat("PREVIEW_ZOOM", 1.8)
	scene := func(frame lens.Frame) Scene {
		return Scene{Frame: frame, Radius: radius, Theme: Light, EdgeRamp: os.Getenv("PREVIEW_RIM") == "1"}
	}
	side := Side(radius, 2)
	// Each frame is drawn over the screenshot around the pointer, as it would sit on screen.
	frameOverPage := func(frame lens.Frame) *image.RGBA {
		canvas := NewImage(side, side)
		Compose(canvas, capture, scene(frame))
		out := image.NewRGBA(image.Rect(0, 0, side, side))
		left, top := int(capture.PointerX)-side/2, int(capture.PointerY)-side/2
		for y := 0; y < side; y++ {
			for x := 0; x < side; x++ {
				c := canvas.Pixels[(y*side+x)*4:]
				sx, sy := min(max(left+x, 0), width-1), min(max(top+y, 0), height-1)
				under := page.Pix[(sy*width+sx)*4:]
				keep := 255 - int(c[3])
				o := out.Pix[(y*side+x)*4:]
				o[0], o[1], o[2], o[3] = byte(int(c[2])+int(under[0])*keep/255), byte(int(c[1])+int(under[1])*keep/255), byte(int(c[0])+int(under[2])*keep/255), 255
			}
		}
		return out
	}

	var frames []lens.Frame
	count := int(envFloat("PREVIEW_FRAMES", 8))
	switch os.Getenv("PREVIEW_MODE") {
	case "open":
		state := lens.New(zoom)
		state.Open(0, false)
		for f := 0; f < count; f++ {
			frames = append(frames, state.Frame(float64(f)*motion.InMs/float64(count-1)))
		}
	case "close":
		state := lens.New(zoom)
		state.Open(0, false)
		state.Frame(1000)
		state.Close(1000)
		for f := 0; f < count; f++ {
			frames = append(frames, state.Frame(1000+float64(f)*motion.OutMs/float64(count-1)))
		}
	case "close-before":
		// 0.3.0: 300 ms, fading the whole time as it shrank to 62%.
		for f := 0; f < count; f++ {
			e := motion.Leave(float64(f) / float64(count-1))
			frames = append(frames, lens.Frame{GlassScale: 1 + (0.62-1)*e, Alpha: 1 - e, Blur: lens.BlurMax * e, Zoom: zoom + (1-zoom)*e})
		}
	default:
		writePNG(t, outPath, frameOverPage(lens.Frame{GlassScale: 1, Alpha: 1, Zoom: zoom, LabelAlpha: 1}))
		return
	}

	// A strip: each frame at a third of its size, side by side.
	cell := side / 3
	strip := image.NewRGBA(image.Rect(0, 0, cell*len(frames), cell))
	draw.Draw(strip, strip.Bounds(), &image.Uniform{color.RGBA{246, 246, 244, 255}}, image.Point{}, draw.Src)
	for f, frame := range frames {
		picture := frameOverPage(frame)
		for y := 0; y < cell; y++ {
			for x := 0; x < cell; x++ {
				strip.Set(f*cell+x, y, picture.At(x*3+1, y*3+1))
			}
		}
	}
	writePNG(t, outPath, strip)
}

func loadRGBA(t *testing.T, path string) *image.RGBA {
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	decoded, err := png.Decode(file)
	if err != nil {
		t.Fatal(err)
	}
	rgba := image.NewRGBA(decoded.Bounds())
	draw.Draw(rgba, rgba.Bounds(), decoded, decoded.Bounds().Min, draw.Src)
	return rgba
}

func writePNG(t *testing.T, path string, picture image.Image) {
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := png.Encode(file, picture); err != nil {
		t.Fatal(err)
	}
}

func envFloat(name string, fallback float64) float64 {
	if value, err := strconv.ParseFloat(os.Getenv(name), 64); err == nil && !math.IsNaN(value) {
		return value
	}
	return fallback
}
