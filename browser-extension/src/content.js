import { createCircleDetector } from '../../shared/circle-gesture.js';
import { createLens } from '../../shared/lens-view.js';
import { cleanSettings } from '../../shared/lens-settings.js';
import { createFastMotion, fastZoomLow } from '../../shared/lens-fast.js';
import lensCss from '../../shared/lens.css';

/**
 * The page side. Draw a circle twice with the mouse and a lens opens at the pointer, showing a screenshot
 * of the tab magnified. The screenshot comes from the background (`tabs.captureVisibleTab`), so canvases,
 * videos, iframes and cross-origin content all magnify exactly as they look. It is refreshed when the
 * page scrolls or resizes, or when something under the lens changes; between refreshes a stale shot slides
 * with the window's scroll. +/− (or Ctrl/⌘ + wheel) change the zoom; Esc or another double circle closes it.
 */

const api = globalThis.browser ?? globalThis.chrome;
const CAPTURE_GAP_MS = 600; // Chrome allows two captures a second.
const SCROLL_SETTLE_MS = 180;
const CHANGE_SETTLE_MS = 700;

if (window.top === window && !globalThis.__circleLensLoaded) {
  globalThis.__circleLensLoaded = true;
  start();
}

function start() {
  let settings = cleanSettings({});
  let detector = null;
  let lens = null;
  let host = null;
  let pointer = { x: innerWidth / 2, y: innerHeight / 2 };
  let capturedScroll = { x: 0, y: 0 };
  let lastCaptureAt = 0;
  let refreshing = false;
  let refreshAgain = false;
  let scrollTimer;
  let changeTimer;
  let stopWatching = () => {};
  const changed = new Set();

  /* ---------- the host: a closed shadow root, so page CSS never reaches the lens ---------- */
  const mountPoint = () => {
    if (host?.isConnected) return host.shadowMount;
    host = document.createElement('circle-lens');
    host.style.cssText = 'all: initial; position: fixed; left: 0; top: 0; width: 0; height: 0; z-index: 2147483647; pointer-events: none;';
    const shadow = host.attachShadow({ mode: 'closed' });
    try {
      const sheet = new CSSStyleSheet();
      sheet.replaceSync(lensCss);
      shadow.adoptedStyleSheets = [sheet];
    } catch {
      const style = document.createElement('style');
      style.textContent = lensCss;
      shadow.append(style);
    }
    host.shadowMount = shadow;
    document.documentElement.append(host);
    return shadow;
  };

  /* ---------- screenshots ---------- */
  const nextFrame = () => new Promise((resolve) => requestAnimationFrame(() => resolve()));
  const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

  /** A screenshot of the visible tab as a canvas the size of the viewport, or null. */
  const capture = async () => {
    const wait = lastCaptureAt + CAPTURE_GAP_MS - performance.now();
    if (wait > 0) await sleep(wait);
    // The lens must not photograph itself: hide it until the capture is taken.
    const hidden = Boolean(lens?.isOpen);
    if (hidden) {
      host.style.visibility = 'hidden';
      await nextFrame();
      await nextFrame();
    }
    const scroll = { x: scrollX, y: scrollY };
    const size = { width: innerWidth, height: innerHeight };
    let reply;
    try {
      reply = await api.runtime.sendMessage({ type: 'circle-lens:capture' });
    } catch {
      reply = null;
    } finally {
      lastCaptureAt = performance.now();
      if (hidden && host) host.style.visibility = '';
    }
    if (!reply?.url) return null;
    const canvas = document.createElement('canvas');
    try {
      const bitmap = await createImageBitmap(dataUrlToBlob(reply.url));
      canvas.width = bitmap.width;
      canvas.height = bitmap.height;
      canvas.getContext('2d').drawImage(bitmap, 0, 0);
      bitmap.close();
    } catch {
      return null;
    }
    return { canvas, scroll, size };
  };

  /** Re-shoot the page under the lens; calls that arrive mid-shot run once more afterwards. */
  const refresh = async () => {
    if (!lens?.isOpen) return;
    if (refreshing) {
      refreshAgain = true;
      return;
    }
    refreshing = true;
    const shot = await capture();
    refreshing = false;
    if (shot && lens?.isOpen) {
      capturedScroll = shot.scroll;
      lens.setContent(shot.canvas, shot.size);
      followScroll();
    }
    if (refreshAgain) {
      refreshAgain = false;
      refresh();
    }
  };

  const followScroll = () => lens?.shift(capturedScroll.x - scrollX, capturedScroll.y - scrollY);

  /* ---------- open and close ---------- */
  const open = async (x, y) => {
    if (lens) return;
    lens = createLens({ mount: mountPoint(), radius: settings.radius, zoom: settings.zoom });
    const shot = await capture();
    if (!shot || !lens) {
      lens = null;
      return;
    }
    capturedScroll = shot.scroll;
    fast.reset();
    lens.open(pointer.x ?? x, pointer.y ?? y, shot.canvas, shot.size);
    stopWatching = watchPage();
  };

  const close = async () => {
    if (!lens) return;
    const closing = lens;
    stopWatching();
    stopWatching = () => {};
    await closing.close();
    if (lens === closing) lens = null;
  };

  // Whatever opens or closes the lens starts the gap before a circle can count again.
  const toggle = (x, y) => {
    detector?.hold(performance.now() + settings.gap * 1000);
    return lens ? close() : open(x, y);
  };

  /* ---------- while open: follow the pointer, keys, and anything that makes the shot stale ---------- */
  const isEditable = (target) => target instanceof Element && (target.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(target.tagName));

  /** True when an element that changed sits under the part of the page the lens magnifies. */
  const underLens = (element) => {
    if (!(element instanceof Element) || element === host) return false;
    const reach = (settings.radius / (lens?.zoom ?? 1)) * 1.5;
    const box = element.getBoundingClientRect();
    return box.right >= pointer.x - reach && box.left <= pointer.x + reach && box.bottom >= pointer.y - reach && box.top <= pointer.y + reach;
  };

  const watchPage = () => {
    const onScroll = () => {
      followScroll();
      clearTimeout(scrollTimer);
      scrollTimer = setTimeout(refresh, SCROLL_SETTLE_MS);
    };
    const onResize = () => {
      clearTimeout(scrollTimer);
      scrollTimer = setTimeout(refresh, SCROLL_SETTLE_MS);
    };
    const noteChange = (element) => {
      if (changed.size < 64) changed.add(element);
      clearTimeout(changeTimer);
      changeTimer = setTimeout(() => {
        const relevant = [...changed].some(underLens);
        changed.clear();
        if (relevant) refresh();
      }, CHANGE_SETTLE_MS);
    };
    const observer = new MutationObserver((records) => {
      for (const record of records) {
        const element = record.target instanceof Element ? record.target : record.target.parentElement;
        if (element && element !== host) noteChange(element);
      }
    });
    observer.observe(document.documentElement, { subtree: true, childList: true, characterData: true, attributes: true });
    const onInput = (event) => noteChange(event.target);
    const onKey = (event) => {
      if (event.key === 'Escape') close();
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
    const onHidden = () => document.hidden && close();
    addEventListener('scroll', onScroll, { capture: true, passive: true });
    addEventListener('resize', onResize);
    addEventListener('input', onInput, true);
    addEventListener('keydown', onKey, true);
    addEventListener('wheel', onWheel, { capture: true, passive: false });
    document.addEventListener('visibilitychange', onHidden);
    return () => {
      clearTimeout(scrollTimer);
      clearTimeout(changeTimer);
      changed.clear();
      observer.disconnect();
      removeEventListener('scroll', onScroll, { capture: true });
      removeEventListener('resize', onResize);
      removeEventListener('input', onInput, true);
      removeEventListener('keydown', onKey, true);
      removeEventListener('wheel', onWheel, { capture: true });
      document.removeEventListener('visibilitychange', onHidden);
    };
  };

  /* ---------- zooming out while moving fast (lens-fast.js) ---------- */
  const fast = createFastMotion();
  let fastFrame = 0;
  const easeFast = () => {
    fastFrame = 0;
    if (!lens?.isOpen) return;
    const amount = settings.zoomOutWhenFast ? fast.advance(performance.now()) : 0;
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
  addEventListener('pointermove', onPointerMove, { capture: true, passive: true });

  const apply = (next) => {
    settings = cleanSettings(next);
    if (!settings.enabled) {
      detector = null;
      close();
    } else if (detector) {
      detector.setLoops(settings.loops);
      detector.setGap(settings.gap);
    } else detector = createCircleDetector({ loops: settings.loops, gap: settings.gap, onCircle: (x, y) => toggle(x, y) });
  };

  api.storage.sync.get(null).then(apply, () => apply({}));
  api.storage.onChanged.addListener((changes, area) => {
    if (area !== 'sync') return;
    api.storage.sync.get(null).then(apply);
  });

  // The toolbar button and keyboard shortcut open or close the lens at the pointer, gesture or not.
  api.runtime.onMessage.addListener((message) => {
    if (message?.type === 'circle-lens:toggle') toggle(pointer.x, pointer.y);
  });
}

function dataUrlToBlob(url) {
  const [head, body] = url.split(',', 2);
  const type = /data:([^;]+)/.exec(head)?.[1] ?? 'image/png';
  const bytes = Uint8Array.from(atob(body), (char) => char.charCodeAt(0));
  return new Blob([bytes], { type });
}
