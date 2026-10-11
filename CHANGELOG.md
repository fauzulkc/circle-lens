# Changelog

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
