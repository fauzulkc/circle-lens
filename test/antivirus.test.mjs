import { test } from 'node:test';
import assert from 'node:assert/strict';
import { alarming, detections, dueForScan, issueBody } from '../scripts/antivirus.mjs';

const HOUR = 3600_000;

test('only engines that call a file malicious or suspicious count as detections', () => {
  const found = detections({ last_analysis_results: {
    Microsoft: { engine_name: 'Microsoft', category: 'malicious', result: 'Trojan:Win32/Wacatac.B!ml' },
    Avast: { engine_name: 'Avast', category: 'undetected', result: null },
    Bkav: { engine_name: 'Bkav', category: 'suspicious', result: null },
    Kaspersky: { engine_name: 'Kaspersky', category: 'type-unsupported', result: null },
  } });
  assert.deepEqual(found, [
    { engine: 'Bkav', result: 'suspicious' },
    { engine: 'Microsoft', result: 'Trojan:Win32/Wacatac.B!ml' },
  ]);
  assert.deepEqual(detections(undefined), []);
});

test('Microsoft alone raises the alarm; other engines need three', () => {
  assert.equal(alarming([{ engine: 'Microsoft', result: 'x' }]), true);
  assert.equal(alarming([{ engine: 'Bkav', result: 'x' }, { engine: 'Jiangmin', result: 'x' }]), false);
  assert.equal(alarming([{ engine: 'A', result: 'x' }, { engine: 'B', result: 'x' }, { engine: 'C', result: 'x' }]), true);
  assert.equal(alarming([]), false);
});

test('a file is scanned hourly in its first day, then daily', () => {
  const published = Date.parse('2026-10-12T00:00:00Z');
  assert.equal(dueForScan(published, 0, published + HOUR), true, 'never scanned');
  assert.equal(dueForScan(published, published + 2 * HOUR, published + 2.5 * HOUR), false);
  assert.equal(dueForScan(published, published + 2 * HOUR, published + 3 * HOUR), true);
  assert.equal(dueForScan(published, published + 30 * HOUR, published + 40 * HOUR), false);
  assert.equal(dueForScan(published, published + 30 * HOUR, published + 51 * HOUR), true);
});

test('the issue names the detections and, when Microsoft flags it, how to get it cleared', () => {
  const body = issueBody({ file: 'CircleLens-Setup-1.2.1.exe', tag: 'v1.2.1', sha256: 'ab'.repeat(32),
    found: [{ engine: 'Microsoft', result: 'Trojan:Win32/Wacatac.B!ml' }] });
  assert.match(body, /\| Microsoft \| `Trojan:Win32\/Wacatac\.B!ml` \|/);
  assert.match(body, /wdsi\/filesubmission/);
  assert.doesNotMatch(issueBody({ file: 'f', tag: 't', sha256: 's', found: [{ engine: 'A', result: 'x' }] }), /wdsi/);
});
