//go:build windows

package main

// Updates for the installed app, from the project's GitHub releases.
//
//   - With "Update automatically" on (the default), it asks GitHub about the latest release a minute and
//     a half after starting and then once a day. A newer version is downloaded into the install
//     folder's updates\ folder and checked against the size and SHA-256 digest GitHub publishes for it
//     (internal/update). It's installed once the lens is closed and the panel isn't open: the new setup
//     runs with -install -update, takes over from this copy through the usual hand-over, and starts the
//     new version, which says so in a notification.
//   - With it off, nothing is ever fetched on its own. "Check for updates" (panel and menu) checks on
//     demand, and offers the new version rather than installing it.
//   - Only an installed copy of the real app updates. A copy run from a download offers to install
//     instead, and a test build (-X main.instance=…) updates only from a test feed
//     (CIRCLE_LENS_UPDATE_FEED and CIRCLE_LENS_UPDATE_DOWNLOADS), never from the real releases.
//
// The network goes through WinINet, Windows' own HTTP stack (proxy settings, certificates), which keeps
// Go's TLS stack out of the exe. The request carries no more than any download does: the app's name
// and version as its user agent.

import (
	"bytes"
	"errors"
	"fmt"
	"image/color"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"circlelens/internal/icon"
	"circlelens/internal/update"
)

var (
	wininet                 = systemDLL("wininet.dll")
	procInternetOpen        = wininet.NewProc("InternetOpenW")
	procInternetOpenURL     = wininet.NewProc("InternetOpenUrlW")
	procInternetReadFile    = wininet.NewProc("InternetReadFile")
	procInternetCloseHandle = wininet.NewProc("InternetCloseHandle")
	procHTTPQueryInfo       = wininet.NewProc("HttpQueryInfoW")
)

const (
	internetOpenTypePreconfig = 0
	internetFlagReload        = 0x80000000
	internetFlagNoCacheWrite  = 0x04000000
	internetFlagNoCookies     = 0x00080000
	internetFlagNoUI          = 0x00000200
	internetFlagPragmaNoCache = 0x00000100
	httpQueryStatusCode       = 19
	httpQueryFlagNumber       = 0x20000000

	// Posted by the update thread when it has a result for the UI thread.
	messageUpdate = wmApp + 8

	timerUpdate        = 10
	updateFirstCheck   = 90 * time.Second // after starting, so the check never slows a start or a sign-in
	updateTick         = time.Hour        // how often it looks whether a check or an install is due
	updateEvery        = 23 * time.Hour   // a day, give or take when the computer was on
	updateRetry        = 6 * time.Hour    // after a failed check
	updateInstallRetry = time.Minute      // while the lens or the panel is open
	updateFeedLimit    = 1 << 20          // the release JSON is a few KB
)

type updateStatus int

const (
	updateIdle        updateStatus = iota
	updateChecking                 // asking GitHub
	updateCurrent                  // asked: this is the latest
	updateAvailable                // a newer version, not downloaded yet
	updateDownloading              // downloading and checking it
	updateReady                    // downloaded and checked: installs when nothing is in use
	updateInstalling               // the new setup has started and is taking over
	updateFailed                   // the last check or download failed (problem says why)
)

type updater struct {
	status    updateStatus
	release   update.Release
	setup     string // the checked download, once ready
	problem   string
	failedAt  time.Time
	requested bool // the person asked for this update: install as soon as it's downloaded
	tidied    bool // the setup that installed this copy has been deleted
	results   chan func()
}

// updatesSupported says whether this copy updates itself: installed, a release build, and either the
// real app or a test build pointed at a test feed.
func updatesSupported() bool {
	if previewing {
		return true
	}
	if !runningInstalled() || !update.Valid(version) {
		return false
	}
	feed, _ := updateFeed()
	return feed != ""
}

// updateFeed is where to ask about releases, and where their files must come from.
func updateFeed() (feed, downloads string) {
	if instance == "CircleLens" {
		return update.Feed, update.Downloads
	}
	feed, downloads = os.Getenv("CIRCLE_LENS_UPDATE_FEED"), os.Getenv("CIRCLE_LENS_UPDATE_DOWNLOADS")
	if feed == "" || downloads == "" {
		return "", ""
	}
	return feed, downloads
}

func updatesFolder() string { return filepath.Join(installDirectory(), "updates") }

// startUpdates sets up the checks; the first comes a little after starting.
func (a *application) startUpdates() {
	a.updates.results = make(chan func(), 4)
	if !updatesSupported() {
		return
	}
	first := updateFirstCheck
	if instance != "CircleLens" {
		if delay, err := time.ParseDuration(os.Getenv("CIRCLE_LENS_UPDATE_DELAY")); err == nil {
			first = delay
		}
	}
	procSetTimer.Call(a.window, timerUpdate, uintptr(first.Milliseconds()), 0)
}

// updateTimer runs on the update timer: install a waiting update, or check if one is due.
func (a *application) updateTimer() {
	procSetTimer.Call(a.window, timerUpdate, uintptr(updateTick.Milliseconds()), 0)
	u := &a.updates
	if !u.tidied && u.status == updateIdle {
		// By the first tick the setup that installed this copy has exited: delete it.
		u.tidied = true
		if !strings.EqualFold(filepath.Dir(currentExecutable()), updatesFolder()) {
			os.RemoveAll(updatesFolder())
		}
	}
	switch {
	case u.status == updateReady:
		a.installUpdateWhenIdle()
	case !a.settings.AutoUpdate || u.status == updateChecking || u.status == updateDownloading || u.status == updateInstalling:
	case u.status == updateFailed && time.Since(u.failedAt) < updateRetry:
	case time.Since(time.Unix(a.settings.LastUpdateCheck, 0)) >= updateEvery:
		a.checkForUpdates()
	}
}

// background runs work off the UI thread; its result is applied on the UI thread.
func (a *application) background(work func() func()) {
	go func() {
		result := work()
		a.updates.results <- result
		procPostMessage.Call(a.window, messageUpdate, 0, 0)
	}()
}

// takeUpdateResults applies what the update thread finished (on messageUpdate).
func (a *application) takeUpdateResults() {
	for {
		select {
		case apply := <-a.updates.results:
			apply()
		default:
			return
		}
	}
}

func (a *application) checkForUpdates() {
	u := &a.updates
	if u.status == updateChecking || u.status == updateDownloading || u.status == updateInstalling {
		return
	}
	u.status, u.problem = updateChecking, ""
	a.flyout.refresh()
	feed, downloads := updateFeed()
	a.background(func() func() {
		var answer bytes.Buffer
		err := fetch(feed, "application/vnd.github+json", updateFeedLimit, &answer)
		var release update.Release
		if err == nil {
			release, err = update.Latest(answer.Bytes(), version, downloads)
		}
		return func() { a.checked(release, err) }
	})
}

func (a *application) checked(release update.Release, err error) {
	u := &a.updates
	switch {
	case errors.Is(err, update.ErrUpToDate):
		u.status = updateCurrent
		a.noteUpdateCheck()
		if u.requested {
			a.notifyUnlessShown("Circle Lens is up to date", "You have the latest version, "+version+".", "")
		}
		u.requested = false
	case err != nil:
		u.status, u.problem, u.failedAt = updateFailed, err.Error(), time.Now()
		debugLog("update check failed: %v", err)
		if u.requested {
			a.notifyUnlessShown("Couldn't check for updates", plainProblem(u.problem), "")
		}
		u.requested = false
	default:
		u.status, u.release = updateAvailable, release
		a.noteUpdateCheck()
		if a.settings.AutoUpdate {
			a.downloadUpdate()
		} else if u.requested {
			a.notifyUnlessShown("Circle Lens "+release.Version+" is available", "Click here to update. You have "+version+".", "update")
		}
	}
	a.flyout.refresh()
}

func (a *application) noteUpdateCheck() {
	a.settings.LastUpdateCheck = time.Now().Unix()
	a.save()
}

func (a *application) downloadUpdate() {
	u := &a.updates
	if u.status != updateAvailable && u.status != updateFailed {
		return
	}
	release := u.release
	u.status, u.problem = updateDownloading, ""
	a.flyout.refresh()
	a.background(func() func() {
		path, err := download(release)
		return func() {
			if err != nil {
				u.status, u.problem, u.failedAt = updateFailed, err.Error(), time.Now()
				debugLog("update download failed: %v", err)
				if u.requested {
					a.notifyUnlessShown("Couldn't update Circle Lens", plainProblem(u.problem), "")
				}
				u.requested = false
				a.flyout.refresh()
				return
			}
			u.status, u.setup = updateReady, path
			a.flyout.refresh()
			a.installUpdateWhenIdle()
		}
	})
}

// download fetches the setup into the updates folder and checks it; a file that doesn't match is
// deleted, never run.
func download(release update.Release) (string, error) {
	folder := updatesFolder()
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(folder, release.Setup.Name)
	partial := path + ".part"
	file, err := os.Create(partial)
	if err != nil {
		return "", err
	}
	err = fetch(release.Setup.URL, "application/octet-stream", release.Setup.Size, file)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = update.Verify(partial, release.Setup)
	}
	if err == nil {
		err = os.Rename(partial, path)
	}
	if err != nil {
		os.Remove(partial)
		return "", err
	}
	// The last check: the program itself says it is that version.
	if said, err := exec.Command(path, "-version").Output(); err != nil || strings.TrimSpace(string(said)) != release.Version {
		os.Remove(path)
		return "", fmt.Errorf("the downloaded setup is not version %s", release.Version)
	}
	return path, nil
}

// installUpdateWhenIdle installs a downloaded update now if nothing is in use, or tries again shortly.
// One the person asked for installs at once.
func (a *application) installUpdateWhenIdle() {
	u := &a.updates
	if u.status != updateReady {
		return
	}
	if !u.requested && (!a.settings.AutoUpdate || a.state.IsOpen() || a.capturer != nil || a.flyout.shown || a.recording) {
		if a.settings.AutoUpdate {
			procSetTimer.Call(a.window, timerUpdate, uintptr(updateInstallRetry.Milliseconds()), 0)
		}
		return
	}
	a.installUpdate()
}

// installUpdate starts the new setup, which closes this copy, installs itself and starts the new version.
func (a *application) installUpdate() {
	u := &a.updates
	if err := exec.Command(u.setup, "-install", "-update").Start(); err != nil {
		u.status, u.problem, u.failedAt = updateFailed, err.Error(), time.Now()
		a.flyout.refresh()
		return
	}
	u.status = updateInstalling
	a.flyout.closeNow()
}

// updateCommand is "Check for updates" and what it turns into: check, update now, or nothing while busy.
func (a *application) updateCommand() {
	u := &a.updates
	u.requested = true
	switch u.status {
	case updateAvailable:
		a.downloadUpdate()
	case updateReady:
		a.installUpdate()
	case updateIdle, updateCurrent:
		a.checkForUpdates()
	case updateFailed:
		if u.release.Version != "" && update.Newer(u.release.Version, version) {
			a.downloadUpdate()
		} else {
			a.checkForUpdates()
		}
	}
}

// updateLabel is what "Check for updates" says right now, and whether it can be clicked.
func (a *application) updateLabel() (string, bool) {
	u := &a.updates
	switch u.status {
	case updateChecking:
		return "Checking for updates…", false
	case updateCurrent:
		return "Up to date · Check again", true
	case updateAvailable:
		return "Update to " + u.release.Version, true
	case updateDownloading:
		return "Downloading " + u.release.Version + "…", false
	case updateReady:
		return "Restart to update to " + u.release.Version, true
	case updateInstalling:
		return "Updating…", false
	case updateFailed:
		return "Couldn't update · Try again", true
	}
	return "Check for updates", true
}

// plainProblem turns an error into something to show a person.
func plainProblem(problem string) string {
	switch {
	case strings.Contains(problem, "could not reach"), strings.Contains(problem, "no internet"):
		return "Couldn't reach GitHub. Check the internet connection, then try again."
	case strings.Contains(problem, "answered 403"), strings.Contains(problem, "answered 429"):
		return "GitHub is busy. Try again in an hour."
	}
	return "Something went wrong (" + problem + "). Try again later."
}

/* ---------- notifications ---------- */

// notify shows a tray notification; a click on it runs `action` ("update", or a web address to open).
func (a *application) notify(title, text, action string) {
	a.balloonAction = action
	data := a.trayData()
	data.Flags |= nifInfo
	data.InfoFlags = niifUser | niifLargeIcon
	accent := accentColor()
	large := makeIcon(int(math.Round(32*systemScale())), icon.Style{Ink: color.NRGBA{R: accent[0], G: accent[1], B: accent[2], A: 0xff}})
	data.BalloonIcon = large
	copy(data.InfoTitle[:len(data.InfoTitle)-1], wideSlice(title))
	copy(data.Info[:len(data.Info)-1], wideSlice(text))
	procShellNotifyIcon.Call(nimModify, uintptr(unsafe.Pointer(&data)))
	if large != 0 {
		procDestroyIcon.Call(large)
	}
}

// notifyUnlessShown notifies, unless the panel is open and already shows it.
func (a *application) notifyUnlessShown(title, text, action string) {
	if !a.flyout.shown {
		a.notify(title, text, action)
	}
}

// notifyUpdated says the update worked; a click opens what's new.
func (a *application) notifyUpdated() {
	a.notify("Circle Lens updated to "+version, "Click to see what's new.", "https://github.com/fauzulkc/circle-lens/releases/tag/v"+version)
}

func (a *application) balloonClicked() {
	action := a.balloonAction
	a.balloonAction = ""
	switch {
	case action == "update":
		a.updateCommand()
	case strings.HasPrefix(action, "https://"):
		procShellExecute.Call(0, uintptr(unsafe.Pointer(wide("open"))), uintptr(unsafe.Pointer(wide(action))), 0, 0, swShowNormal)
	}
}

/* ---------- WinINet ---------- */

// fetch downloads `address` into `sink`, refusing more than `limit` bytes or any answer but 200 OK.
// Redirects (GitHub serves release files from another host) are followed.
func fetch(address, accept string, limit int64, sink io.Writer) error {
	session, _, err := procInternetOpen.Call(uintptr(unsafe.Pointer(wide("CircleLens/"+version))), internetOpenTypePreconfig, 0, 0, 0)
	if session == 0 {
		return fmt.Errorf("no internet access (%v)", err)
	}
	defer procInternetCloseHandle.Call(session)
	headers := "Accept: " + accept + "\r\n"
	flags := uintptr(internetFlagReload | internetFlagNoCacheWrite | internetFlagNoCookies | internetFlagNoUI | internetFlagPragmaNoCache)
	request, _, err := procInternetOpenURL.Call(session, uintptr(unsafe.Pointer(wide(address))), uintptr(unsafe.Pointer(wide(headers))), uintptr(len(headers)), flags, 0)
	if request == 0 {
		return fmt.Errorf("could not reach the update server (%v)", err)
	}
	defer procInternetCloseHandle.Call(request)
	var status uint32
	length := uint32(unsafe.Sizeof(status))
	procHTTPQueryInfo.Call(request, httpQueryStatusCode|httpQueryFlagNumber, uintptr(unsafe.Pointer(&status)), uintptr(unsafe.Pointer(&length)), 0)
	if status != 200 {
		return fmt.Errorf("the update server answered %d", status)
	}
	buffer := make([]byte, 64<<10)
	var total int64
	for {
		var read uint32
		if ok, _, err := procInternetReadFile.Call(request, uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)), uintptr(unsafe.Pointer(&read))); ok == 0 {
			return fmt.Errorf("the download stopped (%v)", err)
		}
		if read == 0 {
			return nil
		}
		if total += int64(read); total > limit {
			return errors.New("the download is larger than expected")
		}
		if _, err := sink.Write(buffer[:read]); err != nil {
			return err
		}
	}
}

func currentExecutable() string {
	path, _ := os.Executable()
	return path
}
