# Circle Lens

Draw a circle twice with the mouse and a magnifying lens opens at the pointer and follows it. `+`/`−` (or Ctrl + wheel) change the zoom; `Esc` or another double circle closes it.

This is the reading lens from BoundStretch (`src/components/Lens.tsx`, gesture in `src/lib/appearance.ts`) taken out on its own, in two forms:

| | Where it works | How it sees the screen | Size |
|---|---|---|---|
| [`browser-extension/`](browser-extension) | any web page in Chrome, Edge or Firefox | a screenshot of the tab (`tabs.captureVisibleTab`), re-shot on scroll, resize and changes under the lens | 19 KB script |
| [`desktop/`](desktop) | anywhere on the screen (Windows now; macOS and Linux next) | its own renderer: a live copy of the screen under the pointer, drawn at the display's refresh rate | 2.7 MB exe, no dependencies |

Both move like the original: the glass springs open from 55% while it fades in and unblurs, the content zooms from 1× to the zoom, the label slides in after it, zoom steps glide, and closing reverses it. Opening again while it is still closing turns it round from wherever it is. The desktop app draws every refresh: on a 120 Hz display, opening is 68–69 frames over 560 ms and closing 35–36 over 300 ms, the cold first open included.

## Use it

**Browser extension.** `npm install`, then `npm run build:extension`. Load `browser-extension/dist` as an unpacked extension: `chrome://extensions` (Developer mode → Load unpacked) or, in Firefox, `about:debugging` → This Firefox → Load Temporary Add-on → `manifest.json`. The toolbar button and `Alt+Shift+L` also open and close the lens. Its options page sets the circles needed, starting zoom and lens size.

**Desktop (Windows 10 2004 or later).** Run `npm run build:desktop`, which needs Go 1.22 or later and cross-compiles from any OS. Then start `desktop/dist/circle-lens.exe`. It lives in the tray:

- **Left click** opens a Windows 11-style flyout, with:
  - an on/off switch for watching for circles;
  - an "Open the lens" button;
  - pickers for circles to draw, starting zoom, lens size, and the wait between open and close;
  - "Start with Windows", and Quit.

  It follows light or dark mode and your accent colour, and works from the keyboard: Tab, the arrow keys, Enter or Space, and Esc.
- **Right click** opens the same settings as a native menu, which screen readers can read.
- **The icon** follows the taskbar's theme. Its glass fills with your accent colour while the lens is open, and it is dimmed and struck through while circle-watching is off.

`Ctrl+Alt+L` opens the lens without the gesture. While the lens is open, `Esc`, `Ctrl+=`/`Ctrl+−` and Ctrl + wheel belong to it, and they go back the moment it closes.

```
circle-lens.exe            start; if a copy is already running, this one replaces it (so a new version takes over)
circle-lens.exe -open      start with the lens open
circle-lens.exe -toggle    open or close the lens of the running copy (bind it to a mouse button)
circle-lens.exe -quit      quit the running copy
```

Settings live in `%APPDATA%\CircleLens\settings.json`.

When a copy is already running, the new start asks it to quit and takes over. A copy running as administrator can't hear a copy that isn't, so for that case the new one asks once and closes it through Windows' permission prompt. Copies from 0.2.1 on accept these messages either way.

## The gesture

The detector counts how far the pointer path turns. It fires when the path has turned the chosen number of times (1, 1½, 2, 2½ or 3; 95% of that is enough), and the last turn closes on itself, is round (its radius steady) and at least 64 px across. Three rules keep everyday mouse use from counting:
- A sharp reversal (over 100°) starts the count over.
- A pause over 250 ms starts it over too, because circles are drawn in one motion.
- The whole gesture must fit in 1.3 s per circle plus 0.4 s.

Two rules keep the lens from flipping open and shut:
- **A gap after every open or close.** It's 1.5 s by default, and you can set 1, 1.5, 2 or 3 s under "Wait between open and close". It applies however the lens was opened or closed, and a circle must be drawn wholly after it. `Esc` and the shortcuts still work at once.
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
| `CIRCLE_LENS_SHOW=flyout` or `menu` | opens one at start |

A test build can run beside the copy in daily use: `-ldflags "-X main.instance=CircleLensTest"` gives it its own single-instance lock and window classes.

## Layout

```
shared/                  the browser side's core, plain ESM
  circle-gesture.js      the circle detector (from BoundStretch)
  lens-view.js, lens.css the glass and its camera-zoom motion
  lens-settings.js       settings and their validation
browser-extension/       Manifest V3 for Chrome and Firefox; build.mjs bundles with esbuild
desktop/                 Go module, standard library only
  internal/gesture       the same detector in Go (keep the two in step), with the realism tests
  internal/lens          open/close/zoom state machine: every keyframe of lens.css
  internal/render        the software renderer (portable: every OS backend draws with it)
  internal/motion        the spring and exact CSS cubic-bezier curves
  internal/settings      settings file, validated on load
  internal/icon          the mark, drawn in code (tray states, and the extension PNGs via cmd/make-icons)
  cmd/circle-lens        the app: *_windows.go is the Windows backend
test/                    tests for shared/ (Go tests sit next to their packages)
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
- **Desktop:**
  - Before Windows 10 2004, Windows can't leave the lens out of captures, so it shows a still of the screen taken as it opens.
  - Fast-moving video under a still lens updates about 30 times a second.
  - `Ctrl+Alt+L` may already belong to another app; if so, use `-toggle`.
  - The lens can't appear over the UAC secure desktop or the lock screen.
