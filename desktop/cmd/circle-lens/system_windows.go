//go:build windows

package main

// What the app reads from (and writes to) Windows itself: theme, animation preference, start-up entry.

import (
	"os"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

const (
	personalizeKey = `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`
	runKey         = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValue       = "CircleLens"
)

// version is set at build time: -ldflags "-X main.version=…".
var version = "dev"

// instance names the single-instance lock and window classes. A test build sets its own
// (-X main.instance=…) so it can run beside the copy in daily use.
var instance = "CircleLens"

// newCallback wraps a Go function as a Win32 callback. Each is made once (Windows callbacks are finite).
func newCallback(function any) uintptr { return syscall.NewCallback(function) }

func wideSlice(text string) []uint16 { return utf16.Encode([]rune(text)) }

// readDword reads a DWORD value under HKEY_CURRENT_USER.
func readDword(path, name string) (uint32, bool) {
	var key uintptr
	if result, _, _ := procRegOpenKeyEx.Call(hkeyCurrentUser, uintptr(unsafe.Pointer(wide(path))), 0, keyQueryValue, uintptr(unsafe.Pointer(&key))); result != 0 {
		return 0, false
	}
	defer procRegCloseKey.Call(key)
	var value, kind uint32
	length := uint32(4)
	if result, _, _ := procRegQueryValueEx.Call(key, uintptr(unsafe.Pointer(wide(name))), 0, uintptr(unsafe.Pointer(&kind)), uintptr(unsafe.Pointer(&value)), uintptr(unsafe.Pointer(&length))); result != 0 || kind != regDword {
		return 0, false
	}
	return value, true
}

// darkMode follows the "app mode" in Settings → Personalisation → Colours (CIRCLE_LENS_THEME=light or
// dark overrides it, for checking both by eye).
func darkMode() bool {
	switch os.Getenv("CIRCLE_LENS_THEME") {
	case "light":
		return false
	case "dark":
		return true
	}
	light, ok := readDword(personalizeKey, "AppsUseLightTheme")
	return ok && light == 0
}

// darkTaskbar follows the "Windows mode" there, which colours the taskbar (dark unless set to light).
func darkTaskbar() bool {
	light, ok := readDword(personalizeKey, "SystemUsesLightTheme")
	return !ok || light == 0
}

// accentColor is the Windows accent colour as RGB (Windows' default blue if none is set).
func accentColor() [3]byte {
	if value, ok := readDword(`Software\Microsoft\Windows\DWM`, "AccentColor"); ok {
		return [3]byte{byte(value), byte(value >> 8), byte(value >> 16)} // stored as 0xAABBGGRR
	}
	return [3]byte{0x00, 0x78, 0xd4}
}

// reducedMotion follows Settings → Accessibility → Visual effects → Animation effects.
func reducedMotion() bool {
	var animate int32 = 1
	procSystemParametersInfo.Call(spiGetClientAreaAnimation, 0, uintptr(unsafe.Pointer(&animate)), 0)
	return animate == 0
}

func openRunKey(access uintptr) (uintptr, bool) {
	var key uintptr
	result, _, _ := procRegOpenKeyEx.Call(hkeyCurrentUser, uintptr(unsafe.Pointer(wide(runKey))), 0, access, uintptr(unsafe.Pointer(&key)))
	return key, result == 0
}

// startsWithWindows is true when the per-user Run entry points at this exe.
func startsWithWindows() bool {
	key, ok := openRunKey(keyQueryValue)
	if !ok {
		return false
	}
	defer procRegCloseKey.Call(key)
	result, _, _ := procRegQueryValueEx.Call(key, uintptr(unsafe.Pointer(wide(runValue))), 0, 0, 0, 0)
	return result == 0
}

func setStartsWithWindows(on bool) {
	key, ok := openRunKey(keySetValue)
	if !ok {
		return
	}
	defer procRegCloseKey.Call(key)
	name := uintptr(unsafe.Pointer(wide(runValue)))
	if !on {
		procRegDeleteValue.Call(key, name)
		return
	}
	executable, err := os.Executable()
	if err != nil {
		return
	}
	command := append(wideSlice(`"`+executable+`"`), 0)
	procRegSetValueEx.Call(key, name, 0, regSz, uintptr(unsafe.Pointer(&command[0])), uintptr(len(command)*2))
}

// monitorScale is the DPI scale (1 = 96 DPI) of the monitor nearest a point.
func monitorScale(at point) float64 {
	monitor, _, _ := procMonitorFromPoint.Call(pointArgument(at), monitorDefaultToNearest)
	var dpiX, dpiY uint32
	if result, _, _ := procGetDpiForMonitor.Call(monitor, mdtEffectiveDpi, uintptr(unsafe.Pointer(&dpiX)), uintptr(unsafe.Pointer(&dpiY))); result != 0 || dpiX == 0 {
		return 1
	}
	return float64(dpiX) / 96
}

// workArea is the work area (the screen less the taskbar) of the monitor nearest a point.
func workArea(at point) rect {
	monitor, _, _ := procMonitorFromPoint.Call(pointArgument(at), monitorDefaultToNearest)
	info := monitorInfo{}
	info.Size = uint32(unsafe.Sizeof(info))
	procGetMonitorInfo.Call(monitor, uintptr(unsafe.Pointer(&info)))
	return info.Work
}
