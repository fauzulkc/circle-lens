# README images

The pictures on the README and in `docs/`. All of them show the real app or the real lens; none are mock-ups.

| File | What it is | Where it comes from |
|---|---|---|
| `banner.png`, `social-preview.png` | the banner (2560×1280) and the repository's social preview (1280×640, set in Settings → General → Social preview) | an HTML card in the website's look, with a frame of the demo video, captured with headless Chrome at 2× and 1× |
| `demo.gif` | the opening 8 seconds of the demo video: two circles open the lens, then it follows the pointer through a test log | the demo video (1440×1440, 60 fps), cut with ffmpeg to 640 px and 15 fps with a 256-colour palette |
| `read-*.jpg` | the lens on a prescription, a newspaper chart, a watchlist, a diagram, a map and a calendar | frames of the demo video, cropped around the lens to 800×500 |
| `windows-app.png`, `windows-tour.png` | the setup window and tray panel; the ready screen and the short tour | a test build's offscreen previews (`CIRCLE_LENS_PREVIEW=install,ready,guide,guide2,panel` with `CIRCLE_LENS_FRAMES`) at 200% scaling, on transparent backgrounds so they suit light and dark themes |

Remake them when the app's look or the demo changes, so the README never shows an older version than the one you'd download. Keep `demo.gif` under about 5 MB, so the README loads quickly.
