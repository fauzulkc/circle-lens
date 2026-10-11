//go:build windows

package main

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// TestReplaceFileTakesTheOldOnesPlace: the new file ends up under the old one's name, with nothing left over.
func TestReplaceFileTakesTheOldOnesPlace(t *testing.T) {
	folder := t.TempDir()
	target := filepath.Join(folder, "circle-lens.exe")
	os.WriteFile(target, []byte("old"), 0o644)
	os.WriteFile(target+".new", []byte("new"), 0o644)
	if err := replaceFile(target+".new", target); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(target); string(data) != "new" {
		t.Fatalf("the file holds %q, not the new one", data)
	}
	for _, leftover := range []string{target + ".new", target + ".old"} {
		if _, err := os.Stat(leftover); err == nil {
			t.Errorf("%s was left behind", filepath.Base(leftover))
		}
	}
}

// TestReplaceFileReplacesARunningExe: Windows won't replace the exe of a process that is still running (an
// update used to fail with "Access is denied" while the old copy was closing). replaceFile moves it aside.
func TestReplaceFileReplacesARunningExe(t *testing.T) {
	folder := t.TempDir()
	target := filepath.Join(folder, "circle-lens.exe")
	ping := filepath.Join(os.Getenv("SystemRoot"), "System32", "PING.EXE")
	if err := copyTestFile(ping, target); err != nil {
		t.Skip("no ping.exe to run:", err)
	}
	running := exec.Command(target, "-n", "30", "127.0.0.1")
	if err := running.Start(); err != nil {
		t.Skip("couldn't run the copy:", err)
	}
	defer func() { running.Process.Kill(); running.Wait() }()
	os.WriteFile(target+".new", []byte("new"), 0o644)
	if err := os.Rename(target+".new", target); err == nil {
		t.Skip("Windows replaced the running exe directly, so there is nothing to test")
	}
	if err := replaceFile(target+".new", target); err != nil {
		t.Fatalf("couldn't replace the running exe: %v", err)
	}
	if data, _ := os.ReadFile(target); string(data) != "new" {
		t.Fatal("the running exe wasn't replaced")
	}
}

// TestWithoutPathsLeavesOutPaths: the failed screen names the file, not its full path, which didn't fit.
func TestWithoutPathsLeavesOutPaths(t *testing.T) {
	err := &os.LinkError{Op: "rename", Old: `C:\Users\someone\AppData\Local\Programs\Circle Lens\circle-lens.exe.new`,
		New: `C:\Users\someone\AppData\Local\Programs\Circle Lens\circle-lens.exe`, Err: syscall.Errno(5)}
	got := sentence(withoutPaths(err))
	if got != "Couldn't replace circle-lens.exe: Access is denied." {
		t.Errorf("got %q", got)
	}
	if strings.Contains(withoutPaths(&os.PathError{Op: "open", Path: `C:\a\b\c.exe`, Err: syscall.Errno(2)}), `\`) {
		t.Error("a path error still shows its path")
	}
}

func copyTestFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(to)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
