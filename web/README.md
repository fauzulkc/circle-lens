# circle-lens

A magnifying lens for your website. Visitors draw a circle twice with the mouse, and a round lens opens at the pointer and follows it. `+`/`−` (or Ctrl + wheel) change the zoom; Esc or another double circle closes it.

The lens shows a live copy of your page, styled by your own CSS, so text stays sharp at every zoom and nothing asks for permission. The script is 7 KB gzipped and has no dependencies. It works with any framework, or none.

## Add it with a script tag

```html
<script src="https://cdn.jsdelivr.net/npm/circle-lens@0.5.0/dist/circle-lens.min.js" defer></script>
```

That's all: the lens starts on its own. Options go in data attributes:

```html
<script src="https://cdn.jsdelivr.net/npm/circle-lens@0.5.0/dist/circle-lens.min.js" data-zoom="2" data-radius="180" defer></script>
```

It puts its API on `window.CircleLens`: `CircleLens.lens` is the running lens, so a button can call `CircleLens.lens.toggle()`. Add `data-manual` to load it without starting it, then call `CircleLens.circleLens({ … })` when you want it.

The same file is on unpkg (`https://unpkg.com/circle-lens@0.5.0/dist/circle-lens.min.js`), and each [GitHub release](https://github.com/fauzulkc/circle-lens/releases/latest) has it as `circle-lens-<version>.min.js` for hosting yourself.

## Add it from npm

```bash
npm install circle-lens
```

```js
import { circleLens } from 'circle-lens';

const lens = circleLens({ zoom: 2 });
```

It is safe to import during server-side rendering: without a `window` it returns a lens that does nothing. In React, start it in an effect:

```jsx
useEffect(() => {
  const lens = circleLens();
  return () => lens.destroy();
}, []);
```

## Add it with an AI coding agent

Give your agent (Claude Code, Cursor, Copilot, Codex…) this:

> Add Circle Lens to this project. Follow https://raw.githubusercontent.com/fauzulkc/circle-lens/main/web/AI_INSTRUCTIONS.md

The instructions cover finding the framework, installing it, and where the one call goes in Next.js, React, Vue, Nuxt, SvelteKit, Astro, Angular, Remix, WordPress, Shopify and plain HTML. They also cover how to check it works.

## Options

| Option | Default | |
|---|---|---|
| `zoom` | `1.8` | the zoom it opens at, 1.2 to 4 |
| `radius` | `150` | the glass radius in CSS pixels, 100 to 300 |
| `loops` | `2` | circles to draw to open or close it: 1 to 3, in halves |
| `gap` | `1.5` | seconds after it opens or closes before a circle counts again, 0.6 to 5 |
| `zoomOutWhenFast` | `true` | ease the zoom out while the pointer moves or the page scrolls fast, and back in as it slows |
| `keys` | `true` | Esc closes it and `+`/`−` change the zoom while it is open |
| `enabled` | `true` | watch for the circle gesture (`open()` and `toggle()` work either way) |
| `onOpen`, `onClose` | | called when it opens, and once it has closed |

As data attributes they are `data-zoom`, `data-radius`, `data-loops`, `data-gap`, `data-zoom-out-when-fast`, `data-keys` and `data-enabled`. Values out of range are clamped, and anything unreadable falls back to the default.

## The lens

| | |
|---|---|
| `lens.open(x?, y?)` | open it at a point in the viewport, or at the pointer |
| `lens.close()` | close it; resolves once it has gone |
| `lens.toggle(x?, y?)` | open or close it |
| `lens.set(options)` | change options; anything left out keeps its value |
| `lens.destroy()` | close it and remove everything it added to the page |
| `lens.isOpen` | whether it is open |

There is one lens per page. Calling `circleLens()` again updates the running lens and returns it, so a script tag and a bundled copy never stack two lenses.

## How it behaves

- **The gesture** comes from a mouse only. Touch and pen are ignored, and so is any movement with a button held, so dragging and selecting text never count. It's tuned so ordinary mouse use doesn't open it: at 2 circles, simulated hand-drawn circles open it 99.8–100% of the time, and none of the ordinary movement tested did.
- **Motion** follows `prefers-reduced-motion`: with it set, the lens appears and goes without animating.
- **It never takes the pointer.** The lens and its copy of the page ignore clicks and the keyboard, and screen readers skip them (`aria-hidden`, `inert`).
- **Changes show up.** While it is open the copy follows scrolling, including boxes that scroll on their own, and is refreshed when the page changes.
- **It costs nothing while closed** beyond one passive `pointermove` listener. The copy exists only while the lens is open.

## Limits

- **Embedded frames, audio and plugins** show as empty boxes, because copying them would load or play them again. Videos show the frame they were on when the copy was made, and canvases show their picture at that moment.
- **Custom elements are created again** inside the copy, so their setup code runs once more each time the copy refreshes.
- **Animations** show in their finished state in the copy, and hover states don't show.

## Also

Circle Lens is also a [browser extension and a Windows app](https://github.com/fauzulkc/circle-lens) that work on any page and anywhere on screen. MIT licensed.
