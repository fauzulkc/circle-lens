# Contributing to Circle Lens

Thanks for helping. Circle Lens is small on purpose: one gesture, one lens, as little memory and CPU as it can manage. Contributions that keep it that way are the easiest to take.

## Getting set up

You need **Node 22** and **Go 1.22 or later**.

```bash
npm install
npm test          # JS tests, then Go tests
npm run build     # the browser extension and the Windows app
npm run package   # the setup exe and the extension zip, in dist/
```

- **The desktop app** cross-compiles for Windows from any OS (standard-library Go, no cgo). Its first build fetches go-winres, which embeds the icon, version information and manifest.
- **On Windows,** run npm and go natively. If you run them from WSL against a Windows checkout, `node_modules` gets the wrong binaries.
- **To try the desktop app without disturbing an installed copy,** build with `-ldflags "-X main.instance=CircleLensTest"`. That build gets its own single-instance lock, settings folder and install location.
- **To try the extension,** load `browser-extension/dist` as an unpacked extension.

## Where to start

- **macOS and Linux backends** are the biggest open piece. The gesture, lens state machine, renderer, settings and icon are portable Go; a backend needs pointer input, a screen capture that leaves its own window out, a per-pixel-alpha overlay, and a tray or menu-bar icon. README.md ("Platforms") sketches each.
- Issues labelled `good first issue` and `help wanted`.
- Bugs with a clear way to reproduce them.

For anything large, open an issue first, so we can agree on the approach before you write it.

## The rules that keep it working

These are also in CLAUDE.md, for AI coding agents:

- **Lean is the point.** No runtime dependencies in the desktop app, and nothing running while the mouse is still. Measure before and after changes to the desktop app: `CIRCLE_LENS_LOG=<file>` logs frame rates and memory, and `go test -bench Compose -cpu 1,8 ./internal/render` times the renderer.
- **The two gesture detectors stay in step.** `shared/circle-gesture.js` and `desktop/internal/gesture` are one algorithm; change both, or neither. Run `go test -v ./internal/gesture` before and after: its realism tests report detection rates and accidental opens.
- **The lens motion matches across both tools.** It's in `shared/lens.css` and `shared/lens-view.js`, and `desktop/internal/lens` with `internal/motion`.
- **Privacy:** the keyboard hook must never log or keep keys or anything typed, not even in debug builds.
- **Settings are untrusted input:** they're cleaned on every load.
- **No personal data in the repo:** no paths, machine details, keys or secrets in files, commits or logs.

## Sending a change

1. **Fork, and branch from `main`.**
2. **Keep the change focused.** Add or update tests, especially for the gesture, motion and settings.
3. **Run `npm test` and `npm run build`.** CI runs both on Windows and Linux for every pull request.
4. **Use conventional commit messages** (`feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `chore:`).
5. **Open a pull request** that says what changed and why. For visible changes, include how you checked them: a screenshot, a frame from `CIRCLE_LENS_FRAMES`, or the fps line from the log.

By contributing, you agree that your contribution is licensed under the MIT License (see LICENSE).

## Releases

Maintainers bump the version in `package.json`, add a section to CHANGELOG.md, and push a tag `vX.Y.Z`. GitHub Actions builds the setup exe and the extension zip and publishes the release.
