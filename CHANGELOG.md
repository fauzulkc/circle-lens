# Changelog

## Unreleased

- **Off means off.** With the switch at the top of the panel (now "Circle Lens on") turned off, Shift twice and every other trigger stop too: the shortcut, "Open the lens" in the menu and `-toggle`. The shortcut used to keep working while circles were off. The lens closes if it was open.
- **An on/off key.** Record a key combination that turns Circle Lens on and off from anywhere, in the panel or under Shortcut in the menu. There's none by default. A notification says which way it went. On macOS and Linux, set `onOffKey` in the settings file.
- **Common shortcuts ask first.** Recording or picking a shortcut or on/off key that already does something common (Ctrl+C, Alt+F4, Win+L, F5, Ctrl twice for PowerToys' Find My Mouse, and others) asks whether to use it anyway, since a combination is taken from every app while Circle Lens runs.
- **Windows 10: the lens shows the whole screen again.** Where Windows won't leave the lens out of screen captures, the lens draws from one still taken as it opens. That still covered little more than the glass, so moving the pointer showed a blank rectangle ([#4](https://github.com/fauzulkc/circle-lens/issues/4)). It now covers the whole monitor. The same fix applies to the macOS and Linux previews.
- **Crashes leave a record.** If the app closes by itself, `%APPDATA%\CircleLens\crash.log` says why, to attach to a bug report. Nothing is written otherwise.
- **A check for capture exclusion.** The *Capture exclusion* workflow reports whether a Windows version leaves the lens window out of captures, which the live lens needs.

## 1.3.0-preview.2

- **Linux: the lens stays live without a compositor.** It used to take one still as it opened, so moving more than about 450 px away showed white. Now, while it's open, it asks the X server to keep windows offscreen (no compositor needed) and reads what's under it as it moves.
- Tested on a real X server through Xephyr: the circle gesture, Shift twice, zoom and the live lens all work.

## 1.3.0-preview.1

The first preview of the desktop app on **macOS** and **Linux**: unsigned builds to try, published as a GitHub pre-release. The Windows app is unchanged and doesn't see previews.

- **macOS 14 or later**, Apple silicon and Intel (`CircleLens-1.3.0-preview.1-mac.zip`): a live lens through ScreenCaptureKit, the circle gesture, Shift twice or a key combination, Ctrl + `+`/`−` and Ctrl + wheel to zoom, and a menu-bar menu with the same settings as on Windows, plus Start at login. It isn't notarized: the first open needs System Settings → Privacy & Security → **Open Anyway**.
- **Linux, X11 sessions**, x86_64 and ARM (`CircleLens-1.3.0-preview.1-linux-<arch>.tar.gz`): the same lens, live under a compositor, with a tray menu. `./circle-lens -install` adds it to the app menu and start-up, under `~/.local`.
- Not in the previews yet: updates, the setup and settings windows, and recording your own shortcut. Wayland isn't supported yet.

## 1.2.3

- **Updates no longer fail with "Couldn't update".** The setup asked the running copy to quit, then replaced it as soon as its window closed, while Windows still held the old file. Now it waits for the old copy to end, and tries again for a few seconds. If the old file is still in use, it moves it aside, which Windows allows.
- **Errors fit the window.** A failed install or update names the file that failed, not its full path, and long text wraps instead of running off the edges.

## 1.2.2

- **A fresh setup file, so downloads go through again.** Windows Defender's cloud check began flagging the 1.2.1 setup file too (`Trojan:Win32/Wacatac.B!ml`) about an hour after release, a false positive. Its code is unchanged from 1.2.1, which scans clean when built again. Defender judged the published file, not the program. Every release up to 1.1.0 scans clean.
- **Downloads are named with their version** on the website (`CircleLens-Setup-1.2.2.exe`), so a saved file says which release it is.
- **The setup file's details are shorter:** a description and the source address.
- **Keys and labels are monospace on every PC.** Without JetBrains Mono, Geist Mono or IBM Plex Mono installed, they fell back to the text font; now they use Windows' own Cascadia Mono or Consolas.

## 1.2.1

> **Don't use this setup file.** Windows Defender flags it as `Trojan:Win32/Wacatac.B!ml`, a false positive. Use 1.2.2 or later.

- **Downloads work again.** Windows Defender's machine-learning check flagged the 1.2.0 setup file as `Trojan:Win32/Sabsik.FL.A!ml`, a false positive that blocked downloads. 1.1.0, and the same code built elsewhere, scanned clean. This is a fresh build, with the program's symbols kept (stripped Go programs are scored as more suspicious).
- **The setup file says what it is:** its details (Properties → Details) describe it as an open-source screen magnifier and give the source and where to report a problem or a false antivirus detection.
- **Proof of origin:** every release file now has a signed attestation that it was built by this repository's release workflow. Check it with `gh attestation verify`.
- **README:** a "Your antivirus flagged it?" section on checking a file and reporting a detection.

## 1.2.0

> **Don't use this setup file.** Windows Defender flags it as `Trojan:Win32/Sabsik.FL.A!ml`, a false positive. Use 1.2.2 or later.

- **A new icon: a lens, not a search.** The tray icon, the panel's mark and the app icon are now a thin hollow ring with a curved glint of light inside, instead of a magnifying glass. The app icon puts it, large and fine, in near-black on a white tile.
- **"Circle twice. Read anything."** The setup window's headline (and the store's promo tiles) now say what opens the lens: two circles.

## 1.1.0

- **The website's monochrome look** for the setup window and the tray panel:
  - **Colour and text:** near-black with a soft neutral light, white headlines over a grey second line, no gradients or glows.
  - **Buttons:** solid white primary buttons with dark text and hairline secondary ones, with 10 px corners.
  - **Switches and chips:** light switches with dark knobs, grey selected chips, flat keycaps.
  - **Installing:** a white progress ring.
  - **Uninstall:** keeps its warning in soft red text rather than a red button.
- **The tray icon** fills its glass in light grey while the lens is open (dark on a light taskbar), instead of blue.
- **The app icon** follows suit: a near-black tile with a fine light edge, the white magnifier and sparkle, and the lens glass in blue, the one colour.

## 1.0.0

- **Circle Lens 1.0.** The Windows app, the browser extension and the website script are stable: they install, update and uninstall cleanly, and keep your settings.
- **A proper app icon:** the white magnifier on a rounded tile in the brand's blue, with a small sparkle. It reads at every size on light and dark backgrounds: the setup file, downloads, Start menu, taskbar and Settings → Apps. The extension's store and extensions-page icons use it too. Its toolbar icon stays a plain magnifier.

## 0.9.0

- **How to use, What's new, Website:** a four-page tour of the lens, using your own keys, and this version's release notes, both in the setup window's style. The panel's links row and the right-click menu open them, along with the project's website.
- **A lighter panel:** more room around the header, a calm flat "Open lens" button (it reads "Close lens" while the lens is open), small flat switches, borderless chips and smaller type. The setup window's switches match.
- **The tray icon** shows the lens open in the brand's blue, instead of following your Windows accent colour.
- **Better type, nothing bundled:** text uses fonts you may already have (Inter, Geist, Bricolage Grotesque, JetBrains Mono, IBM Plex Mono), falling back to Windows' Segoe UI Variable. The exe ships no fonts.

## 0.8.0

- **Choose the keys that work while the lens is open:**
  - **Zoom in and zoom out:** `Ctrl+=` and `Ctrl+−` by default. Click a keycap in the panel to record others, or use "Keys while the lens is open" in the menu.
  - **A close key:** none by default. Choose Esc, or record any combination.
  - **Rules:** the keys are taken only while the lens is open. A typing key on its own is refused, as are clashes with your other shortcuts and combinations another app already has.
- **Fixed:** a shortcut ending in the keypad's plus (`Ctrl+Num+`) couldn't be read back from the settings file.

## 0.7.0

- **A setup window** in the look of the project's website, instead of plain Windows dialogs. It installs or updates with a progress ring and a checklist of what it did, and lets you choose whether Circle Lens starts with Windows and updates itself. It ends with "Try it now". Uninstalling from Settings → Apps uses it too. With a screen reader running, setup keeps Windows' own dialogs, which it can read.
- **The settings panel has the website's look:** near-black with a soft blue light, a gradient pill to open the lens, the shortcut drawn as a keycap, and the website's switches.
- **Updates keep your start-up choice:** an update, from the app or winget, no longer turns "Start with Windows" back on.
- **A failed silent install** (`-install`) now exits with an error code, so winget and scripts can tell.

## 0.6.1

- **Every release reaches every channel by itself:**
  - the GitHub release, which installed apps update from;
  - npm, through trusted publishing, with no stored token;
  - winget, as a pull request with the new version;
  - the Chrome Web Store, uploaded and submitted for review.

  Each channel switches on with a one-time setup ([RELEASING.md](RELEASING.md)), and until then it's skipped without failing the release.
- **Stable download links:** each release also carries `CircleLens-Setup.exe` and `circle-lens-extension.zip`, so `…/releases/latest/download/CircleLens-Setup.exe` always gives the newest version. The README and llms.txt link straight to them.
- **Store listings, ready to submit:** the Chrome Web Store listing (text, privacy answers, screenshots and promo tiles, drawn from the real lens by `store/chrome-web-store/make-images.mjs`) and winget manifests. Releases submit new versions to winget once the package is there and a `WINGET_TOKEN` secret is set.
- **The extension asks for less:** it no longer requests `activeTab`, which its access to all sites already covers. Its description now says what it is: a free magnifier for any web page.
- **A privacy policy** ([PRIVACY.md](PRIVACY.md)) covering the extension, the Windows app and the website script.
- **Mac, Linux and Chromebook:** llms.txt and the README now point people on these systems to the browser extension, instead of saying Circle Lens doesn't run there.

## 0.6.0

- **The desktop app keeps itself up to date.** Once installed, it checks this repository's GitHub releases about once a day and installs a newer version when the lens is closed and the settings panel isn't open. A notification then says it's done; click it to see what's new.
- **Updates are checked before they run:** the download must come from this repository's releases, match the size and SHA-256 digest GitHub publishes for it, and report the expected version, or it's deleted unrun.
- **Your choice:** an "Update automatically" switch (on by default; off means the app never connects on its own), and "Check for updates" in the panel and the menu.
- Versions 0.5.0 and earlier don't update themselves, so install this one by hand once.
- The first-run notification names your shortcut, not Esc.

## 0.5.0

- **Circle Lens for websites.** The new `web/` package puts the lens on any site for every visitor: one script tag from jsDelivr or unpkg, or `npm install circle-lens`. The lens shows a live copy of the page, styled by the page's own CSS, so text stays sharp at every zoom and no permission is needed. It follows scrolling, sticky and fixed elements, nested scroll boxes, form values and canvases. It ships as an ES module, a CommonJS copy and a self-starting script (7 KB gzipped), with TypeScript types, and is safe to import during server-side rendering.
- **Releases** also carry `circle-lens-<version>.min.js`, and publish the package to npm.
- **Instructions for AI assistants:** `llms.txt` walks anyone who asks for a better magnifier through installing it, in plain words, and `web/AI_INSTRUCTIONS.md` tells coding agents how to add it to a project, framework by framework.

## 0.4.0

- **Installable on Windows.** The exe installs itself, per user and without admin: Start menu entry, Start with Windows, and an Uninstall entry in Settings → Apps. A newer one updates the installed copy and takes over from it while it runs. It also takes `-install` and `-uninstall [-quiet]`.
- **The exe identifies itself:** it carries its icon, version information and a Windows manifest.
- **Packaging:** `npm run package` writes the setup exe and the extension zip; tagged releases are built by GitHub Actions.
- **Open source** under the MIT License.

## 0.3.0

- **Shortcut:** one shortcut opens and closes the lens, Shift twice by default. Presets in the menus, or record your own. Esc is no longer taken from other apps.
- **Zoom out when moving fast:** fast pointer movement and scrolling ease the zoom out in proportion to speed, then back in.
- **Convex rim**, as a toggle.
- **Closing** draws the glass in before it fades.
- **Settings panel:** compact and dark, in the style of Linear.

## 0.2.x

- **Renderer:** the desktop lens is drawn by its own renderer at the display's refresh rate, with the original's open, close and zoom motion.
- **Gesture:** the detector is checked against simulated hand-drawn circles and ordinary mouse use; there's a gap after every open or close, and the gesture stays spent until the circling stops.
- **Tray:** a Windows 11-style flyout, a theme-aware tray icon, and a new start takes over from a running copy.

## 0.1.0

- **First release:** a browser extension and a Windows app, ported from BoundStretch's reading lens.
