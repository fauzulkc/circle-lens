# How Circle Lens works

The lens, the gesture that opens it, why the desktop app stays light, how each platform is done, where the code lives, and the known limits.

- [Three forms of one lens](#three-forms-of-one-lens)
- [The gesture](#the-gesture)
- [How lean the desktop app is](#how-lean-the-desktop-app-is)
- [Platforms](#platforms)
- [Layout](#layout)
- [Known limits](#known-limits)

## Three forms of one lens

Draw a circle twice with the mouse, or use the shortcut, and a magnifying lens opens at the pointer and follows it. `+`/`−` (or Ctrl + wheel) change the zoom; the shortcut or another double circle closes it. Move fast at a high zoom and the lens zooms out so you can see where you are going, then back in as you slow down.

This is the reading lens from BoundStretch (`src/components/Lens.tsx`, gesture in `src/lib/appearance.ts`) taken out on its own, in three forms:

| | Where it works | How it sees the screen | Size |
|---|---|---|---|
| [`browser-extension/`](../browser-extension) | any web page in Chrome, Edge or Firefox | a screenshot of the tab (`tabs.captureVisibleTab`), re-shot on scroll, resize and changes under the lens | 19 KB script |
| [`desktop/`](../desktop) | anywhere on the screen (Windows; macOS and Linux in preview) | its own renderer: a live copy of the screen under the pointer, drawn at the display's refresh rate | 2.7 MB exe, no dependencies |
| [`web/`](../web) | your own website, for every visitor (npm `circle-lens`, or one script tag) | a live copy of the page, styled by the page's own CSS, so text stays sharp at every zoom | 7 KB gzipped |

All three move like the original: the glass springs open from 55% while it fades in and unblurs, the content zooms from 1× to the zoom, the label slides in after it, zoom steps glide, and closing reverses it. Opening again while it is still closing turns it round from wherever it is. The desktop app draws every refresh: on a 120 Hz display, opening is 68–69 frames over 560 ms and closing 39–40 over 340 ms, the cold first open included.

A web page can't screenshot itself without asking to share the screen. So instead of a screenshot, the website script shows a live copy of the page, as BoundStretch's lens does: the page's own CSS styles it, so text stays sharp at every zoom. The copy follows scrolling, sticky and fixed elements, nested scroll boxes, form values and canvases. [web/README.md](../web/README.md) has the options, the API and the limits.

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

## Platforms

The gesture, lens state machine, renderer, settings and icon are portable Go. Each OS adds a backend that reads the pointer, copies the screen around it while leaving the lens out, puts a per-pixel-alpha window on top, and shows a tray or menu-bar icon:

- **Windows.** A layered, click-through window left out of captures (`WDA_EXCLUDEFROMCAPTURE`), GDI capture, a low-level mouse hook.
- **macOS (preview, `*_darwin.go`).** A borderless, click-through `NSPanel`; ScreenCaptureKit with this app left out of the capture (Screen Recording permission); a global mouse monitor (no permission); a listen-only key tap for a double-tap shortcut (Input Monitoring) and Carbon hot keys for combinations (no permission); an `NSStatusItem` menu. This backend uses cgo; the others don't.
- **Linux, X11 (preview, `*_linux.go`).** An override-redirect ARGB window with an empty SHAPE input region (click-through); XInput2 raw events for the pointer, which arrive only while it moves; key and button grabs; a StatusNotifierItem tray with a dbusmenu menu. X11 can't leave a window out of a capture, so the capture reads the windows under the lens one by one, from their offscreen copies, skipping the lens. A compositor keeps those copies; without one, the app asks the X server to keep them while the lens is open (Composite's automatic redirection). X11 and D-Bus are spoken directly over their sockets, so it stays pure Go.
- **Linux, Wayland (planned).** Compositors don't let apps read the global pointer or draw overlays freely. GNOME will get a Shell extension (it runs inside the compositor, so the lens stays live); KDE and wlroots desktops a layer-shell lens on a Screenshot-portal still, opened by a shortcut.

The plan for the rest is in [PLATFORMS-PLAN.md](PLATFORMS-PLAN.md).

## Layout

```
shared/                  the browser side's core, plain ESM (used by the extension and web/)
  circle-gesture.js      the circle detector (from BoundStretch)
  lens-view.js, lens.css the glass and its camera-zoom motion
  lens-settings.js       settings and their validation
browser-extension/       Manifest V3 for Chrome and Firefox; build.mjs bundles with esbuild
web/                     the website package (npm `circle-lens`): src/page-copy.js copies the page into
                         the lens; build.mjs makes the ES module, CommonJS and script-tag builds
desktop/                 Go module: standard library, plus golang.org/x/image for text on Linux
  internal/gesture       the same detector in Go (keep the two in step), with the realism tests
  internal/lens          open/close/zoom state machine: every keyframe of lens.css
  internal/render        the software renderer (portable: every OS backend draws with it)
  internal/motion        the spring and exact CSS cubic-bezier curves
  internal/settings      settings file, validated on load
  internal/update        reads the latest-release answer and verifies downloads (the Windows side fetches)
  internal/icon          the mark, drawn in code (tray states, and the extension PNGs via cmd/make-icons)
  cmd/circle-lens        the app: *_windows.go is the Windows backend; *_unix.go the app on macOS
                         and Linux, over *_darwin.go (AppKit, cgo) and *_linux.go (X11 and D-Bus,
                         spoken directly, no cgo)
scripts/                 release packaging, and the download numbers the stats workflow saves (RELEASING.md)
test/                    tests for shared/, web/ and scripts/ (Go tests sit next to their packages)
store/                   store listings: Chrome Web Store text and images, winget manifests
docs/                    these pages, and the README's images (docs/media)
```

`npm test` runs both the JS and Go tests. `npm run icons` redraws the extension's icons (`go run ./cmd/make-icons -preview sheet.png …` also draws every tray state on light and dark taskbars).

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
- **macOS and Linux previews:**
  - They don't update themselves yet, and have no settings window: the menu has every setting except recording your own shortcut (pick a preset, or set `shortcut` in the settings file) or on/off key (set `onOffKey`).
  - macOS: not notarized, so the first open needs **Open Anyway**, and each new version asks for its permissions again. Ctrl + wheel zooms the lens but also reaches the app underneath.
  - Linux: X11 sessions only. Without a compositor the lens has a hard edge, with no shadow.
