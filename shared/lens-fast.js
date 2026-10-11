/**
 * While the pointer moves fast at a high zoom, the lens zooms out, so what rushes past stays readable
 * and you can see where you are going; as the pointer slows it zooms back in. Scrolling under the lens
 * counts the same way: the faster the content scrolls past, the further out. Out is quick, back in is
 * gentler, like a camera settling. The Go twin is desktop/internal/lens/fast.go; keep the two in step.
 *
 * How far out is the same at every zoom: to half of it, but not below 1× (no magnification). So 4×
 * eases to 2×, 3× to 1.5×, and 1.8× to 1×. Speeds are CSS pixels per second.
 */

export const FAST_FROM = 600;
export const FAST_FULL = 2000;

const FAST_LOWEST = 1;
const FAST_OUT_MS = 80; // how quickly it zooms out as the pointer speeds up
const FAST_BACK_MS = 200; // how gently it zooms back in as it slows
const SPEED_SMOOTH_MS = 40; // smooths the measured speed, so one jumpy reading does not count
const SPEED_FADE_MS = 60; // once the pointer stops reporting, its speed fades out at this rate
const STILL_AFTER_MS = 40; // no pointer report for this long means it has stopped
// Scrolling: a burst after a pause is timed as if over SCROLL_FIRST_MS, so one notch on its own does
// not zoom out; the scroll speed is smoothed and fades out once the scrolling stops.
const SCROLL_FIRST_MS = 250;
const SCROLL_SMOOTH_MS = 80;
const SCROLL_FADE_MS = 100;
const SCROLL_STILL_MS = 100;
const SETTLED_AMOUNT = 0.01; // under this the zoom is within 0.02× of where it belongs: snap the rest
const SETTLED_SPEED = 20;

/** The zoom the lens eases out to while moving fast, for a set zoom of `zoom`. */
export const fastZoomLow = (zoom) => Math.min(zoom, Math.max(FAST_LOWEST, zoom / 2));

const smoothstep = (t) => {
  const x = Math.min(1, Math.max(0, t));
  return x * x * (3 - 2 * x);
};

/** Tracks the pointer's speed and how far the lens is zoomed out for it (0 to 1). */
export function createFastMotion() {
  let speed = 0;
  let scrollSpeed = 0;
  let lastScroll = null;
  let amount = 0;
  let last = null;
  let lastPointer = 0;
  let lastAdvance = null;

  return {
    /** Where the pointer is (CSS pixels) at `t` (ms). */
    pointer(t, x, y) {
      if (last) {
        const dt = t - last.t;
        if (dt < 1) return;
        const moved = Math.hypot(x - last.x, y - last.y);
        if (moved > 0 || dt < 200) speed += (moved / dt * 1000 - speed) * (1 - Math.exp(-dt / SPEED_SMOOTH_MS));
      }
      last = { x, y, t };
      lastPointer = t;
    },
    /** A scroll under the lens of `distance` CSS pixels (a wheel notch is about 100): faster, further out. */
    scrolled(t, distance) {
      let dt = lastScroll === null || t - lastScroll > SCROLL_FIRST_MS ? SCROLL_FIRST_MS : t - lastScroll;
      dt = Math.max(dt, 8);
      scrollSpeed += ((Math.abs(distance) / dt) * 1000 - scrollSpeed) * (1 - Math.exp(-dt / SCROLL_SMOOTH_MS));
      lastScroll = t;
    },
    /** Moves the zoom-out along to `t` and returns how far out it is (0 to 1). */
    advance(t) {
      if (lastAdvance === null) {
        lastAdvance = t;
        return amount;
      }
      const dt = t - lastAdvance;
      if (dt <= 0) return amount;
      lastAdvance = t;
      if (t - lastPointer > STILL_AFTER_MS) speed *= Math.exp(-dt / SPEED_FADE_MS);
      if (lastScroll === null || t - lastScroll > SCROLL_STILL_MS) scrollSpeed *= Math.exp(-dt / SCROLL_FADE_MS);
      const want = smoothstep((Math.max(speed, scrollSpeed) - FAST_FROM) / (FAST_FULL - FAST_FROM));
      const rate = want > amount ? FAST_OUT_MS : FAST_BACK_MS;
      amount += (want - amount) * (1 - Math.exp(-dt / rate));
      if (amount < SETTLED_AMOUNT && want === 0) amount = 0;
      return amount;
    },
    /** True once there is nothing left to ease. */
    get settled() {
      return amount === 0 && speed < SETTLED_SPEED && scrollSpeed < SETTLED_SPEED;
    },
    reset() {
      speed = 0;
      scrollSpeed = 0;
      lastScroll = null;
      amount = 0;
      last = null;
      lastAdvance = null;
    },
  };
}
