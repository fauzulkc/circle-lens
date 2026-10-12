package main

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"runtime/pprof"
	"time"

	"circlelens/internal/render"
)

// debugLog appends a line to the file named by CIRCLE_LENS_LOG, when it is set (for diagnosing a machine).
var debugFile = func() *os.File {
	path := os.Getenv("CIRCLE_LENS_LOG")
	if path == "" {
		return nil
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil
	}
	return file
}()

// CIRCLE_LENS_PROFILE names a file to write a CPU profile to, from start to quit (go tool pprof).
func startProfile() func() {
	path := os.Getenv("CIRCLE_LENS_PROFILE")
	if path == "" {
		return func() {}
	}
	file, err := os.Create(path)
	if err != nil || pprof.StartCPUProfile(file) != nil {
		return func() {}
	}
	return func() {
		pprof.StopCPUProfile()
		file.Close()
	}
}

func debugLog(format string, values ...any) {
	if debugFile != nil {
		fmt.Fprintf(debugFile, "%s "+format+"\n", append([]any{time.Now().Format("15:04:05.000")}, values...)...)
	}
}

// CIRCLE_LENS_FRAMES names a folder to save the first frames of each lens to, as PNGs. The lens is
// left out of screen captures, so this is how to see what it drew.
var (
	framesFolder = os.Getenv("CIRCLE_LENS_FRAMES")
	framesSaved  int
)

const framesToSave = 80

func saveFrame(canvas *render.Image) {
	if framesFolder == "" || framesSaved >= framesToSave {
		return
	}
	picture := image.NewNRGBA(image.Rect(0, 0, canvas.Width, canvas.Height))
	for i := 0; i < canvas.Width*canvas.Height; i++ {
		b, g, r, a := canvas.Pixels[i*4], canvas.Pixels[i*4+1], canvas.Pixels[i*4+2], canvas.Pixels[i*4+3]
		if a > 0 {
			r, g, b = byte(int(r)*255/int(a)), byte(int(g)*255/int(a)), byte(int(b)*255/int(a))
		}
		picture.Pix[i*4], picture.Pix[i*4+1], picture.Pix[i*4+2], picture.Pix[i*4+3] = r, g, b, a
	}
	if file, err := os.Create(filepath.Join(framesFolder, fmt.Sprintf("frame-%03d.png", framesSaved))); err == nil {
		png.Encode(file, picture)
		file.Close()
	}
	framesSaved++
}
