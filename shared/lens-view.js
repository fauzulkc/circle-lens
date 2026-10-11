import { clampZoom } from './lens-settings.js';

const IN_MS = 560;
const OUT_MS = 300;
const ZOOM_MS = 260;
const reducedMotion = () => matchMedia('(prefers-reduced-motion: reduce)').matches;

/**
 * The glass, ported from BoundStretch's reading lens. A round window follows the pointer and shows `content`
 * magnified around it. `content` is any element (a screenshot canvas, a live video of the screen) drawn at
 * `size` CSS pixels from the viewport's top-left corner, so at 1× it lines up exactly with what is under it.
 *
 * Opening, closing and zoom steps move like a camera zoom: the glass springs open while the content inside
 * zooms from 1× up to the zoom; closing reverses it; zoom steps glide rather than jump. Placement never goes
 * through a framework render, only two transforms per pointer move.
 *
 * @param {{ mount: Element | ShadowRoot, radius?: number, zoom?: number, hint?: string }} options
 */
export function createLens({ mount, radius = 150, zoom = 1.8, hint = 'Esc' }) {
  const documentRef = mount.ownerDocument ?? mount;
  let element = null;
  let inner = null;
  let label = null;
  let content = null;
  let pointer = { x: 0, y: 0 };
  let offset = { x: 0, y: 0 };
  let target = clampZoom(zoom);
  // The zoom the content is drawn at right now (1 while opening or closing, else `target`).
  let shown = 1;
  let closing = null;
  let glideTimer;
  let introFrame = 0;

  /** Let the content glide to its next zoom over `ms` (then follow the pointer directly again). */
  const glide = (ms, easing) => {
    if (!inner || reducedMotion()) return;
    inner.style.transition = `transform ${ms}ms ${easing}`;
    clearTimeout(glideTimer);
    glideTimer = setTimeout(() => inner && (inner.style.transition = ''), ms);
  };

  const place = () => {
    if (!element) return;
    const { x, y } = pointer;
    element.style.transform = `translate(${x - radius}px, ${y - radius}px)`;
    inner.style.transform = `translate(${radius - x * shown}px, ${radius - y * shown}px) scale(${shown})`;
    if (content) content.style.transform = `translate(${offset.x}px, ${offset.y}px)`;
  };

  const showLabel = () => label && (label.textContent = `${target.toFixed(1)}× · ${hint}`);

  /** Put `next` behind the glass, `size` CSS pixels, replacing what was there. */
  const setContent = (next, size) => {
    content = next;
    offset = { x: 0, y: 0 };
    if (!inner) return;
    Object.assign(next.style, { width: `${size.width}px`, height: `${size.height}px` });
    inner.style.width = `${size.width}px`;
    inner.style.height = `${size.height}px`;
    inner.replaceChildren(next);
    place();
  };

  const open = (x, y, next, size) => {
    if (element) return;
    pointer = { x, y };
    element = documentRef.createElement('div');
    element.className = 'lens';
    element.setAttribute('aria-hidden', 'true');
    element.style.width = element.style.height = `${radius * 2}px`;
    const glass = documentRef.createElement('div');
    glass.className = 'lens-glass';
    inner = documentRef.createElement('div');
    inner.className = 'lens-inner';
    label = documentRef.createElement('span');
    label.className = 'lens-zoom';
    glass.append(inner);
    element.append(glass, label);
    showLabel();
    mount.append(element);
    // Open like a camera zoom: start at 1×, then glide up to the zoom on a spring.
    shown = reducedMotion() ? target : 1;
    setContent(next, size);
    const zoomIn = () => {
      glide(IN_MS, 'var(--spring)');
      shown = target;
      place();
    };
    introFrame = requestAnimationFrame(() => (introFrame = requestAnimationFrame(zoomIn)));
  };

  const move = (x, y) => {
    pointer = { x, y };
    place();
  };

  /** Shift the content by (dx, dy): a stale screenshot follows the page's scroll until it is replaced. */
  const shift = (dx, dy) => {
    offset = { x: dx, y: dy };
    place();
  };

  const setZoom = (next) => {
    target = clampZoom(next);
    showLabel();
    if (!element || closing) return;
    cancelAnimationFrame(introFrame);
    glide(ZOOM_MS, 'cubic-bezier(.2, .8, .2, 1)');
    shown = target;
    place();
  };

  /** Close with the reverse of the opening; resolves once the glass is gone. */
  const close = () => {
    if (!element) return Promise.resolve();
    if (closing) return closing;
    cancelAnimationFrame(introFrame);
    const finish = () => {
      clearTimeout(glideTimer);
      element?.remove();
      element = inner = label = content = null;
      closing = null;
    };
    if (reducedMotion()) {
      finish();
      return Promise.resolve();
    }
    element.classList.add('out');
    glide(OUT_MS, 'cubic-bezier(.4, 0, .7, .2)');
    shown = 1;
    place();
    closing = new Promise((resolve) => setTimeout(() => (finish(), resolve()), OUT_MS));
    return closing;
  };

  return {
    open,
    move,
    shift,
    setContent,
    setZoom,
    zoomBy: (step) => setZoom(target + step),
    close,
    get isOpen() {
      return Boolean(element) && !closing;
    },
    get zoom() {
      return target;
    },
    get element() {
      return element;
    },
  };
}
