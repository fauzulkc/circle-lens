/**
 * Spots a circle drawn with the mouse, ported from BoundStretch's gesture detector (the circle half; the
 * flick half stays there). The Go twin is desktop/internal/gesture; keep the two in step.
 *
 * It watches the pointer path, resampled every 5 px, and adds up how far it turns. `loops` turns (90% is
 * enough) within the time window is a circle drawn that many times, provided the last turn closes on
 * itself, is round (its radius steady) and big enough. Three more things keep ordinary mouse use from
 * counting:
 *   - a sharp reversal (> 100°) starts the count over, so a side-to-side shake never counts;
 *   - so does a pause longer than 250 ms: circles are drawn in one motion, while reaching for things
 *     comes in moves with stops between them, which could otherwise add up;
 *   - the window is 1.3 s per circle plus 0.4 s, so turning has to be steady.
 * After a circle (or any open or close the page reports with `hold`) there is a gap, 1.5 s unless set
 * otherwise, in which nothing counts: a circle must be drawn wholly after it. And after a circle the
 * gesture is spent until the circling stops: a pause, or 0.6 s of movement that
 * turns less than a quarter turn. People keep spinning a moment past the point it opens; without this,
 * spinning on would close it again, and open it, and close it. Points with a button held (dragging,
 * selecting text) or from touch or pen reset it.
 *
 * It has no DOM or platform code: the browser extension feeds it `pointermove` events. Coordinates are
 * CSS pixels; times are milliseconds.
 */

const RESAMPLE_PX = 5;
const PAUSE_MS = 250;
const WINDOW_BASE_MS = 400;
const WINDOW_PER_LOOP_MS = 1300;
const SHARP_TURN = (100 * Math.PI) / 180;
const FULL_TURN = 2 * Math.PI * 0.9;
// How much of each circle must be drawn: nearly all of it.
const NEED_TURN = 0.95;
const MAX_POINTS = 600;
const MIN_SIZE = 64;
const MIN_LENGTH_PER_LOOP = 200;
// The circling has stopped once the last CALM_MS of movement turned less than CALM_TURN.
const CALM_MS = 600;
const CALM_TURN = Math.PI / 2;
// How uneven a drawn circle may be: the spread of its points' distances from their centre, relative
// to the mean distance.
const MAX_RADIUS_SPREAD = 0.3;

export const LOOPS_MIN = 1;
export const LOOPS_MAX = 3;
// The gap between an open and a close: at least as long as the animations, at most a few seconds.
export const GAP_MIN = 0.6;
export const GAP_MAX = 5;
export const DEFAULT_GAP = 1.5;

/** The signed change of heading at `b` going from `a` to `c`, in (-π, π]. */
function bend(a, b, c) {
  let turn = Math.atan2(c.y - b.y, c.x - b.x) - Math.atan2(b.y - a.y, b.x - a.x);
  while (turn > Math.PI) turn -= 2 * Math.PI;
  while (turn <= -Math.PI) turn += 2 * Math.PI;
  return turn;
}

/** Whether one turn of the path is round: its radius steady about its centre. */
function round(turn) {
  let cx = 0;
  let cy = 0;
  for (const p of turn) {
    cx += p.x;
    cy += p.y;
  }
  cx /= turn.length;
  cy /= turn.length;
  let sum = 0;
  let sumSquares = 0;
  for (const p of turn) {
    const r = Math.hypot(p.x - cx, p.y - cy);
    sum += r;
    sumSquares += r * r;
  }
  const mean = sum / turn.length;
  const spread = Math.sqrt(Math.max(0, sumSquares / turn.length - mean * mean));
  return mean > 0 && spread / mean <= MAX_RADIUS_SPREAD;
}

/**
 * @param {{ loops?: number, gap?: number, onCircle: (x: number, y: number) => void }} options
 * @returns {{ feed: (point: { x: number, y: number, t: number, buttons?: number, pointerType?: string }) => boolean, reset: () => void, setLoops: (loops: number) => void, setGap: (seconds: number) => void, hold: (until: number) => void }}
 */
export function createCircleDetector({ loops = 2, gap = DEFAULT_GAP, onCircle }) {
  let needed = clampLoops(loops);
  let gapMs = clampGap(gap) * 1000;
  let holdUntil = 0; // no circle counts that began before this time
  let points = [];
  let circleFrom = 0;
  let spent = false; // a circle was just found; nothing counts until the circling stops

  const reset = () => {
    points = [];
    circleFrom = 0;
  };

  /** Adds one pointer position; returns true when it completed a circle (and `onCircle` ran). */
  const feed = ({ x, y, t, buttons = 0, pointerType = 'mouse' }) => {
    if (pointerType !== 'mouse' || buttons) {
      reset();
      return false;
    }
    const last = points[points.length - 1];
    if (last) {
      if (Math.hypot(x - last.x, y - last.y) < RESAMPLE_PX) return false;
      if (t - last.t > PAUSE_MS) {
        reset(); // a pause: whatever comes next is a new motion
        spent = false;
      }
    }
    points.push({ x, y, t });
    if (spent) {
      if (!calm(t)) {
        if (points.length > MAX_POINTS) points.splice(0, points.length - MAX_POINTS);
        return false;
      }
      spent = false;
      points = [{ x, y, t }];
      circleFrom = 0;
      return false;
    }
    if (t < holdUntil) {
      // Within the gap: the count starts afresh here, so only circling after the gap counts.
      circleFrom = points.length - 1;
      return false;
    }
    if (points.length > MAX_POINTS) {
      const dropped = points.length - MAX_POINTS;
      points.splice(0, dropped);
      circleFrom = Math.max(0, circleFrom - dropped);
    }
    const n = points.length;
    if (n < 3) return false;
    if (Math.abs(bend(points[n - 3], points[n - 2], points[n - 1])) > SHARP_TURN) {
      // A sharp reversal ends any circle in progress; the next one starts at the turn.
      circleFrom = n - 2;
      return false;
    }

    // The circle in progress: everything since the last reversal, within the time window.
    const window = WINDOW_BASE_MS + WINDOW_PER_LOOP_MS * needed;
    while (circleFrom < n - 3 && t - points[circleFrom].t > window) circleFrom++;
    const loop = points.slice(circleFrom);
    const turns = [0, 0];
    let turn = 0;
    let length = 0;
    for (let i = 2; i < loop.length; i++) {
      turn += bend(loop[i - 2], loop[i - 1], loop[i]);
      length += Math.hypot(loop[i].x - loop[i - 1].x, loop[i].y - loop[i - 1].y);
      turns.push(turn);
    }
    if (Math.abs(turn) < needed * 2 * Math.PI * NEED_TURN || length < needed * MIN_LENGTH_PER_LOOP) return false;

    // The last full turn must close on itself and be round and big enough. Judging the last turn, not
    // the whole path, lets circles drift or change size from one turn to the next, as drawn ones do,
    // and lets half-turn settings (1½, 2½) work: the path's start is then across the circle.
    let lastTurn = loop.length - 1;
    while (lastTurn > 0 && Math.abs(turn - turns[lastTurn]) < FULL_TURN) lastTurn--;
    const recent = loop.slice(lastTurn);
    const xs = recent.map((point) => point.x);
    const ys = recent.map((point) => point.y);
    const width = Math.max(...xs) - Math.min(...xs);
    const height = Math.max(...ys) - Math.min(...ys);
    if (width < MIN_SIZE || height < MIN_SIZE) return false;
    const ratio = width / height;
    const closed = Math.hypot(x - loop[lastTurn].x, y - loop[lastTurn].y) <= 0.5 * Math.max(width, height);
    if (ratio <= 0.45 || ratio >= 2.2 || !closed || !round(recent)) return false;

    points = [{ x, y, t }];
    circleFrom = 0;
    spent = true;
    holdUntil = t + gapMs;
    onCircle((Math.max(...xs) + Math.min(...xs)) / 2, (Math.max(...ys) + Math.min(...ys)) / 2);
    return true;
  };

  /** Whether the circling has stopped: the last CALM_MS of movement turned less than a quarter turn. */
  const calm = (now) => {
    let first = points.length - 1;
    while (first > 0 && now - points[first - 1].t <= CALM_MS) first--;
    if (first === 0 || now - points[first].t < CALM_MS * 0.8) return false;
    let turn = 0;
    for (let i = first + 2; i < points.length; i++) turn += bend(points[i - 2], points[i - 1], points[i]);
    return Math.abs(turn) < CALM_TURN;
  };

  return {
    feed,
    reset,
    setLoops: (value) => (needed = clampLoops(value)),
    setGap: (seconds) => (gapMs = clampGap(seconds) * 1000),
    /** Start the gap for an open or close that came from elsewhere (pointer-event time, ms). */
    hold: (until) => (holdUntil = Math.max(holdUntil, until)),
  };
}

function clampGap(value) {
  const number = Number(value);
  if (!Number.isFinite(number)) return DEFAULT_GAP;
  return Math.min(GAP_MAX, Math.max(GAP_MIN, number));
}

function clampLoops(value) {
  const number = Number(value);
  if (!Number.isFinite(number)) return 2;
  return Math.min(LOOPS_MAX, Math.max(LOOPS_MIN, Math.round(number * 2) / 2));
}
