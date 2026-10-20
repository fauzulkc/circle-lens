//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCrashesReachTheLog: a panic in a copy that records crashes ends up in crash.log, trace and all.
func TestCrashesReachTheLog(t *testing.T) {
	if os.Getenv("CIRCLE_LENS_CRASH_TEST") == "1" {
		recordCrashes()
		panic("a test crash")
	}
	config := t.TempDir()
	command := exec.Command(os.Args[0], "-test.run=^TestCrashesReachTheLog$")
	command.Env = append(os.Environ(), "CIRCLE_LENS_CRASH_TEST=1", "APPDATA="+config)
	if err := command.Run(); err == nil {
		t.Fatal("the crashing copy exited cleanly")
	}
	text, err := os.ReadFile(filepath.Join(config, instance, "crash.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(text), "panic: a test crash") || !strings.Contains(string(text), "goroutine ") {
		t.Errorf("crash.log lacks the panic and its trace:\n%s", text)
	}
}

// TestNoCrashLeavesNoLog: a clean exit removes the empty log.
func TestNoCrashLeavesNoLog(t *testing.T) {
	config := t.TempDir()
	t.Setenv("APPDATA", config)
	stop := recordCrashes()
	path := filepath.Join(config, instance, "crash.log")
	if _, err := os.Stat(path); err != nil {
		t.Fatal("no crash.log while running:", err)
	}
	stop()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("an empty crash.log was left behind")
	}
}
