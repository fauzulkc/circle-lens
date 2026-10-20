//go:build windows

package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
	"unsafe"
)

const wdaMonitor = 0x01 // WDA_MONITOR: shown black in captures (every Windows since 7)

// TestCaptureExclusion reports whether this Windows leaves the lens window out of screen captures, the
// way the app needs for a live lens. It makes the overlay exactly as the app does (newOverlay), shows a
// solid magenta square in it, and captures that spot as the capturer does. It puts a window on the
// desktop, so it runs only when asked: CIRCLE_LENS_EXCLUSION_TEST=1 (the exclusion workflow sets it).
// It reports rather than fails: the answer differs between Windows versions.
func TestCaptureExclusion(t *testing.T) {
	if os.Getenv("CIRCLE_LENS_EXCLUSION_TEST") != "1" {
		t.Skip("set CIRCLE_LENS_EXCLUSION_TEST=1 to run it (it shows a window)")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	var report strings.Builder
	note := func(format string, args ...any) {
		line := fmt.Sprintf(format, args...)
		t.Log(line)
		report.WriteString("- " + line + "\n")
	}

	module, _, _ := procGetModuleHandle.Call(0)
	lens, ok := newOverlay(module)
	if !ok {
		t.Fatal("could not create the overlay window")
	}
	defer procDestroyWindow.Call(lens.window)
	note("overlay (layered, transparent, topmost): newOverlay says excluded = %v", lens.excluded)

	// The call again, for its error, and what Windows says the window's affinity now is.
	result, _, err := procSetWindowDisplayAff.Call(lens.window, wdaExcludeFromCapture)
	note("SetWindowDisplayAffinity(overlay, WDA_EXCLUDEFROMCAPTURE) = %d (%v); GetWindowDisplayAffinity = %#x", result, err, affinityOf(lens.window))
	result, _, err = procSetWindowDisplayAff.Call(lens.window, wdaMonitor)
	note("SetWindowDisplayAffinity(overlay, WDA_MONITOR) = %d (%v)", result, err)
	procSetWindowDisplayAff.Call(lens.window, wdaExcludeFromCapture)

	// For comparison: a plain popup window, not layered.
	plainClass := wide(instance + "ExclusionProbe")
	class := windowClassEx{WndProc: procDefWindowProc.Addr(), Instance: module, ClassName: plainClass}
	class.Size = uint32(unsafe.Sizeof(class))
	procRegisterClassEx.Call(uintptr(unsafe.Pointer(&class)))
	plain, _, _ := procCreateWindowEx.Call(wsExToolWindow, uintptr(unsafe.Pointer(plainClass)), 0, wsPopup, 0, 0, 1, 1, 0, 0, module, 0)
	if plain != 0 {
		defer procDestroyWindow.Call(plain)
		result, _, err = procSetWindowDisplayAff.Call(plain, wdaExcludeFromCapture)
		note("SetWindowDisplayAffinity(plain popup, WDA_EXCLUDEFROMCAPTURE) = %d (%v)", result, err)
	}

	// Show a magenta square in the overlay, as overlay.draw does, then capture that spot.
	const left, top, side = 100, 100, 200
	canvas, ok := newDIB(side, side)
	if !ok {
		t.Fatal("could not make a bitmap")
	}
	defer canvas.Close()
	for i := 0; i < len(canvas.Pixels); i += 4 {
		canvas.Pixels[i], canvas.Pixels[i+1], canvas.Pixels[i+2], canvas.Pixels[i+3] = 255, 0, 255, 255
	}
	before := magentaAt(left+side/2, top+side/2, srcCopy|captureBlt)
	destination, dimensions, origin := point{left, top}, size{side, side}, point{}
	blend := blendFunction{SourceConstantAlpha: 255, AlphaFormat: acSrcAlpha}
	shown, _, err := procUpdateLayeredWindow.Call(lens.window, 0, uintptr(unsafe.Pointer(&destination)), uintptr(unsafe.Pointer(&dimensions)), canvas.dc,
		uintptr(unsafe.Pointer(&origin)), 0, uintptr(unsafe.Pointer(&blend)), ulwAlpha)
	procShowWindow.Call(lens.window, swShowNoActivate)
	procSetWindowPos.Call(lens.window, hwndTopmost, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoActivate)
	for i := 0; i < 10; i++ {
		procDwmFlush.Call()
	}
	time.Sleep(300 * time.Millisecond)
	note("UpdateLayeredWindow = %d (%v); the spot was magenta before showing it: %v", shown, err, before)
	note("**the lens is in a capture with CAPTUREBLT (as the app captures): %v**", magentaAt(left+side/2, top+side/2, srcCopy|captureBlt))
	note("the lens is in a capture without CAPTUREBLT: %v", magentaAt(left+side/2, top+side/2, srcCopy))
	// The control: with no affinity the square must show, or the two answers above mean nothing.
	procSetWindowDisplayAff.Call(lens.window, 0)
	for i := 0; i < 5; i++ {
		procDwmFlush.Call()
	}
	time.Sleep(200 * time.Millisecond)
	note("control, no affinity: the lens is in a capture with CAPTUREBLT: %v (should be true)", magentaAt(left+side/2, top+side/2, srcCopy|captureBlt))

	if summary := os.Getenv("GITHUB_STEP_SUMMARY"); summary != "" {
		if file, err := os.OpenFile(summary, os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
			fmt.Fprintf(file, "### Capture exclusion\n\n%s\n", report.String())
			file.Close()
		}
	}
}

// magentaAt captures one screen pixel with the given raster operation and says whether it's the probe's magenta.
func magentaAt(x, y int, operation uintptr) bool {
	pixel, ok := newDIB(1, 1)
	if !ok {
		return false
	}
	defer pixel.Close()
	screen, _, _ := procGetDC.Call(0)
	defer procReleaseDC.Call(0, screen)
	procBitBlt.Call(pixel.dc, 0, 0, 1, 1, screen, uintptr(x), uintptr(y), operation)
	procGdiFlush.Call()
	return pixel.Pixels[0] > 240 && pixel.Pixels[1] < 15 && pixel.Pixels[2] > 240
}

func affinityOf(window uintptr) uint32 {
	var affinity uint32
	user32.NewProc("GetWindowDisplayAffinity").Call(window, uintptr(unsafe.Pointer(&affinity)))
	return affinity
}
