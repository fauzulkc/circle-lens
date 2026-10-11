import { test } from 'node:test';
import assert from 'node:assert/strict';
import { fileURLToPath } from 'node:url';
import { build } from 'esbuild';
import { cleanOptions, optionsFromAttributes } from '../web/src/options.js';

test('script-tag attributes become options', () => {
  assert.deepEqual(optionsFromAttributes({ zoom: '2', radius: ' 180 ', loops: '1.5', gap: 'soon', zoomOutWhenFast: 'false', keys: '' }), {
    zoom: 2,
    radius: 180,
    loops: 1.5,
    zoomOutWhenFast: false,
    keys: true,
  });
  assert.deepEqual(optionsFromAttributes({}), {});
  assert.deepEqual(optionsFromAttributes({ enabled: 'nope', zoom: '' }), {});
});

test('options outside their range fall back or are clamped', () => {
  const options = cleanOptions({ zoom: 99, radius: 'big', loops: 2.3, keys: 'yes', surprise: true });
  assert.equal(options.zoom, 4);
  assert.equal(options.radius, 150);
  assert.equal(options.loops, 2.5);
  assert.equal(options.keys, true);
  assert.equal('surprise' in options, false);
  assert.equal(cleanOptions({ keys: false }).keys, false);
});

test('the package imports and runs without a browser (server-side rendering)', async () => {
  const entry = fileURLToPath(new URL('../web/src/index.js', import.meta.url));
  const { outputFiles } = await build({ entryPoints: [entry], bundle: true, format: 'esm', write: false, loader: { '.css': 'text' }, define: { __VERSION__: '"test"' } });
  const url = `data:text/javascript;base64,${Buffer.from(outputFiles[0].text).toString('base64')}`;
  const { circleLens } = await import(url);
  const lens = circleLens({ zoom: 2 });
  assert.equal(lens.isOpen, false);
  lens.open();
  await lens.close();
  lens.destroy();
});
