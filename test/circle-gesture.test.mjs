import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createCircleDetector } from '../shared/circle-gesture.js';

/** Pointer samples every 16 ms around a circle (or ellipse) drawn `loops` times over `durationMs`. */
function circlePath({ cx = 400, cy = 300, rx = 80, ry = rx, loops = 2, durationMs = 1600, start = 1000 }) {
  const points = [];
  const steps = Math.round(durationMs / 16);
  for (let i = 0; i <= steps; i++) {
    const angle = (i / steps) * loops * 2 * Math.PI;
    points.push({ x: cx + rx * Math.cos(angle), y: cy + ry * Math.sin(angle), t: start + i * 16 });
  }
  return points;
}

function run(points, options = {}) {
  const hits = [];
  const detector = createCircleDetector({ loops: 2, ...options, onCircle: (x, y) => hits.push({ x, y }) });
  for (const point of points) detector.feed(point);
  return hits;
}

test('two circles open the lens near their centre', () => {
  const hits = run(circlePath({}));
  assert.equal(hits.length, 1);
  assert.ok(Math.abs(hits[0].x - 400) < 10 && Math.abs(hits[0].y - 300) < 10, JSON.stringify(hits[0]));
});

test('counter-clockwise circles count too', () => {
  const hits = run(circlePath({}).map((point) => ({ ...point, y: 600 - point.y })));
  assert.equal(hits.length, 1);
});

test('one circle is not enough when two are needed, but is when one is', () => {
  assert.equal(run(circlePath({ loops: 1.05, durationMs: 800 })).length, 0);
  assert.equal(run(circlePath({ loops: 1.05, durationMs: 800 }), { loops: 1 }).length, 1);
});

test('a straight line and a side-to-side shake never count', () => {
  const line = Array.from({ length: 120 }, (_, i) => ({ x: 100 + i * 6, y: 200, t: 1000 + i * 16 }));
  const shake = Array.from({ length: 120 }, (_, i) => ({ x: 300 + (i % 2 ? 120 : 0), y: 200 + i, t: 1000 + i * 16 }));
  assert.equal(run(line).length, 0);
  assert.equal(run(shake).length, 0);
});

test('circles drawn too slowly fall out of the time window', () => {
  assert.equal(run(circlePath({ durationMs: 6000 })).length, 0);
});

test('half-turn settings work: 1½ and 2½ circles', () => {
  assert.equal(run(circlePath({ loops: 1.6, durationMs: 1200 }), { loops: 1.5 }).length, 1);
  assert.equal(run(circlePath({ loops: 2.6, durationMs: 2000 }), { loops: 2.5 }).length, 1);
});

test('a pause starts the gesture over', () => {
  const path = circlePath({});
  const half = path.length / 2;
  const paused = path.map((point, i) => (i >= half ? { ...point, t: point.t + 400 } : point));
  assert.equal(run(paused).length, 0);
});

test('tiny circles and flat ellipses are ignored', () => {
  assert.equal(run(circlePath({ rx: 15 })).length, 0);
  assert.equal(run(circlePath({ rx: 200, ry: 40 })).length, 0);
});

test('a held button or a touch pointer resets the gesture', () => {
  assert.equal(run(circlePath({}).map((point) => ({ ...point, buttons: 1 }))).length, 0);
  assert.equal(run(circlePath({}).map((point) => ({ ...point, pointerType: 'touch' }))).length, 0);
});

test('the detector rests after a circle, then fires again', () => {
  const first = circlePath({});
  const end = first[first.length - 1].t;
  const tooSoon = circlePath({ start: end + 16, durationMs: 1100 });
  const later = circlePath({ start: end + 3000 });
  assert.equal(run([...first, ...tooSoon]).length, 1);
  assert.equal(run([...first, ...later]).length, 2);
});

test('spinning on past the gesture does not toggle again', () => {
  assert.equal(run(circlePath({ loops: 6, durationMs: 4000 })).length, 1);
});

test('ordinary movement after a circle re-arms it, once the gap has passed', () => {
  const first = circlePath({});
  const end = first[first.length - 1].t;
  const away = Array.from({ length: 100 }, (_, i) => ({ x: 480 + (i + 1) * 8, y: 300, t: end + (i + 1) * 16 }));
  const second = circlePath({ cx: 1400, start: end + 101 * 16 });
  assert.equal(run([...first, ...away, ...second]).length, 2);
});

test('circles started within the gap do not count', () => {
  const first = circlePath({});
  const end = first[first.length - 1].t;
  assert.equal(run([...first, ...circlePath({ loops: 2.2, durationMs: 1000, start: end + 300 })]).length, 1);
});

test('hold starts the gap for an open from elsewhere', () => {
  const hits = [];
  const detector = createCircleDetector({ loops: 2, onCircle: () => hits.push(1) });
  detector.hold(1000 + 1500);
  for (const point of circlePath({ loops: 2.2, durationMs: 1200, start: 1000 })) detector.feed(point);
  assert.equal(hits.length, 0);
});

test('fast motion eases the zoom out and back (lens-fast)', async () => {
  const { createFastMotion, fastZoomLow } = await import('../shared/lens-fast.js');
  const fast = createFastMotion();
  let x = 0;
  let amount = 0;
  for (let t = 0; t <= 400; t += 8) {
    x += (3000 * 8) / 1000;
    fast.pointer(t, x, 0);
    amount = fast.advance(t);
  }
  assert.ok(amount > 0.9, `fast movement only reached ${amount}`);
  for (let t = 408; t <= 1600; t += 8) amount = fast.advance(t);
  assert.equal(amount, 0);
  assert.ok(fast.settled);
  assert.equal(fastZoomLow(4), 2);
  assert.equal(fastZoomLow(1.8), 1);
  assert.equal(fastZoomLow(1.2), 1);
});

test('faster scrolling zooms out further (lens-fast)', async () => {
  const { createFastMotion } = await import('../shared/lens-fast.js');
  const peak = (every, forMs) => {
    const fast = createFastMotion();
    let highest = 0;
    let next = 0;
    for (let t = 0; t <= forMs; t += 8) {
      if (t >= next) {
        fast.scrolled(t, 100);
        next += every;
      }
      highest = Math.max(highest, fast.advance(t));
    }
    return highest;
  };
  const slow = peak(400, 1600);
  const brisk = peak(90, 600);
  const quick = peak(30, 600);
  assert.ok(slow < 0.02, `a notch every 400 ms zoomed out by ${slow}`);
  assert.ok(brisk > 0.1 && quick > brisk + 0.15, `brisk ${brisk}, fast ${quick}`);
});
