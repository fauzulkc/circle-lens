import { test } from 'node:test';
import assert from 'node:assert/strict';
import { changes, downloadsBetween, kind, merge, parseCsv } from '../scripts/stats.mjs';

test('release assets are told apart by name', () => {
  assert.equal(kind('CircleLens-Setup.exe'), 'app');
  assert.equal(kind('circle-lens-extension.zip'), 'extension');
  assert.equal(kind('CircleLens-Setup-0.9.0.exe'), 'update');
  assert.equal(kind('circle-lens-extension-0.9.0.zip'), 'extension-versioned');
  assert.equal(kind('circle-lens-0.9.0.min.js'), 'script');
  assert.equal(kind('CircleLens-1.3.0-preview.1-mac.zip'), 'mac');
  assert.equal(kind('CircleLens-1.3.0-preview.1-linux-x86_64.tar.gz'), 'linux');
  assert.equal(kind('CircleLens-1.3.0-linux-aarch64.tar.gz'), 'linux');
  assert.equal(kind('notes.txt'), 'other');
});

test('an asset gets a row when it first appears and whenever its count changes', () => {
  const release = (app, update) => [{ tag: 'v1.0.0', assets: [
    { name: 'CircleLens-Setup.exe', downloads: app },
    { name: 'CircleLens-Setup-1.0.0.exe', downloads: update },
  ] }];
  const rows = changes([], release(3, 0), '2026-10-11');
  assert.equal(rows.length, 2);
  assert.deepEqual(changes(rows, release(3, 0), '2026-10-12'), []);
  assert.deepEqual(changes(rows, release(5, 0), '2026-10-13'), [
    { date: '2026-10-13', tag: 'v1.0.0', asset: 'CircleLens-Setup.exe', downloads: 5 },
  ]);
});

test('a file checked out with CRLF line ends reads the same, so no row looks changed', () => {
  const text = 'date,tag,asset,downloads\n2026-10-11,v1.0.0,CircleLens-Setup.exe,3\n';
  const rows = parseCsv(text.replaceAll('\n', '\r\n'));
  assert.deepEqual(rows, parseCsv(text));
  assert.deepEqual(changes(rows, [{ tag: 'v1.0.0', assets: [{ name: 'CircleLens-Setup.exe', downloads: 3 }] }], '2026-10-12'), []);
});

test('downloads in a window are the lifetime counts at its end less those at its start', () => {
  const rows = [
    { date: '2026-10-01', tag: 'v1.0.0', asset: 'CircleLens-Setup.exe', downloads: 10 },
    { date: '2026-10-05', tag: 'v1.0.0', asset: 'CircleLens-Setup.exe', downloads: 14 },
    { date: '2026-10-06', tag: 'v1.1.0', asset: 'CircleLens-Setup.exe', downloads: 2 }, // a new release counts from 0
    { date: '2026-10-06', tag: 'v1.1.0', asset: 'CircleLens-Setup-1.1.0.exe', downloads: 7 },
    { date: '2026-10-09', tag: 'v1.1.0', asset: 'CircleLens-Setup.exe', downloads: 9 }, // after the window
  ];
  assert.equal(downloadsBetween(rows, 'app', '2026-10-03', '2026-10-08'), 6);
  assert.equal(downloadsBetween(rows, 'app', '', '2026-10-08'), 16);
  assert.equal(downloadsBetween(rows, 'update', '2026-10-03', '2026-10-08'), 7);
});

test('a daily series merges by date, the newer answer winning', () => {
  const merged = merge(
    [{ date: '2026-10-02', downloads: 0 }, { date: '2026-10-01', downloads: 4 }],
    [{ date: '2026-10-02', downloads: 3 }, { date: '2026-10-03', downloads: 1 }],
  );
  assert.deepEqual(merged, [
    { date: '2026-10-01', downloads: 4 },
    { date: '2026-10-02', downloads: 3 },
    { date: '2026-10-03', downloads: 1 },
  ]);
});
