//go:build windows

package main

// The exe installs itself: there is no separate installer. The file handed out
// (CircleLens-Setup-<version>.exe) is the app; started from anywhere other than its install folder, it
// offers to install:
//
//   - for the person only, so no administrator: %LOCALAPPDATA%\Programs\Circle Lens\circle-lens.exe;
//   - a Start menu entry, Start with Windows, and an entry in Settings → Apps whose Uninstall runs
//     `circle-lens.exe -uninstall`;
//   - a newer one updates the installed copy, taking over from it while it runs; the same version just
//     starts the installed copy.
//
// Everything it creates is named after `instance`, so a test build installs as its own app.

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// displayName is the name in the Start menu and Settings → Apps.
func displayName() string {
	if instance == "CircleLens" {
		return "Circle Lens"
	}
	return "Circle Lens (" + strings.TrimPrefix(instance, "CircleLens") + ")"
}

func installDirectory() string {
	return filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", displayName())
}

func installedExecutable() string { return filepath.Join(installDirectory(), "circle-lens.exe") }

func startMenuShortcut() string {
	return filepath.Join(os.Getenv("APPDATA"), `Microsoft\Windows\Start Menu\Programs`, displayName()+".lnk")
}

func uninstallKey() string { return `Software\Microsoft\Windows\CurrentVersion\Uninstall\` + instance }

// runningInstalled is true when this is the installed copy.
func runningInstalled() bool {
	executable, err := os.Executable()
	if err != nil {
		return false
	}
	return strings.EqualFold(filepath.Clean(executable), filepath.Clean(installedExecutable()))
}

// installedVersion is the version in Settings → Apps, or "" if it is not installed.
func installedVersion() string {
	if _, err := os.Stat(installedExecutable()); err != nil {
		return ""
	}
	return readString(uninstallKey(), "DisplayVersion")
}

// offerInstall runs when this copy is not the installed one. It returns true when this copy should
// stop: it installed (or found the same version installed) and started that copy instead.
func offerInstall(declined bool, rememberDeclined func()) bool {
	installed := installedVersion()
	switch {
	case installed == version:
		return startInstalled() // already there: use that copy, not a second one elsewhere
	case installed == "" && declined:
		return false // asked once already; the tray menu can still install it
	}
	question := fmt.Sprintf("Install %s %s?\n\nIt installs for you only (no administrator needed), adds it to the Start menu, starts it with Windows, and can be removed in Settings → Apps.\n\nChoose No to just run it this time.", displayName(), version)
	if installed != "" {
		question = fmt.Sprintf("Update %s from %s to %s?\n\nChoose No to just run this copy this time.", displayName(), installed, version)
	}
	if !ask(question) {
		if installed == "" {
			rememberDeclined()
		}
		return false
	}
	if err := install(); err != nil {
		tell("Could not install "+displayName()+": "+err.Error(), mbIconError)
		return false
	}
	return startInstalled()
}

// install copies this exe into place and registers it. A copy already running is asked to quit first,
// so the new one can take its place.
func install() error {
	if running, _, _ := procFindWindow.Call(uintptr(unsafe.Pointer(wide(instance+"App"))), 0); running != 0 && !handOver(running) {
		return errors.New("the copy that is running would not close")
	}
	source, err := os.Executable()
	if err != nil {
		return err
	}
	directory := installDirectory()
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	if err := copyFile(source, installedExecutable()); err != nil {
		return err
	}
	if err := createShortcut(startMenuShortcut(), installedExecutable(), directory, "Draw a circle with the mouse to magnify"); err != nil {
		return err
	}
	size := int64(0)
	if info, err := os.Stat(installedExecutable()); err == nil {
		size = info.Size()
	}
	key := uninstallKey()
	quoted := `"` + installedExecutable() + `"`
	for name, value := range map[string]string{
		"DisplayName":          displayName(),
		"DisplayVersion":       version,
		"DisplayIcon":          installedExecutable(),
		"Publisher":            "Fauzul Chowdhury",
		"InstallLocation":      directory,
		"InstallDate":          time.Now().Format("20060102"),
		"UninstallString":      quoted + " -uninstall",
		"QuietUninstallString": quoted + " -uninstall -quiet",
	} {
		if err := writeString(key, name, value); err != nil {
			return err
		}
	}
	for name, value := range map[string]uint32{"NoModify": 1, "NoRepair": 1, "EstimatedSize": uint32(size / 1024)} {
		if err := writeDword(key, name, value); err != nil {
			return err
		}
	}
	setStartupCommand(installedExecutable())
	return nil
}

// copyFile copies through a temporary file, so a half-copied exe never takes the real one's place.
func copyFile(from, to string) error {
	if strings.EqualFold(filepath.Clean(from), filepath.Clean(to)) {
		return nil
	}
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	temporary := to + ".new"
	out, err := os.Create(temporary)
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

func startInstalled() bool {
	if err := exec.Command(installedExecutable()).Start(); err != nil {
		tell("Installed, but could not start it: "+err.Error(), mbIconError)
		return false
	}
	return true
}

// uninstall removes the app, its Start menu entry, start-up entry, Settings → Apps entry and settings.
// The exe cannot delete the folder it runs from, so a hidden command prompt does that once it exits.
func uninstall(quiet bool) {
	if !quiet && !ask("Uninstall "+displayName()+"?\n\nIts settings are removed too.") {
		return
	}
	if running, _, _ := procFindWindow.Call(uintptr(unsafe.Pointer(wide(instance+"App"))), 0); running != 0 && !handOver(running) {
		if !quiet {
			tell("Close "+displayName()+" from its tray icon first, then uninstall again.", mbIconError)
		}
		return
	}
	setStartsWithWindows(false)
	os.Remove(startMenuShortcut())
	deleteKey(uninstallKey())
	if path, err := settingsFolder(); err == nil {
		os.RemoveAll(path)
	}
	directory := installDirectory()
	if runningInstalled() {
		// ping is the delay that works without a console (timeout does not); the command line is passed
		// exactly as written, since Go's argument quoting is not what cmd.exe expects.
		command := exec.Command("cmd.exe")
		command.SysProcAttr = &syscall.SysProcAttr{
			CmdLine:       `cmd.exe /c ping -n 3 127.0.0.1 >nul & rmdir /s /q "` + directory + `"`,
			HideWindow:    true,
			CreationFlags: createNoWindow,
		}
		_ = command.Start()
	} else {
		os.RemoveAll(directory)
	}
	if !quiet {
		tell(displayName()+" was uninstalled.", mbIconInformation)
	}
}

func settingsFolder() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, instance), nil
}

// ask shows a Yes/No question; tell shows a message.
func ask(question string) bool {
	answer, _, _ := procMessageBox.Call(0, uintptr(unsafe.Pointer(wide(question))), uintptr(unsafe.Pointer(wide(displayName()))), mbYesNo|mbIconQuestion)
	return answer == idYes
}

func tell(text string, icon uintptr) {
	procMessageBox.Call(0, uintptr(unsafe.Pointer(wide(text))), uintptr(unsafe.Pointer(wide(displayName()))), icon)
}

/* ---------- registry, under HKEY_CURRENT_USER ---------- */

func createKey(path string) (uintptr, error) {
	var key uintptr
	if result, _, _ := procRegCreateKeyEx.Call(hkeyCurrentUser, uintptr(unsafe.Pointer(wide(path))), 0, 0, 0, keySetValue|keyQueryValue, 0,
		uintptr(unsafe.Pointer(&key)), 0); result != 0 {
		return 0, fmt.Errorf("could not write to the registry (%d)", result)
	}
	return key, nil
}

func writeString(path, name, value string) error {
	key, err := createKey(path)
	if err != nil {
		return err
	}
	defer procRegCloseKey.Call(key)
	data := append(wideSlice(value), 0)
	if result, _, _ := procRegSetValueEx.Call(key, uintptr(unsafe.Pointer(wide(name))), 0, regSz, uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)*2)); result != 0 {
		return fmt.Errorf("could not write %s (%d)", name, result)
	}
	return nil
}

func writeDword(path, name string, value uint32) error {
	key, err := createKey(path)
	if err != nil {
		return err
	}
	defer procRegCloseKey.Call(key)
	if result, _, _ := procRegSetValueEx.Call(key, uintptr(unsafe.Pointer(wide(name))), 0, regDword, uintptr(unsafe.Pointer(&value)), 4); result != 0 {
		return fmt.Errorf("could not write %s (%d)", name, result)
	}
	return nil
}

func readString(path, name string) string {
	var key uintptr
	if result, _, _ := procRegOpenKeyEx.Call(hkeyCurrentUser, uintptr(unsafe.Pointer(wide(path))), 0, keyQueryValue, uintptr(unsafe.Pointer(&key))); result != 0 {
		return ""
	}
	defer procRegCloseKey.Call(key)
	buffer := make([]uint16, 256)
	length := uint32(len(buffer) * 2)
	var kind uint32
	if result, _, _ := procRegQueryValueEx.Call(key, uintptr(unsafe.Pointer(wide(name))), 0, uintptr(unsafe.Pointer(&kind)),
		uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&length))); result != 0 || kind != regSz {
		return ""
	}
	return syscall.UTF16ToString(buffer)
}

func deleteKey(path string) {
	procRegDeleteKey.Call(hkeyCurrentUser, uintptr(unsafe.Pointer(wide(path))))
}
