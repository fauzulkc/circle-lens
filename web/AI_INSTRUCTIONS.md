# Adding Circle Lens to a project: instructions for AI coding agents

You are adding **Circle Lens** to the project you're working in. Once it's added, a visitor draws a circle twice with the mouse and a magnifying lens opens at the pointer and follows it. `+`/`−` or Ctrl + wheel change the zoom, and Esc or another double circle closes it. The package is `circle-lens` on npm: 7 KB gzipped, no dependencies, TypeScript types included. It works with any framework.

Follow these steps in order. Keep the change small: one install and one call to `circleLens()` in one place.

## 1. Find out what the project is

Read the project before changing anything:

- `package.json`: the framework (`next`, `react`, `vue`, `nuxt`, `svelte`, `@sveltejs/kit`, `astro`, `@angular/core`, `@remix-run/*`, `react-router`, `solid-js`, …) and whether there is a bundler at all.
- The lockfile, to pick the package manager: `package-lock.json` → npm, `pnpm-lock.yaml` → pnpm, `yarn.lock` → yarn, `bun.lockb` or `bun.lock` → bun.
- With no `package.json`, it is a static site, a CMS theme or a server-rendered app (WordPress, Shopify, Hugo, Jekyll, Django, Rails, Laravel, …). Use the script tag (step 2B).

## 2. Add it

### A. Projects with a bundler: install from npm

```bash
npm install circle-lens        # or: pnpm add circle-lens / yarn add circle-lens / bun add circle-lens
```

Then call `circleLens()` once, on the client, where the app starts. Pick the matching case below and add only that.

**React with Vite, or any single-page app.** In the entry file (`src/main.tsx`, `src/main.jsx`, `src/index.tsx`), after the app mounts:

```ts
import { circleLens } from 'circle-lens';

circleLens();
```

**Vue with Vite.** In `src/main.ts`, after `app.mount(...)`, the same two lines.

**Next.js, App Router.** Make a client component and render it once in the root layout:

```tsx
// app/circle-lens-loader.tsx
'use client';

import { useEffect } from 'react';
import { circleLens } from 'circle-lens';

export function CircleLensLoader() {
  useEffect(() => {
    const lens = circleLens();
    return () => lens.destroy();
  }, []);
  return null;
}
```

```tsx
// app/layout.tsx: inside <body>, next to {children}
import { CircleLensLoader } from './circle-lens-loader';
// …
<CircleLensLoader />
```

If the project keeps components in `src/app`, `components/` or similar, put the file where its other components live, and match the project's file extension (`.jsx` for JavaScript projects).

**Next.js, Pages Router.** In `pages/_app.tsx`, add the same `useEffect` inside the `App` component.

**Remix, or React Router 7 in framework mode.** In `app/root.tsx`, add the same `useEffect` inside the default-exported `App` component.

**Nuxt 3 or 4.** Create a client-only plugin; Nuxt loads it by itself:

```ts
// plugins/circle-lens.client.ts
import { circleLens } from 'circle-lens';

export default defineNuxtPlugin(() => {
  circleLens();
});
```

**SvelteKit.** In `src/routes/+layout.svelte`, add this to the existing `<script>` block; keep the rest of the layout as it is:

```svelte
<script>
  import { onMount } from 'svelte';
  import { circleLens } from 'circle-lens';

  onMount(() => {
    const lens = circleLens();
    return () => lens.destroy();
  });
</script>
```

**Svelte with Vite (no SvelteKit).** In `src/main.ts`, after the app mounts: `import { circleLens } from 'circle-lens'; circleLens();`

**Astro.** In the base layout every page uses (often `src/layouts/Layout.astro`), before `</body>`:

```astro
<script>
  import { circleLens } from 'circle-lens';
  circleLens();
</script>
```

**Angular.** In `src/main.ts`, after the app bootstraps:

```ts
import { circleLens } from 'circle-lens';

bootstrapApplication(AppComponent, appConfig)
  .then(() => circleLens())
  .catch((error) => console.error(error));
```

Keep the project's existing bootstrap call and its error handling. Only add `.then(() => circleLens())` and the import.

**Anything else with a bundler.** Call `circleLens()` once in code that runs only in the browser, when the app starts. If that code can unmount, call `lens.destroy()` on the returned lens when it does.

### B. Projects without a bundler: one script tag

First look up the current version, then pin it in the URL. Don't use an unpinned URL.

```bash
npm view circle-lens version
```

Add the tag before `</body>` in the template every page uses (base layout, theme footer, `index.html`):

```html
<script src="https://cdn.jsdelivr.net/npm/circle-lens@0.5.0/dist/circle-lens.min.js" defer></script>
```

Replace `0.5.0` with the version you looked up. The script starts the lens by itself.

**WordPress.** Enqueue it from the theme's `functions.php` (or a site plugin) instead of editing templates:

```php
add_action('wp_enqueue_scripts', function () {
    wp_enqueue_script('circle-lens', 'https://cdn.jsdelivr.net/npm/circle-lens@0.5.0/dist/circle-lens.min.js', [], null, ['strategy' => 'defer', 'in_footer' => true]);
});
```

**Shopify.** In `layout/theme.liquid`, before `</body>`.

## 3. Options: only when the user asks for them

The defaults suit most sites. Set options only if the user asked for something specific.

| Option | Default | Range | What it does |
|---|---|---|---|
| `zoom` | `1.8` | 1.2 to 4 | the zoom it opens at |
| `radius` | `150` | 100 to 300 | the glass radius in CSS pixels |
| `loops` | `2` | 1 to 3, in halves | circles to draw to open or close it |
| `gap` | `1.5` | 0.6 to 5 | seconds after it opens or closes before a circle counts again |
| `zoomOutWhenFast` | `true` | | ease the zoom out while the pointer moves or the page scrolls fast |
| `keys` | `true` | | Esc closes it and `+`/`−` change the zoom while it's open |
| `enabled` | `true` | | watch for the circle gesture (`open()` and `toggle()` work either way) |
| `onOpen`, `onClose` | | | callbacks for when it opens, and once it has closed |

- **From npm,** pass an object: `circleLens({ zoom: 2, radius: 180 })`.
- **With the script tag,** use data attributes: `data-zoom="2" data-radius="180"`, plus `data-loops`, `data-gap`, `data-zoom-out-when-fast="false"`, `data-keys="false"` and `data-enabled="false"`.

**If the user asks for a button that opens the lens,** call `toggle` with the click position:

```ts
const lens = circleLens();
button.addEventListener('click', (event) => lens.toggle(event.clientX, event.clientY));
```

With the script tag, the running lens is `window.CircleLens.lens`.

The lens's other methods are `open(x?, y?)`, `close()` (returns a promise), `set(options)`, `destroy()` and `isOpen`.

## 4. Rules

- **One lens per page.** `circleLens()` is idempotent: a second call updates the running lens and returns it. Still, start it from one place only (the root, entry or layout), never from individual pages or components that render many times.
- **Client side only.** Importing it is safe during server-side rendering, where `circleLens()` returns a lens that does nothing. But it must be called from code that runs in the browser, as the cases above do.
- **Clean up where the framework unmounts.** Where the call sits in a component, call `lens.destroy()` in its cleanup. Entry files and client plugins don't need it.
- **Use the package; don't copy its source** into the project, and don't add both the npm package and the script tag.
- **No other changes are needed.** Leave the project's CSS, viewport meta tag, pinch-zoom and accessibility settings alone. The lens doesn't replace browser zoom; it's an extra aid, and it ignores touch and pen, so there is nothing to switch off on phones.
- **Content Security Policy.** If the site sends a `Content-Security-Policy`, the script tag needs `https://cdn.jsdelivr.net` in `script-src` (installing from npm avoids that). The lens also adds a small stylesheet and inline styles while it runs. If `style-src` has no `'unsafe-inline'`, open the lens on a page and check it looks right before finishing.

## 5. Check it works

1. Run the project's own checks: typecheck, lint and build, whichever it has.
2. Start the dev server. With a mouse, draw a circle twice, about the size of a coin and in one smooth motion, and the lens opens. Two more circles or Esc close it.
3. If you can run JavaScript in the page (browser automation, devtools), open it directly:
   ```js
   window[Symbol.for('circle-lens.current')].open(400, 300)
   ```
   With the script tag, `CircleLens.lens.open(400, 300)` does the same.
4. Tell the user what you changed and how to use it, in one or two sentences: draw a circle twice with the mouse to open it; `+`/`−` to zoom; Esc or two more circles to close it.

## More

- Package docs: https://github.com/fauzulkc/circle-lens/tree/main/web
- Source and issues: https://github.com/fauzulkc/circle-lens
