/**
 * A live copy of the page for the lens to magnify, the way BoundStretch's reading lens does it: the body
 * is cloned into the lens, where the page's own stylesheets style it, so text stays sharp at any zoom and
 * no permission is needed (a web page cannot screenshot itself without asking to share the screen).
 *
 * The copy sits in a box the size of the viewport that scrolls along with the page:
 *
 *   scroller   the viewport: clips, carries the page background, and is scrolled to the page's scroll
 *              position, so sticky elements stick in the copy as they do on the page;
 *   frame      the initial containing block: the width of the page without its scroll bar and the
 *              height of the viewport, so absolutely placed elements land where they do on the page;
 *   <body>     the clone. It is a real BODY element, so `body > …` selectors still match it.
 *
 * Fixed elements stay put because the lens places the scroller inside a transformed box, which becomes
 * their containing block. Things a clone cannot carry are patched: form values, canvases (their current
 * picture), videos (their current frame), and iframes, embeds and audio (an empty box of the same size, so
 * the copy never loads or plays anything). Nested scroll positions are copied and follow the page.
 */

// Inherited properties the copy takes from <html>, so it does not inherit the lens's own font and colour.
const INHERITED = [
  'color', 'font-family', 'font-size', 'font-style', 'font-variant', 'font-weight', 'font-stretch', 'font-feature-settings',
  'font-variation-settings', 'font-kerning', 'line-height', 'letter-spacing', 'word-spacing', 'text-align', 'text-indent',
  'text-transform', 'text-rendering', 'text-shadow', 'white-space', 'word-break', 'overflow-wrap', 'hyphens', 'tab-size',
  'direction', 'writing-mode', 'color-scheme', 'list-style-type', 'list-style-position', 'quotes', '-webkit-font-smoothing',
];

const TRANSPARENT = /^(transparent|rgba\(0, 0, 0, 0\))$/;

/** Replays nothing in the copy: animations show their settled state and transitions do not run. */
export const PAGE_COPY_CSS = `
[data-circle-lens-page], [data-circle-lens-page] * {
  animation-delay: -60s !important;
  transition: none !important;
  pointer-events: none !important;
  caret-color: transparent !important;
  user-select: none !important;
}`;

/** @param {Document} doc */
export function createPageCopy(doc) {
  const view = doc.defaultView;
  const scroller = doc.createElement('div');
  scroller.setAttribute('data-circle-lens-page', '');
  scroller.setAttribute('aria-hidden', 'true');
  scroller.inert = true;
  const frame = doc.createElement('div');
  scroller.append(frame);
  // Page element → its copy, for following scroll positions inside the page.
  let twins = new WeakMap();

  const dress = () => {
    const root = view.getComputedStyle(doc.documentElement);
    const body = doc.body ? view.getComputedStyle(doc.body) : null;
    let background = root.backgroundColor;
    if (TRANSPARENT.test(background)) background = body && !TRANSPARENT.test(body.backgroundColor) ? body.backgroundColor : 'Canvas';
    scroller.style.cssText = 'position:absolute;left:0;top:0;width:100%;height:100%;overflow:hidden;margin:0;padding:0;border:0;';
    scroller.style.background = background;
    for (const name of INHERITED) {
      const value = root.getPropertyValue(name);
      if (value) scroller.style.setProperty(name, value);
    }
    frame.style.cssText = `position:relative;margin:0;padding:0;border:0;width:${doc.documentElement.clientWidth}px;height:${doc.documentElement.clientHeight}px;`;
  };

  const followScroll = () => {
    scroller.scrollLeft = view.scrollX;
    scroller.scrollTop = view.scrollY;
  };

  /** Clone the page again; the old copy is replaced in one step, so nothing flickers. */
  const copy = () => {
    const original = doc.body;
    if (!original) return;
    const clone = /** @type {HTMLElement} */ (original.cloneNode(true));
    twins = new WeakMap([[original, clone]]);
    const scrolled = [];
    const originals = original.querySelectorAll('*');
    const clones = clone.querySelectorAll('*');
    for (let i = 0; i < originals.length && i < clones.length; i++) {
      const from = originals[i];
      const to = clones[i];
      twins.set(from, to);
      if (from.scrollTop || from.scrollLeft) scrolled.push(from);
      patch(from, to);
    }
    dress();
    frame.replaceChildren(clone);
    for (const element of scrolled) mirror(element);
    followScroll();
  };

  /** Copy the scroll position of a scrolled element in the page to its copy. */
  const mirror = (element) => {
    const twin = twins.get(element);
    if (twin && twin !== element) {
      twin.scrollLeft = element.scrollLeft;
      twin.scrollTop = element.scrollTop;
    }
  };

  return {
    element: scroller,
    copy,
    followScroll,
    /** The page scrolled at `target` (the document, or an element that scrolls on its own). */
    scrolled(target) {
      if (!target || target === doc || target === doc.documentElement || target === doc.scrollingElement) followScroll();
      else mirror(target);
    },
    clear() {
      frame.replaceChildren();
      twins = new WeakMap();
    },
  };
}

/** Make `to` (a fresh clone of `from`) look like `from` does now. */
function patch(from, to) {
  switch (from.tagName) {
    case 'INPUT':
      if (from.type === 'checkbox' || from.type === 'radio') to.checked = from.checked;
      else if (from.type !== 'file' && from.type !== 'password') to.value = from.value;
      break;
    case 'TEXTAREA':
      to.value = from.value;
      break;
    case 'SELECT':
      for (let i = 0; i < from.options.length && i < to.options.length; i++) to.options[i].selected = from.options[i].selected;
      break;
    case 'CANVAS':
      try {
        to.getContext('2d')?.drawImage(from, 0, 0);
      } catch {
        // A canvas the page has taken a WebGL or bitmap context of may refuse; it stays blank.
      }
      break;
    case 'VIDEO':
      to.replaceWith(still(from));
      break;
    case 'IFRAME':
    case 'FRAME':
    case 'OBJECT':
    case 'EMBED':
    case 'AUDIO':
      to.replaceWith(placeholder(from));
      break;
  }
}

/** A canvas with the video's current frame (or its poster), in the video's place. */
function still(video) {
  const doc = video.ownerDocument;
  const canvas = doc.createElement('canvas');
  copyLook(video, canvas);
  const width = video.videoWidth || video.clientWidth;
  const height = video.videoHeight || video.clientHeight;
  canvas.width = Math.max(1, width);
  canvas.height = Math.max(1, height);
  canvas.style.objectFit = video.ownerDocument.defaultView.getComputedStyle(video).objectFit || 'contain';
  try {
    if (video.readyState >= 2) canvas.getContext('2d')?.drawImage(video, 0, 0, width, height);
    else if (video.poster) {
      const poster = doc.createElement('img');
      copyLook(video, poster);
      poster.src = video.poster;
      return poster;
    }
  } catch {
    // Nothing to draw yet.
  }
  return canvas;
}

/** An empty box the size of `element`, for content the copy must not load or play. */
function placeholder(element) {
  const box = element.ownerDocument.createElement('div');
  copyLook(element, box);
  const style = element.ownerDocument.defaultView.getComputedStyle(element);
  box.style.display = style.display === 'inline' ? 'inline-block' : style.display;
  box.style.background = 'color-mix(in srgb, currentColor 8%, transparent)';
  return box;
}

/** Same classes, inline style and size as `from`. */
function copyLook(from, to) {
  if (from.className && typeof from.className === 'string') to.className = from.className;
  const style = from.getAttribute('style');
  if (style) to.setAttribute('style', style);
  to.style.width = `${from.offsetWidth}px`;
  to.style.height = `${from.offsetHeight}px`;
}
