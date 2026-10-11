# Circle Lens

**A free screen magnifier that opens where you're looking.** Draw a circle twice with the mouse, or tap Shift twice, and a round magnifying glass opens at the pointer and follows it. For Windows, for your browser, and for your own website.

**[Download for Windows](https://github.com/fauzulkc/circle-lens/releases/latest/download/CircleLens-Setup.exe)** · [Browser extension](https://github.com/fauzulkc/circle-lens/releases/latest/download/circle-lens-extension.zip) · [Add it to your website](#add-it-to-your-website) · [Ask an AI to set it up](#set-it-up-with-an-ai-assistant) · MIT licensed

## Just want a magnifier for your computer?

On **Windows 10 or 11**:
1. [Download **CircleLens-Setup.exe**](https://github.com/fauzulkc/circle-lens/releases/latest/download/CircleLens-Setup.exe), the newest version, and open it.
2. If Windows says "Windows protected your PC", click **More info**, then **Run anyway**. The app is new and isn't signed yet.
3. Click **Yes** to install. It needs no administrator password and starts with Windows.
4. Draw a circle twice with the mouse, or tap **Shift** twice, and the lens opens. Ctrl + `+`/`−` zooms. Do the same again to close it.

Its settings are behind the lens icon (a small ring) next to the clock. To remove it: Settings → Apps → Circle Lens → Uninstall.

On a **Mac, Linux or Chromebook**, the [browser extension](#use-it) works in Chrome and Edge, on web pages. The app that magnifies anywhere on screen is Windows only for now, but your system has a magnifier built in; [llms.txt](llms.txt) says where to find it.

## Your antivirus flagged it?

Circle Lens is open source and isn't code-signed yet. Antivirus machine-learning checks sometimes flag a new, unsigned build as a "trojan" (for example Defender's `Trojan:Win32/Sabsik…!ml`), because it does things malware also does: it watches the mouse, copies part of the screen, installs itself for you, starts with Windows and updates itself. It never logs keys or sends anything anywhere ([PRIVACY.md](PRIVACY.md)).

**Known false positives:** Windows Defender flags two setup files: 1.2.0 (`Trojan:Win32/Sabsik.FL.A!ml`) and 1.2.1 (`Trojan:Win32/Wacatac.B!ml`). Don't use those two; get [the latest release](https://github.com/fauzulkc/circle-lens/releases/latest) instead. A copy installed from one of them updates itself to the latest, unless your antivirus removed it; then install the latest. Every other release scanned clean on 12 October 2026.

To check that the file you have is the real one:

- Its SHA-256 should match the one on the [release page](https://github.com/fauzulkc/circle-lens/releases/latest). In PowerShell: `Get-FileHash CircleLens-Setup.exe`.
- Every release file has a signed attestation showing it was built by this repository's release workflow: `gh attestation verify CircleLens-Setup.exe --repo fauzulkc/circle-lens`.

If yours flags it, please [report it privately](https://github.com/fauzulkc/circle-lens/security/advisories/new) with the detection name. False positives are reported to Microsoft and cleared, and a fresh build is released.

## Set it up with an AI assistant

Ask ChatGPT, Claude, Gemini or Copilot:

> Help me install Circle Lens, a screen magnifier, on my computer. Follow https://raw.githubusercontent.com/fauzulkc/circle-lens/main/llms.txt

To add it to a website, give your coding agent (Claude Code, Cursor, Copilot, Codex…):

> Add Circle Lens to this project. Follow https://raw.githubusercontent.com/fauzulkc/circle-lens/main/web/AI_INSTRUCTIONS.md

## How it works

Draw a circle twice with the mouse, or use the shortcut, and a magnifying lens opens at the pointer and follows it. `+`/`−` (or Ctrl + wheel) change the zoom; the shortcut or another double circle closes it. Move fast at a high zoom and the lens zooms out so you can see where you are going, then back in as you slow down.

This is the reading lens from BoundStretch (`src/components/Lens.tsx`, gesture in `src/lib/appearance.ts`) taken out on its own, in three forms:

| | Where it works | How it sees the screen | Size |
|---|---|---|---|
| [`browser-extension/`](browser-extension) | any web page in Chrome, Edge or Firefox | a screenshot of the tab (`tabs.captureVisibleTab`), re-shot on scroll, resize and changes under the lens | 19 KB script |
| [`desktop/`](desktop) | anywhere on the screen (Windows now; macOS and Linux next) | its own renderer: a live copy of the screen under the pointer, drawn at the display's refresh rate | 2.7 MB exe, no dependencies |
| [`web/`](web) | your own website, for every visitor (npm `circle-lens`, or one script tag) | a live copy of the page, styled by the page's own CSS, so text stays sharp at every zoom | 7 KB gzipped |

All three move like the original: the glass springs open from 55% while it fades in and unblurs, the content zooms from 1× to the zoom, the label slides in after it, zoom steps glide, and closing reverses it. Opening again while it is still closing turns it round from wherever it is. The desktop app draws every refresh: on a 120 Hz display, opening is 68–69 frames over 560 ms and closing 39–40 over 340 ms, the cold first open included.

## Install

`npm run package` builds all three and puts the files to hand out in `dist/`:

| File | What it is |
|---|---|
| `CircleLens-Setup-<version>.exe` | **The desktop app (Windows 10 2004 or later).** Run it from anywhere and its setup window offers to install it, for you only, with no administrator needed. You choose whether it starts with Windows and updates itself, and when it's done you can try it straight away. With a screen reader running, setup uses Windows' own dialogs instead, so it can be read. It goes into `%LOCALAPPDATA%\Programs\Circle Lens`, with a Start menu entry, Start with Windows, and an entry in Settings → Apps whose Uninstall removes it along with its settings. Running a newer one offers to update the installed copy and takes over from it while it runs. Choose *Just run it* to run it without installing; the tray menu can still install it later. Uninstalling from Settings → Apps uses the same window. Once installed, it **keeps itself up to date** from this repository's releases (see [Updates](#updates)). |
| `circle-lens-extension-<version>.zip` | **The browser extension**, in the form the Chrome Web Store, Edge Add-ons and Firefox Add-ons each take. |
| `circle-lens-<version>.min.js` | **The website script**, for sites that host it themselves. It is also on npm as [`circle-lens`](https://www.npmjs.com/package/circle-lens). |

For scripts, the exe also takes `-install` (install or update without asking) and `-uninstall -quiet`.

The exe isn't code-signed yet, so a downloaded copy gets Windows' "Windows protected your PC" screen the first time (More info → Run anyway). Signing it, with a certificate or through the Microsoft Store, removes that. Firefox only keeps unsigned extensions until restart; publishing through Firefox Add-ons signs it.

## Use it

**Browser extension.** `npm install`, then `npm run build:extension`. Load `browser-extension/dist` as an unpacked extension: `chrome://extensions` (Developer mode → Load unpacked) or, in Firefox, `about:debugging` → This Firefox → Load Temporary Add-on → `manifest.json`. The toolbar button and `Alt+Shift+L` also open and close the lens. Its options page sets the circles needed, starting zoom, lens size, the wait between open and close, and zooming out when moving fast. To change `Alt+Shift+L`, use `chrome://extensions/shortcuts` (in Firefox: Add-ons → ⚙ → Manage Extension Shortcuts).

**Desktop (Windows 10 2004 or later).** Install it as above, or run `npm run build:desktop`, which needs Go 1.22 or later and cross-compiles from any OS (the first build fetches `go-winres`, which embeds the app icon, version information and manifest). It lives in the tray:

- **Left click** opens a compact settings panel in the monochrome look of the project's website (near-black, soft neutral light, solid white buttons). It uses the best fonts your computer has (Inter, Geist and similar, else Segoe UI Variable), and bundles none. It has:
  - an on/off switch for watching for circles;
  - an "Open lens" button showing your shortcut;
  - one-line pickers for circles, zoom, size, and the wait between open and close;
  - the shortcut, which you can click to record your own;
  - the keys that work while the lens is open (zoom in, zoom out and close), each a keycap you can click to record another;
  - switches for zooming out when moving fast, the convex rim, starting with Windows, and updating automatically;
  - links to a short tour ("How to use"), this version's release notes ("What's new") and the website;
  - the version, with "Check for updates" beside it;
  - Quit.

  It works from the keyboard: Tab, the arrow keys, Enter or Space, and Esc.
- **Right click** opens the same settings as a native menu, which screen readers can read.
- **The icon** follows the taskbar's theme. While the lens is open, its glass fills in. It is dimmed and struck through while circle-watching is off.

**The shortcut** opens and closes the lens. It's **Shift twice** by default: tap Shift twice quickly, on its own, and capitals never count. The menus offer Ctrl twice, Ctrl+Alt+Z and Ctrl+Alt+L as presets, and you can record any combination or double tap from the panel or the menu.
- A combination is an ordinary Windows hotkey; if another app already has it, you're told and the old shortcut stays.
- A double tap needs a keyboard hook. It runs only while a double-tap shortcut is set, or while you're recording one. It looks only at which keys go up and down, and keeps and logs nothing.
- **Keys while the lens is open:**
  - Zoom in and out are `Ctrl+=`/`Ctrl+−` by default (and the keypad's `+`/`−`), plus Ctrl + wheel.
  - There's no close key unless you set one: Esc, or any combination you record. The shortcut always closes the lens too.
  - Change them in the panel or under "Keys while the lens is open" in the menu.
  - They're taken only while the lens is open and go back the moment it closes, so even plain Esc or a function key works.
  - A key that types something (a letter, digit or punctuation) needs Ctrl, Alt or Win, so the lens never takes what you type.
  - No two shortcuts can be the same, and one another app already has is refused.
  - While recording the close key, Esc sets Esc and Backspace sets none.

**The lens itself** has two optional touches, both on by default:
- **Zoom out when moving fast.** Quick movement eases the zoom out, to half the zoom at full speed but never below 1×, and back in as you slow down. Scrolling under the lens does the same, in proportion to how fast it scrolls:

  | Lens | A notch every 400 ms | every 90 ms | every 30 ms |
  |---|---|---|---|
  | 4× | 4× | 3.4× | 2× |
  | 3× | 3× | 2.55× | 1.5× |
  | 1.8× | 1.8× | 1.56× | 1× |
- **Convex rim.** A thin band at the glass's edge eases the zoom off, like the rim of a real lens.

```
circle-lens.exe            start; if a copy is already running, this one replaces it (so a new version takes over);
                           run from outside its install folder, it first offers to install
circle-lens.exe -install   install or update without asking
circle-lens.exe -uninstall uninstall (-quiet: without asking)
circle-lens.exe -open      start with the lens open
circle-lens.exe -toggle    open or close the lens of the running copy (bind it to a mouse button)
circle-lens.exe -quit      quit the running copy
```

Settings live in `%APPDATA%\CircleLens\settings.json`.

### Updates

The installed app keeps itself up to date from this repository's [releases](https://github.com/fauzulkc/circle-lens/releases):

- **Automatically,** by default. It checks GitHub about once a day; the first check comes a minute and a half after it starts. A newer version is downloaded and checked: it must come from this repository's releases, match the size and SHA-256 digest GitHub publishes for it, and report the expected version. Anything else is deleted unrun. The update then waits until the lens is closed and the settings panel isn't open, swaps itself in within a second or two, and a notification says it's done; click it to see what's new.
- **On demand.** "Check for updates", beside the version in the panel or in the right-click menu, checks right away. If there's a newer version, it offers "Update to …".
- **Off.** Turn off "Update automatically" in the panel or menu, and the app never connects on its own. "Check for updates" still works when you want it.

Updates need the installed copy. A copy run straight from a download offers to install instead. Version 0.5.0 and earlier don't update themselves, so install 0.6.0 by hand once.

When a copy is already running, the new start asks it to quit and takes over. A copy running as administrator can't hear a copy that isn't, so for that case the new one asks once and closes it through Windows' permission prompt. Copies from 0.2.1 on accept these messages either way.

## Add it to your website

The `web/` package puts the lens on your own site, for every visitor, with no extension needed. Paste one line into any page:

```html
<script src="https://cdn.jsdelivr.net/npm/circle-lens@0.5.0/dist/circle-lens.min.js" defer></script>
```

Or install it from npm:

```bash
npm install circle-lens
```

```js
import { circleLens } from 'circle-lens';

const lens = circleLens({ zoom: 2 });
```

A web page can't screenshot itself without asking to share the screen. So instead of a screenshot, the lens shows a live copy of the page, as BoundStretch's lens does: the page's own CSS styles it, so text stays sharp at every zoom. The copy follows scrolling, sticky and fixed elements, nested scroll boxes, form values and canvases. [web/README.md](web/README.md) has the options, the API and the limits; `web/demo/index.html` is a page to try it on after `npm run build:web`.

## The gesture

The detector counts how far the pointer path turns. It fires when the path has turned the chosen number of times (1, 1½, 2, 2½ or 3; 95% of that is enough), and the last turn closes on itself, is round (its radius steady) and at least 64 px across. Three rules keep everyday mouse use from counting:
- A sharp reversal (over 100°) starts the count over.
- A pause over 250 ms starts it over too, because circles are drawn in one motion.
- The whole gesture must fit in 1.3 s per circle plus 0.4 s.

Two rules keep the lens from flipping open and shut:
- **A gap after every open or close.** It's 1.5 s by default, and you can set 1, 1.5, 2 or 3 s under "Wait between open and close". It applies however the lens was opened or closed, and a circle must be drawn wholly after it. The shortcut still works at once.
- **A spent gesture.** After it fires, the gesture stays spent until the circling stops, through either a pause or 0.6 s of movement that turns less than a quarter turn. People keep spinning a moment past the point the lens opens, and without this, spinning on would close it, open it again, and so on.

`desktop/internal/gesture/realism_test.go` checks the detector against simulated hand-drawn circles that wobble, drift, aren't quite round and change speed, at three speed bands from slow to brisk. It also feeds in ordinary use: reading sweeps, curved reaches, idle wandering and hovering.

| Setting | Detected | Ordinary use that opened it |
|---|---|---|
| 1½ to 3 circles | 99.8–100% | none |
| 1 circle | 100% | about once per 16 minutes of busy mousing (marked "may open by accident" in the menu) |

## How lean the desktop app is

Measured on Windows 11, 4K at 200% scaling, 120 Hz:

| State | Memory | CPU |
|---|---|---|
| Idle, watching for circles | 13 MB | none while the mouse is still (a low-level hook on its own thread; no timers) |
| Lens open | about 37 MB private | only when something changes: moving the lens costs about 4 ms per frame, a still lens checks the screen 10 times a second |
| After closing | under 1 MB working set | the capture threads stop and the memory is handed back |

How it gets there:
- **No GPU stack.** The lens is drawn in software into one per-pixel-alpha window that Windows leaves out of screen captures, so no Direct3D is loaded.
- **Frames follow the display.** A ticker thread waits for each refresh (`DwmFlush`), and a separate capture thread copies the screen around the pointer, so a slow copy never costs a frame.
- **Most of each frame is cached.** The glass's border, shadows and highlight are drawn once and reused; each frame only resamples the magnified content (fixed-point bilinear, across up to 8 cores).
- **Unchanged frames are skipped.** Nothing is captured or redrawn when nothing moved.
- **The mouse never waits on the app.** The mouse hook has its own thread, because a low-level hook holds up every mouse event on the system until it returns.

## Diagnosing

| Variable | What it does |
|---|---|
| `CIRCLE_LENS_LOG=<file>` | writes a diagnostic log: open-animation frame rate and longest gap, frames per second while shown, Go memory |
| `CIRCLE_LENS_FRAMES=<folder>` | saves the first 80 frames the lens (or flyout) draws as PNGs. The lens is left out of screen captures, so this is how to see it |
| `CIRCLE_LENS_PROFILE=<file>` | writes a CPU profile for `go tool pprof` |
| `CIRCLE_LENS_THEME=light` or `dark` | overrides the app theme |
| `CIRCLE_LENS_SHOW=flyout`, `menu`, `record` or `update` | opens one at start (`record` starts recording a shortcut, `update` presses "Check for updates"); several can be given, comma-separated |
| `CIRCLE_LENS_UPDATE_FEED`, `CIRCLE_LENS_UPDATE_DOWNLOADS`, `CIRCLE_LENS_UPDATE_DELAY` | **test builds only:** a stand-in for GitHub's latest-release address, the address prefix its downloads must have, and how soon after starting to check (`5s`). The real app always uses GitHub |
| `CIRCLE_LENS_NETWORK_TESTS=1` | lets `go test ./cmd/circle-lens` fetch and verify the real latest release |

A test build can run beside the copy in daily use: `-ldflags "-X main.instance=CircleLensTest"` gives it its own single-instance lock, window classes and settings folder.

## Layout

```
shared/                  the browser side's core, plain ESM (used by the extension and web/)
  circle-gesture.js      the circle detector (from BoundStretch)
  lens-view.js, lens.css the glass and its camera-zoom motion
  lens-settings.js       settings and their validation
browser-extension/       Manifest V3 for Chrome and Firefox; build.mjs bundles with esbuild
web/                     the website package (npm `circle-lens`): src/page-copy.js copies the page into
                         the lens; build.mjs makes the ES module, CommonJS and script-tag builds
desktop/                 Go module, standard library only
  internal/gesture       the same detector in Go (keep the two in step), with the realism tests
  internal/lens          open/close/zoom state machine: every keyframe of lens.css
  internal/render        the software renderer (portable: every OS backend draws with it)
  internal/motion        the spring and exact CSS cubic-bezier curves
  internal/settings      settings file, validated on load
  internal/update        reads the latest-release answer and verifies downloads (the Windows side fetches)
  internal/icon          the mark, drawn in code (tray states, and the extension PNGs via cmd/make-icons)
  cmd/circle-lens        the app: *_windows.go is the Windows backend
scripts/                 release packaging, and the download numbers the stats workflow saves (RELEASING.md)
test/                    tests for shared/, web/ and scripts/ (Go tests sit next to their packages)
store/                   store listings: Chrome Web Store text and images, winget manifests
```

`npm test` runs both the JS and Go tests. `npm run icons` redraws the extension's icons (`go run ./cmd/make-icons -preview sheet.png …` also draws every tray state on light and dark taskbars).

## Platforms

The gesture, lens state machine, renderer, settings and icon are portable Go. A new OS needs only a backend that reads the pointer, copies the screen around it while leaving its own window out, puts a per-pixel-alpha window on top, and shows a tray or menu-bar icon:

- **macOS.** A borderless, click-through `NSPanel` excluded from capture (`sharingType = .none`), fed by ScreenCaptureKit with our window filtered out. Pointer from a `CGEventTap`, which needs the Accessibility and Screen Recording permissions.
- **Linux (X11).** An override-redirect ARGB window, the screen via XShm, and the pointer via XInput2.
- **Linux (Wayland).** Compositors don't let apps read the global pointer or draw overlays freely. The route there is the ScreenCast portal (PipeWire), plus a compositor-specific layer-shell surface, so expect a narrower feature set.

## Known limits

- **Extension:**
  - Browser pages (`chrome://`, the extension stores, the PDF viewer) don't allow content scripts.
  - The gesture isn't seen while the pointer is over a cross-origin iframe.
  - Re-shooting hides the lens for a frame or two, so a refresh blinks briefly.
  - Chrome allows two captures a second.
  - Video and animation under the lens update only when the page scrolls or changes near the lens.
- **Website script:**
  - Iframes, embeds and audio show as empty boxes in the lens, since copying them would load or play them again. Videos and canvases show what they had when the copy was made.
  - Custom elements are created again in the copy, so their setup code runs again.
  - Animations show in their finished state, and hover states don't show.
- **Desktop:**
  - Before Windows 10 2004, Windows can't leave the lens out of captures, so it shows a still of the screen taken as it opens.
  - Fast-moving video under a still lens updates about 30 times a second.
  - Shift twice clashes with apps that use it too (JetBrains IDEs, for one); pick another shortcut there. PowerToys' Find My Mouse uses Ctrl twice by default.
  - The lens can't appear over the UAC secure desktop or the lock screen.

## Contributing

Circle Lens collects no personal data; [PRIVACY.md](PRIVACY.md) has the details. It's open source under the [MIT License](LICENSE), and contributions are welcome. The biggest open piece is the macOS and Linux backends. [CONTRIBUTING.md](CONTRIBUTING.md) covers setup, the project's rules and how to send a change. Please also read the [code of conduct](CODE_OF_CONDUCT.md), and report security problems privately as [SECURITY.md](SECURITY.md) describes.
