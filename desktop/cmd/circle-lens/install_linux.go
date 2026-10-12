//go:build linux

package main

// Installing on Linux is per user and needs no root: the program is copied to ~/.local/bin, with an
// app-menu entry and icon under ~/.local/share and a start-up entry in ~/.config/autostart (the XDG
// places every desktop reads). Everything is named after instance, so a test build never touches the
// copy in daily use.

import (
	"bytes"
	"fmt"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"circlelens/internal/icon"
)

// appID names the files: "circle-lens", or the test build's own.
func appID() string {
	if instance == "CircleLens" {
		return "circle-lens"
	}
	return strings.ToLower(instance)
}

func displayName() string {
	if instance == "CircleLens" {
		return "Circle Lens"
	}
	return "Circle Lens (" + strings.TrimPrefix(instance, "CircleLens") + ")"
}

func dataHome() string {
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share")
}

func configHome() string {
	dir, _ := os.UserConfigDir()
	return dir
}

func installedPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "bin", appID())
}

func menuEntryPath() string { return filepath.Join(dataHome(), "applications", appID()+".desktop") }
func iconPath() string {
	return filepath.Join(dataHome(), "icons", "hicolor", "256x256", "apps", appID()+".png")
}
func autostartPath() string { return filepath.Join(configHome(), "autostart", appID()+".desktop") }

// desktopEntry is the .desktop file for the app menu and for start-up.
func desktopEntry(program string, atLogin bool) string {
	lines := []string{
		"[Desktop Entry]",
		"Type=Application",
		"Name=" + displayName(),
		"Comment=Draw a circle twice to magnify",
		"Exec=" + program,
		"Icon=" + appID(),
		"Terminal=false",
		"Categories=Utility;Accessibility;",
		"StartupNotify=false",
	}
	if atLogin {
		lines = append(lines, "X-GNOME-Autostart-enabled=true")
	}
	return strings.Join(lines, "\n") + "\n"
}

// install copies this program into place, adds the menu entry, icon and start-up entry, then starts it.
func install() error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	target := installedPath()
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	if resolved, _ := filepath.EvalSymlinks(self); resolved != target {
		sendCommand("quit")
		if err := copyProgram(self, target); err != nil {
			return err
		}
	}
	var picture bytes.Buffer
	png.Encode(&picture, icon.DrawApp(256))
	for _, file := range []struct {
		path string
		data []byte
		mode os.FileMode
	}{
		{iconPath(), picture.Bytes(), 0o644},
		{menuEntryPath(), []byte(desktopEntry(target, false)), 0o644},
		{autostartPath(), []byte(desktopEntry(target, true)), 0o644},
	} {
		if err := os.MkdirAll(filepath.Dir(file.path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(file.path, file.data, file.mode); err != nil {
			return err
		}
	}
	exec.Command("update-desktop-database", filepath.Dir(menuEntryPath())).Run()
	fmt.Printf("Installed %s %s to %s. It starts at login, and is in your app menu.\n", displayName(), version, target)
	return startDetached(target)
}

// copyProgram writes the program next to its target and renames it into place, which works even while
// the old copy is running.
func copyProgram(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	temporary := to + ".new"
	out, err := os.OpenFile(temporary, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(temporary)
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(temporary, to)
}

func uninstall() error {
	for _, path := range []string{autostartPath(), menuEntryPath(), iconPath(), installedPath()} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	fmt.Printf("Uninstalled %s. Its settings stay in %s.\n", displayName(), filepath.Join(configHome(), instance))
	return nil
}

// startDetached starts a program in its own session, so it outlives this one and the terminal.
func startDetached(program string) error {
	command := exec.Command(program)
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return command.Start()
}

func (s *x11System) startsAtLogin() bool {
	_, err := os.Stat(autostartPath())
	return err == nil
}

// setStartAtLogin adds or removes the start-up entry, pointing at the installed copy if there is one.
func (s *x11System) setStartAtLogin(on bool) error {
	if !on {
		err := os.Remove(autostartPath())
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	program := installedPath()
	if _, err := os.Stat(program); err != nil {
		if program, err = os.Executable(); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(autostartPath()), 0o755); err != nil {
		return err
	}
	return os.WriteFile(autostartPath(), []byte(desktopEntry(program, true)), 0o644)
}
