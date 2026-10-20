//go:build windows

package main

import (
	"os"
	"path/filepath"
)

// The app has no console (-H windowsgui), so a panic used to vanish with it. The runtime writes a
// panic to whatever the standard error handle is at that moment, so pointing that handle at a file
// keeps the trace: crash.log in the settings folder, which a bug report can attach. Nothing else
// writes there (os.Stderr keeps the handle it had at start).
const crashLogLimit = 256 << 10

// recordCrashes starts sending crashes to crash.log, and returns what stops that on a clean exit,
// removing the log if nothing was written.
func recordCrashes() func() {
	folder, err := settingsFolder()
	if err != nil || os.MkdirAll(folder, 0o755) != nil {
		return func() {}
	}
	path := filepath.Join(folder, "crash.log")
	flags := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	if info, err := os.Stat(path); err == nil && info.Size() > crashLogLimit {
		flags |= os.O_TRUNC // a long history helps no one: start again
	}
	file, err := os.OpenFile(path, flags, 0o644)
	if err != nil {
		return func() {}
	}
	if ok, _, _ := procSetStdHandle.Call(stdErrorHandle, file.Fd()); ok == 0 {
		file.Close()
		return func() {}
	}
	return func() {
		procSetStdHandle.Call(stdErrorHandle, os.Stderr.Fd())
		info, err := file.Stat()
		file.Close()
		if err == nil && info.Size() == 0 {
			os.Remove(path)
		}
	}
}
