import { createCircleDetector } from '../../shared/circle-gesture.js';
import { createLens } from '../../shared/lens-view.js';
import { createFastMotion, fastZoomLow } from '../../shared/lens-fast.js';
import lensCss from '../../shared/lens.css';
import { createPageCopy, PAGE_COPY_CSS } from './page-copy.js';
import { cleanOptions } from './options.js';

export { cleanOptions, optionsFromAttributes } from './options.js';

/**
 * Circle Lens for a website: visitors draw a circle twice with the mouse and a magnifying lens opens at
 * the pointer, the same lens as the browser extension and the desktop app. Here the lens shows a live
 * copy of the page (page-copy.js) rather than a screenshot, so it needs no extension and no permission.
 *
 * One lens per page: calling `circleLens` again updates the running one and returns it, so a script tag
 * and a bundled copy never stack two lenses. Server-side (no `window`) it returns a lens that does
 * nothing, so it is safe to import anywhere.
 */

const MUTATION_SETTLE_MS = 350; // copy the page again once it has been quiet this long
const MUTATION_MAX_WAIT_MS = 1200; // or at least this often while it keeps changing
const CURRENT = Symbol.for('circle-lens.current');

/**
 * @param {import('../index').CircleLensOptions} [options]
 * @returns {import('../index').CircleLens}
 */
export function circleLens(options = {}) {
  if (typeof window === 'undefined' || typeof document === 'undefined') return inertLens();
  const running = window[CURRENT];
  if (running && !running.destroyed) {
    running.set(options);
    return running;
  }
  const lens = start(options);
  window[CURRENT] = lens;
  return lens;
}

function inertLens() {
  const nothing = () => {};
  return { open: nothing, close: () => Promise.resolve(), toggle: () => Promise.resolve(), set: nothing, destroy: nothing, isOpen: false, destroyed: false };
}

function start(initial) {
  let options = cleanOptions(initial);
  let detector = null;
  let lens = null;
  let host = null;
  let shadow = null;
  let page = null;
  let slot = null;
  let pointer = { x: innerWidth / 2, y: innerHeight / 2 };
  let stopWatching = () => {};
  let destroyed = false;
  let listeners = [];
  const callbacks = { onOpen: initial.onOpen, onClose: initial.onClose };

  /* ---------- the host: the lens in a closed shadow root, the page copy slotted into it ---------- */
  const mountPoint = () => {
    if (host?.isConnected) return shadow;
    host = document.createElement('circle-lens');
    host.style.cssText = 'all: initial !important; position: fixed !important; left: 0 !important; top: 0 !important; width: 0 !important; height: 0 !important; z-index: 2147483647 !important; pointer-events: none !important;';
    shadow = host.attachShadow({ mode: 'closed' });
    // Scaled text stays sharp: no layer kept at its 1× picture while it zooms.
    const css = `${lensCss}\n.lens, .lens-inner { will-change: auto; }\nslot { display: block; }`;
    try {
      const sheet = new CSSStyleSheet();
      sheet.replaceSync(css);
      shadow.adoptedStyleSheets = [sheet];
    } catch {
      const style = document.createElement('style');
      style.textContent = css;
      shadow.append(style);
    }
    // The copy is styled by the page's stylesheets, so it lives in the page (slotted into the lens),
    // with one rule of its own: nothing in it animates, transitions or takes the pointer.
    const style = document.createElement('style');
    style.textContent = PAGE_COPY_CSS;
    page = createPageCopy(document);
    host.append(style, page.element);
    document.documentElement.append(host);
    return shadow;
  };

  const viewport = () => ({ width: innerWidth, height: innerHeight });

  /* ---------- open and close ---------- */
  const open = (x = pointer.x, y = pointer.y) => {
    if (destroyed || lens?.isOpen || !document.body) return;
    if (lens) return lens.close().then(() => open(x, y)); // still closing: open once it has gone
    const mount = mountPoint();
    slot = document.createElement('slot');
    lens = createLens({ mount, radius: options.radius, zoom: options.zoom });
    fast.reset();
    lens.open(x, y, slot, viewport());
    // Copy once the slot shows the copy: scroll positions only take on an element that is laid out.
    page.copy();
    stopWatching = watchPage();
    callbacks.onOpen?.();
  };

  const close = async () => {
    if (!lens) return;
    const closing = lens;
    stopWatching();
    stopWatching = () => {};
    const wasOpen = closing.isOpen;
    await closing.close();
    if (lens === closing) {
      lens = null;
      page?.clear();
    }
    if (wasOpen) callbacks.onClose?.();
  };

  // Whatever opens or closes the lens starts the gap before a circle can count again.
  const toggle = (x, y) => {
    detector?.hold(performance.now() + options.gap * 1000);
    return lens?.isOpen ? close() : open(x, y);
  };

  /* ---------- while open: follow the pointer, scrolling, keys, and changes to the page ---------- */
  const isEditable = (target) => target instanceof Element && (target.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(target.tagName));

  const watchPage = () => {
    let settleTimer;
    let firstChange = 0;
    let resizeFrame = 0;
    const recopy = () => {
      firstChange = 0;
      if (lens?.isOpen) page.copy();
    };
    const observer = new MutationObserver(() => {
      const now = performance.now();
      if (!firstChange) firstChange = now;
      clearTimeout(settleTimer);
      settleTimer = setTimeout(recopy, now - firstChange > MUTATION_MAX_WAIT_MS ? 0 : MUTATION_SETTLE_MS);
    });
    observer.observe(document.body, { subtree: true, childList: true, characterData: true, attributes: true });
    observer.observe(document.documentElement, { attributes: true });
    const onScroll = (event) => page.scrolled(event.target);
    const onResize = () => {
      cancelAnimationFrame(resizeFrame);
      resizeFrame = requestAnimationFrame(() => {
        if (!lens?.isOpen) return;
        page.copy();
        lens.setContent(slot, viewport());
      });
    };
    const onKey = (event) => {
      if (!options.keys) return;
      if (event.key === 'Escape') toggle();
      else if (isEditable(event.target)) return;
      else if (event.key === '+' || event.key === '=') lens?.zoomBy(0.2);
      else if (event.key === '-' || event.key === '_') lens?.zoomBy(-0.2);
    };
    const onWheel = (event) => {
      if (!(event.ctrlKey || event.metaKey)) {
        // Scrolling under the lens: the faster, the further it zooms out (lens-fast.js).
        const unit = event.deltaMode === 1 ? 40 : event.deltaMode === 2 ? innerHeight : 1;
        fast.scrolled(event.timeStamp, Math.hypot(event.deltaX, event.deltaY) * unit);
        kickFast();
        return;
      }
      event.preventDefault();
      lens?.zoomBy(-Math.sign(event.deltaY) * 0.1);
    };
    addEventListener('scroll', onScroll, { capture: true, passive: true });
    addEventListener('resize', onResize);
    addEventListener('keydown', onKey, true);
    addEventListener('wheel', onWheel, { capture: true, passive: false });
    return () => {
      clearTimeout(settleTimer);
      cancelAnimationFrame(resizeFrame);
      observer.disconnect();
      removeEventListener('scroll', onScroll, { capture: true });
      removeEventListener('resize', onResize);
      removeEventListener('keydown', onKey, true);
      removeEventListener('wheel', onWheel, { capture: true });
    };
  };

  /* ---------- zooming out while moving fast (lens-fast.js) ---------- */
  const fast = createFastMotion();
  let fastFrame = 0;
  const easeFast = () => {
    fastFrame = 0;
    if (!lens?.isOpen) return;
    const amount = options.zoomOutWhenFast ? fast.advance(performance.now()) : 0;
    const target = lens.zoom;
    lens.setLiveZoom(target - (target - fastZoomLow(target)) * amount);
    if (!fast.settled) fastFrame = requestAnimationFrame(easeFast);
  };
  const kickFast = () => {
    if (!fastFrame) fastFrame = requestAnimationFrame(easeFast);
  };

  /* ---------- the gesture, always on while enabled ---------- */
  let moveFrame = 0;
  const onPointerMove = (event) => {
    pointer = { x: event.clientX, y: event.clientY };
    detector?.feed({ x: event.clientX, y: event.clientY, t: event.timeStamp, buttons: event.buttons, pointerType: event.pointerType });
    if (lens?.isOpen) {
      cancelAnimationFrame(moveFrame);
      moveFrame = requestAnimationFrame(() => lens?.move(pointer.x, pointer.y));
      fast.pointer(event.timeStamp, event.clientX, event.clientY);
      kickFast();
    }
  };
  const onHidden = () => document.hidden && close();
  addEventListener('pointermove', onPointerMove, { capture: true, passive: true });
  document.addEventListener('visibilitychange', onHidden);
  listeners = [
    () => removeEventListener('pointermove', onPointerMove, { capture: true }),
    () => document.removeEventListener('visibilitychange', onHidden),
  ];

  const apply = (next) => {
    const previous = options;
    options = cleanOptions({ ...previous, ...next });
    if ('onOpen' in next) callbacks.onOpen = next.onOpen;
    if ('onClose' in next) callbacks.onClose = next.onClose;
    if (!options.enabled) {
      detector = null;
    } else if (detector) {
      detector.setLoops(options.loops);
      detector.setGap(options.gap);
    } else detector = createCircleDetector({ loops: options.loops, gap: options.gap, onCircle: (x, y) => toggle(x, y) });
    if (lens?.isOpen && options.zoom !== previous.zoom) lens.setZoom(options.zoom);
    // A new size takes effect the next time the lens opens.
  };
  apply(initial);

  const controller = {
    /** Open the lens at (x, y), or at the pointer. */
    open: (x, y) => open(x, y),
    close,
    toggle: (x, y) => toggle(x, y),
    /** Change options; anything left out keeps its current value. */
    set: (next = {}) => apply(next),
    /** Close the lens, stop watching for the gesture and remove everything it added to the page. */
    destroy() {
      if (destroyed) return;
      destroyed = true;
      detector = null;
      stopWatching();
      lens?.close().then(() => host?.remove());
      if (!lens) host?.remove();
      for (const remove of listeners) remove();
      if (window[CURRENT] === controller) delete window[CURRENT];
    },
    get isOpen() {
      return Boolean(lens?.isOpen);
    },
    get destroyed() {
      return destroyed;
    },
    get options() {
      return { ...options };
    },
  };
  return controller;
}
