# Circle Lens — working notes

Draw a circle twice with the mouse to open a magnifying lens at the pointer. It comes as a browser extension (`browser-extension/`, built on `shared/`) and a native desktop app (`desktop/`, Go). The feature started as BoundStretch's reading lens. See README.md for use, layout and limits.

- **Lives on D:.** The repo is `D:\code\circle-lens` (`/mnt/d/code/circle-lens` from WSL) because C: is nearly full. Keep caches on D: as well: Go uses `GOCACHE=D:\code\circle-lens\.cache\go-build`, and npm uses `npm_config_cache=D:\code\circle-lens\.cache\npm`. `.cache/` is ignored.
- **Toolchain.** Run npm, node and go through Windows PowerShell, so `node_modules` gets Windows binaries. Go 1.22 or later, standard library only, no cgo.
- **Lean is the point.** The desktop app has no runtime dependencies, and nothing runs while the mouse is still. It uses no GPU stack: `internal/render` draws the lens in software, and its capture threads exist only while the lens is shown. Measure before and after any change to the desktop app, using `CIRCLE_LENS_LOG` (open-animation fps, longest gap, memory) and the `render` benchmarks (`go test -bench Compose -cpu 1,8`).
- **Frames follow the display.** `capture_windows.go` has a ticker (`DwmFlush`, once per refresh) and a capturer, on separate threads; the UI thread composes. Never put a capture or anything slow on the ticker. A frame draws at the pointer's current position over the newest capture.
- **Keep the two detectors in step.** `shared/circle-gesture.js` and `desktop/internal/gesture` are the same algorithm with the same constants: turn count, last-turn closure and roundness, the 250 ms pause reset, the window of 1.3 s per circle plus 0.4 s, the 64 px minimum, 95% of each turn, the gap after any open or close (`gap` setting, 0.6-5 s, default 1.5; `Hold`/`hold` for opens from elsewhere), and being spent after firing until the circling stops. Change both, or neither. Before and after any change, run `realism_test.go` (`go test -v ./internal/gesture`): it reports detection rates and accidental opens.
- **Motion matches BoundStretch.** The JS lens motion is in `shared/lens.css` and `shared/lens-view.js`; `desktop/internal/lens` reproduces it keyframe by keyframe, using the exact CSS curves from `internal/motion`. The values:
  - opening is a 560 ms spring from 55% scale, with opacity and blur done by 60%, and the label 140 ms late;
  - closing takes 340 ms: the glass shrinks first, fully opaque, then fades and blurs out from 45% of the way, ending at 30% scale;
  - zoom glides take 260 ms, with zoom from 1.2× to 4×.
- **Settings are untrusted.** They go through `cleanSettings` (JS) or `settings.Parse` (Go) on every load.
- **Windows backend rules:**
  - The low-level mouse hook runs on its own locked thread (`hook_windows.go`) and only posts to the UI thread. Never do slow work in it, because it delays every mouse event on the system.
  - The lens window is left out of screen captures (`WDA_EXCLUDEFROMCAPTURE`). That is what lets the capture see behind it, and also why screenshots never show it: use `CIRCLE_LENS_FRAMES`.
  - Tray: left click opens the flyout (`flyout_windows.go`, compact, always dark, Linear-like palette), right click the native menu (`tray_windows.go`). Keep the two offering the same settings.
  - The keyboard hook (`keyboard_windows.go`) runs only for a double-tap shortcut or while recording. **Never log keys or anything typed from it, not even in debug builds**: its log once captured real typing. Test it through `onKeyAt` (`keyboard_windows_test.go`), never with injected keys while someone is at the machine.
  - Esc is not claimed globally; the shortcut (`internal/shortcut`, default Shift twice) opens and closes the lens.
  - Test builds take `-X main.instance=CircleLensTest`, so they run beside the copy in daily use (which may be elevated and so beyond `-quit`), with their own settings folder.
  - System DLLs load from System32 by full path.
  - Global keys other than `Ctrl+Alt+L` are registered only while the lens is open.
- **New platforms** add `*_darwin.go` / `*_linux.go` backends under `cmd/circle-lens` and reuse `internal/*`.
- **Installing.** The exe installs itself (`install_windows.go`): there is no installer framework. Per-user, under `%LOCALAPPDATA%\Programs`, with a Start menu shortcut made through COM (`link_windows.go`), an Uninstall entry under HKCU, and the start-up entry. Everything it creates is named after `instance`, so test builds (`-X main.instance=CircleLensTest`) install as "Circle Lens (Test)" and never touch the real copy. When testing, never use PowerShell's `Start-Process -Wait`: it waits for the installed copy the installer starts as well.
- **Releases.** `npm run package` builds both tools and writes `dist/CircleLens-Setup-<version>.exe` and `dist/circle-lens-extension-<version>.zip`. `desktop/build.mjs` runs go-winres first (icon, version information, manifest from `cmd/circle-lens/winres`). Keep the Go module cache on D: (`GOMODCACHE=D:\code\circle-lens\.cache\gomod`).
- **Checks.** Run `npm test` and `npm run build` before committing. Conventional commit types. Bump the root `package.json` version, which the manifest and the exe both take.
