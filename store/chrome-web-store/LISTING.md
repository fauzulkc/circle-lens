# Chrome Web Store listing

Everything the Chrome Web Store developer dashboard asks for, in the order its tabs ask, ready to paste. The same listing works for Microsoft Edge Add-ons, which takes the same zip.

## Before you start

1. **Register** at the [Chrome Web Store developer dashboard](https://chrome.google.com/webstore/devconsole). It costs a one-time $5 and needs a Google account with 2-step verification.
2. **Choose the contact email shown on the listing.** The store shows publishers' contact email publicly, so use an address you're happy to publish (Account → Contact email). It must be verified.
3. **Set the publisher name** to `Fauzul Chowdhury` (Account → Publisher name).
4. **Build the package.** Use the release zip `circle-lens-extension-<version>.zip` from 0.6.1 or later, since 0.6.1 drops the unneeded `activeTab` permission. Or build it locally with `npm run package`, which puts it in `dist/`.

## Package

**Items → New item → upload** `circle-lens-extension-<version>.zip`.

## Store listing tab

**Description** (plain text; the store doesn't render Markdown):

```
Circle Lens is a free magnifier for every web page. Draw a circle twice with the mouse, and a round magnifying lens opens right where you're looking. It follows your pointer, so you can read along a line of small print, a dense table or a busy chart without zooming the whole page.

HOW TO USE IT
• Draw a circle twice with the mouse, about the size of a coin, in one smooth motion. The lens opens at the pointer.
• Move the mouse and the lens follows.
• + and − (or Ctrl + mouse wheel) change the zoom, from 1.2× to 4×.
• Draw two more circles, or press Esc, to close it.
• The toolbar button and Alt+Shift+L also open and close it.

MADE TO STAY OUT OF YOUR WAY
• Ordinary mouse use doesn't open it. Dragging, selecting text and touch never count, and there's a short pause after it opens or closes.
• Move fast and it zooms out a little so you can see where you are going, then zooms back in as you slow down. Scrolling under it does the same.
• It opens with a smooth spring and closes the same way, and respects your system's reduced-motion setting.

YOUR SETTINGS
• How many circles open it (1 to 3).
• The starting zoom and the lens size.
• The wait between opening and closing.
• Zooming out while moving fast, on or off.

PRIVATE BY DESIGN
• No accounts, ads, analytics or tracking.
• The picture in the lens is taken from the tab you're viewing, only while the lens is open. It stays in memory and is never saved or sent anywhere.
• The extension makes no network requests.

FREE AND OPEN SOURCE
Circle Lens is MIT licensed. The source, a Windows app that magnifies anywhere on screen, and a script for adding the lens to your own website are at https://github.com/fauzulkc/circle-lens

Limits: browsers don't let extensions run on their own pages (settings, the Web Store, the built-in PDF viewer), so the lens doesn't open there.
```

| Field | Value |
|---|---|
| Category | Accessibility |
| Language | English |
| Store icon (128×128) | `browser-extension/icons/icon-128.png` |
| Screenshots (1280×800) | `screenshot-1-fine-print.png`, `screenshot-2-numbers.png`, `screenshot-3-settings.png` |
| Small promo tile (440×280) | `promo-small-440x280.png` |
| Marquee promo tile (1400×560) | `promo-marquee-1400x560.png` |
| Official URL | none (needs a verified domain; leave unset) |
| Homepage URL | `https://github.com/fauzulkc/circle-lens` |
| Support URL | `https://github.com/fauzulkc/circle-lens/issues` |
| Mature content | No |

The images are in this folder. `node store/chrome-web-store/make-images.mjs` draws them again, after `npm run build`, with the real lens open on each page in `pages/`.

## Privacy practices tab

**Single purpose:**

```
Magnify the part of the web page under the mouse pointer: drawing a circle twice opens a round magnifying lens that follows the pointer.
```

**Permission justifications:**

| Permission | Justification |
|---|---|
| `storage` | Saves the user's settings: how many circles open the lens, the starting zoom, the lens size, the wait between opening and closing, and whether it zooms out while moving fast. |
| Host permission `<all_urls>` | The lens has to work on any page the user reads, and opens from a mouse gesture rather than a click on the extension. The content script watches pointer movement on each page to recognise that gesture and draws the lens. The background uses tabs.captureVisibleTab to take the picture shown magnified in the lens, only while the lens is open, only for the visible tab, and only when that tab's top frame asks. Nothing is stored or sent. |

**Remote code:** No, I am not using remote code. All code is in the package.

**Data usage.** Tick none of the data types: the extension collects none. Then tick all three certifications:
- I do not sell or transfer user data to third parties, outside of the approved use cases.
- I do not use or transfer user data for purposes that are unrelated to my item's single purpose.
- I do not use or transfer user data to determine creditworthiness or for lending purposes.

**Privacy policy URL:** `https://github.com/fauzulkc/circle-lens/blob/main/PRIVACY.md`

## Distribution tab

| Field | Value |
|---|---|
| Payments | Free of charge |
| Visibility | Public |
| Distribution | All regions |

## After it's published

- Point the README's "Browser extension" link, `llms.txt` and the marketing site at the store page, and replace the "Load unpacked" steps for non-technical users with one-click install.
- Later versions go to the store by themselves: once the `CWS_*` secrets are set ([RELEASING.md](../../RELEASING.md#chrome-web-store)), each release uploads its zip and submits it for review. Until then, upload it by hand: **Package → Upload new package**.
