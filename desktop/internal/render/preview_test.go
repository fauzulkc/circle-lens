package render

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"testing"

	"circlelens/internal/lens"
)

// TestPreview draws one steady frame from a real screenshot, for judging the lens by eye:
//
//	PREVIEW_SOURCE=shot.png PREVIEW_OUT=frame.png go test -run Preview ./internal/render
func TestPreview(t *testing.T) {
	sourcePath, outPath := os.Getenv("PREVIEW_SOURCE"), os.Getenv("PREVIEW_OUT")
	if sourcePath == "" || outPath == "" {
		t.Skip("set PREVIEW_SOURCE and PREVIEW_OUT")
	}
	file, err := os.Open(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(file)
	file.Close()
	if err != nil {
		t.Fatal(err)
	}
	rgba := image.NewRGBA(decoded.Bounds())
	draw.Draw(rgba, rgba.Bounds(), decoded, decoded.Bounds().Min, draw.Src)
	width, height := rgba.Bounds().Dx(), rgba.Bounds().Dy()
	capture := &Capture{Image: *NewImage(width, height), Scale: 2, PointerX: float64(width) / 2, PointerY: float64(height) / 2}
	for i := 0; i < width*height; i++ {
		capture.Pixels[i*4], capture.Pixels[i*4+1], capture.Pixels[i*4+2], capture.Pixels[i*4+3] = rgba.Pix[i*4+2], rgba.Pix[i*4+1], rgba.Pix[i*4], 255
	}
	const radius = 300
	canvas := NewImage(Side(radius, 2), Side(radius, 2))
	Compose(canvas, capture, Scene{Frame: lens.Frame{GlassScale: 1, Alpha: 1, Zoom: 1.8, LabelAlpha: 1}, Radius: radius, Theme: Dark, EdgeRamp: os.Getenv("PREVIEW_FLAT") == ""})
	// Over the screenshot itself, as it would sit on screen.
	out := image.NewRGBA(image.Rect(0, 0, canvas.Width, canvas.Height))
	left, top := width/2-canvas.Width/2, height/2-canvas.Height/2
	for y := 0; y < canvas.Height; y++ {
		for x := 0; x < canvas.Width; x++ {
			c := canvas.Pixels[(y*canvas.Width+x)*4:]
			sx, sy := min(max(left+x, 0), width-1), min(max(top+y, 0), height-1)
			under := rgba.Pix[(sy*width+sx)*4:]
			keep := 255 - int(c[3])
			o := out.Pix[(y*canvas.Width+x)*4:]
			o[0], o[1], o[2], o[3] = byte(int(c[2])+int(under[0])*keep/255), byte(int(c[1])+int(under[1])*keep/255), byte(int(c[0])+int(under[2])*keep/255), 255
		}
	}
	file, err = os.Create(outPath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := png.Encode(file, out); err != nil {
		t.Fatal(err)
	}
}

// TestPreviewClosing draws the closing animation as a strip of frames, from the real state machine:
//
//	PREVIEW_SOURCE=shot.png PREVIEW_STRIP=strip.png go test -run PreviewClosing ./internal/render
func TestPreviewClosing(t *testing.T) {
	sourcePath, outPath := os.Getenv("PREVIEW_SOURCE"), os.Getenv("PREVIEW_STRIP")
	if sourcePath == "" || outPath == "" {
		t.Skip("set PREVIEW_SOURCE and PREVIEW_STRIP")
	}
	file, err := os.Open(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(file)
	file.Close()
	if err != nil {
		t.Fatal(err)
	}
	rgba := image.NewRGBA(decoded.Bounds())
	draw.Draw(rgba, rgba.Bounds(), decoded, decoded.Bounds().Min, draw.Src)
	width, height := rgba.Bounds().Dx(), rgba.Bounds().Dy()
	capture := &Capture{Image: *NewImage(width, height), Scale: 2, PointerX: float64(width) / 2, PointerY: float64(height) / 2}
	for i := 0; i < width*height; i++ {
		capture.Pixels[i*4], capture.Pixels[i*4+1], capture.Pixels[i*4+2], capture.Pixels[i*4+3] = rgba.Pix[i*4+2], rgba.Pix[i*4+1], rgba.Pix[i*4], 255
	}
	const radius, frames = 300, 8
	state := lens.New(1.8)
	state.Open(0, false)
	state.Frame(1000)
	state.Close(1000)
	side := Side(radius, 2)
	cell := side / 3
	strip := image.NewRGBA(image.Rect(0, 0, cell*frames, cell))
	draw.Draw(strip, strip.Bounds(), &image.Uniform{color.RGBA{40, 44, 52, 255}}, image.Point{}, draw.Src)
	for f := 0; f < frames; f++ {
		canvas := NewImage(side, side)
		Compose(canvas, capture, Scene{Frame: state.Frame(1000 + float64(f)*340/(frames-1)), Radius: radius, Theme: Dark})
		for y := 0; y < cell; y++ {
			for x := 0; x < cell; x++ {
				c := canvas.Pixels[((y*3)*side+x*3)*4:]
				o := strip.Pix[(y*strip.Bounds().Dx()+f*cell+x)*4:]
				keep := 255 - int(c[3])
				o[0], o[1], o[2] = byte(int(c[2])+int(o[0])*keep/255), byte(int(c[1])+int(o[1])*keep/255), byte(int(c[0])+int(o[2])*keep/255)
			}
		}
	}
	out, err := os.Create(outPath)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	if err := png.Encode(out, strip); err != nil {
		t.Fatal(err)
	}
}
