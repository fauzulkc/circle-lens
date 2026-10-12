# Plan: macOS and Linux

Status: in progress, from 2026-10-13.

**Direction.** Unofficial preview builds come first: unsigned, free, built by GitHub Actions, and downloadable from GitHub. Anything that costs money or needs an account or a licence (Apple's developer program, store listings, signing) is a to-do for later; see "Official, later" below. There is no iOS version for now.

## Previews

- **Tags.** A preview is a tag like `v1.3.0-preview.1`. The release workflow builds every platform and publishes it as a GitHub **pre-release**.
- **Windows users are unaffected.** Installed Windows copies, winget and the website's download links all read `releases/latest`, which never points at a pre-release.
- **Graduating.** Once a platform is good enough, its files simply ride along with each normal release.

| Platform | Files | How people run it |
|---|---|---|
| macOS 13 or later, Apple silicon and Intel | `CircleLens-<version>-mac.zip`: one universal `Circle Lens.app`, ad-hoc signed, not notarized | Unzip, move to Applications, open. macOS blocks the first open: System Settings → Privacy & Security → **Open Anyway**. Then allow Screen Recording (and Input Monitoring, for the Shift-twice shortcut). |
| Linux, X11 session | `CircleLens-<version>-linux-x86_64.tar.gz`, `…-aarch64.tar.gz` | Unpack, run `./circle-lens`. `./circle-lens -install` adds it to the app menu and start-up, under `~/.local`. |
| Linux, GNOME (Wayland or X11) | `circle-lens-gnome-<version>.zip` (phase 4) | `gnome-extensions install circle-lens-gnome-<version>.zip`, log out and in, then turn it on in Extensions |

An ad-hoc-signed Mac app gets a new code identity with each build. So after each update, macOS asks again for Screen Recording. Developer ID signing (official, later) fixes that.

## How the release workflow changes

1. **Build jobs run in parallel, each uploading workflow artifacts:**
   - `windows` (as today);
   - `extension`;
   - `linux` (`ubuntu-latest`; pure Go, so it cross-compiles both architectures);
   - `macos` (`macos-latest`; cgo, so it builds on a Mac, joined with `lipo`).
2. **One `publish` job** waits for them all. It makes the attestations and creates the release with every file at once, so a "latest" release is never missing a file.
3. **Channel jobs** run after `publish`: npm, winget and Chrome Web Store, as today. They're skipped for previews.

`ci.yml` gains:
- **A Linux job:** Go tests, plus an Xvfb run with a compositing manager that draws circles through XTEST and checks the frames written to `CIRCLE_LENS_FRAMES`. That's a virtual display, never a real person's.
- **A macOS job:** Go tests with cgo, and building the `.app`.

## Code

Shared Go stays shared: `internal/gesture`, `lens`, `motion`, `render`, `settings`, `shortcut`, `icon`, `update`. New files under `cmd/circle-lens`:

| File | Systems | What it does |
|---|---|---|
| `app_unix.go` | Linux, macOS | The app: flags, one copy at a time, the lens's open, close, zoom and frame loop, settings. Same behaviour as `app_windows.go`. |
| `capture_unix.go` | Linux, macOS | The ticker and the capturer from `capture_windows.go`, with the system calls behind a small interface |
| `label_unix.go` | Linux, macOS | The zoom pill's shape around text the system draws |
| `x11_linux.go` | Linux | A small X11 client on the wire protocol (standard library): Xauthority, SHAPE, XInput2, Composite, MIT-SHM-free `GetImage` |
| `desktop_linux.go` | Linux | Pointer and buttons from XInput2 raw events (they fire only while the mouse moves), the ARGB click-through lens window, capture |
| `text_linux.go` | Linux | Text through `golang.org/x/image` (pure Go) from installed fonts. Nothing is bundled. |
| `dbus_linux.go`, `tray_linux.go` | Linux | The tray icon and menu (StatusNotifierItem, dbusmenu), on a small D-Bus client |
| `desktop_darwin.go` with `.m` | macOS | cgo, macOS only: NSPanel lens (click-through, `sharingType = .none`), ScreenCaptureKit, NSEvent monitors, NSStatusItem menu, CoreText |

**Capture without the lens in it.**
- **Windows and macOS** leave the lens window out of captures, so the lens is live.
- **X11** can't do that. With a compositing manager running (GNOME, KDE, Cinnamon and Xfce all have one), each window's contents are kept offscreen, so the app reads the windows under the small area it needs (radius ÷ zoom), skipping its own lens. That keeps the lens live.
- **Without a compositing manager**, the lens is a still taken as it opens, and has hard edges.

**Rules that change for the preview:**
- **macOS uses cgo.** Windows and Linux stay cgo-free.
- **Linux adds one Go-team module,** `golang.org/x/image`, to draw text.

Both still have no runtime dependencies.

## Phases

1. **Linux X11 app:** lens, gesture, scroll and zoom, shortcuts, tray menu, `-install` (start-up and app menu).
2. **macOS app.**
3. **Release restructure and the first preview tag.**
4. **GNOME Shell extension** for GNOME on Wayland. It runs inside GNOME, so it has the pointer and a live lens from Clutter clones, and it reuses `shared/circle-gesture.js` as it is.
5. **KDE and wlroots on Wayland:** layer-shell lens, Screenshot portal snapshot, opened by shortcut.

## Official, later (costs money, needs an account or a licence)

- **Apple Developer Program ($99 a year):**
  - Developer ID signing and notarization for macOS, which removes the "Open Anyway" step and keeps permissions across updates;
  - automatic updates for the Mac app;
  - a Homebrew cask;
  - a Safari extension (Mac, later iPhone and iPad).
- **Windows code signing:** Azure Trusted Signing (about $10 a month) or SignPath Foundation (free for open source).
- **Chrome Web Store:** a $5 developer account. The listing is ready.
- **Firefox Add-ons (free account):** Firefox desktop and Android listing, with `data_collection_permissions: none` (required of new extensions since 2025-11-03).
- **Edge Add-ons:** Partner Center account.
- **extensions.gnome.org:** a free account for the GNOME extension.
- **Linux stores:** Flathub, Snap Store, and a `.deb` or apt repository.
- **iOS and iPadOS:** a Safari extension with touch support (the lens above the finger, pinch to zoom). Not now.
