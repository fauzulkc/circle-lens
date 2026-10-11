# Changelog

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
