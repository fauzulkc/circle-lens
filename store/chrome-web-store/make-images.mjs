// Renders the Chrome Web Store images from pages/ with headless Chrome, the real lens open on each.
//
//   npm run build            (the lens comes from web/dist, the settings page from browser-extension/dist)
//   node store/chrome-web-store/make-images.mjs
//
// Chrome is found in the usual places; set CHROME_PATH to use another. It runs with a throwaway
// profile under .cache/ and never touches your own.
import { spawn } from 'node:child_process';
import { existsSync } from 'node:fs';
import { mkdir, rm, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const here = fileURLToPath(new URL('.', import.meta.url));
const root = join(here, '..', '..');
const port = 9335;

// Each image: its page, size, and where the lens opens (an element and a point inside it).
const images = [
  { out: 'screenshot-1-fine-print.png', page: 'fine-print.html', width: 1280, height: 800, lens: { selector: '.page', x: 0.5, y: 0.78 } },
  { out: 'screenshot-2-numbers.png', page: 'numbers.html', width: 1280, height: 800, lens: { selector: 'tr:nth-child(8) td:nth-child(6)', x: 0.85, y: 0.5 } },
  { out: 'screenshot-3-settings.png', page: 'settings.html', width: 1280, height: 800 },
  { out: 'promo-small-440x280.png', page: 'promo-small.html', width: 440, height: 280 },
  { out: 'promo-marquee-1400x560.png', page: 'promo-marquee.html', width: 1400, height: 560, lens: { selector: '.sample', x: 0.5, y: 0.52 } },
];

const chromePath =
  process.env.CHROME_PATH ??
  [
    'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe',
    'C:\\Program Files (x86)\\Google\\Chrome\\Application\\chrome.exe',
    '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
    '/usr/bin/google-chrome',
    '/usr/bin/chromium',
  ].find(existsSync);
if (!chromePath) throw new Error('Chrome not found: set CHROME_PATH');
for (const built of ['web/dist/circle-lens.min.js', 'browser-extension/dist/options.html']) {
  if (!existsSync(join(root, built))) throw new Error(`${built} is missing: run npm run build first`);
}

const profile = join(root, '.cache', 'store-chrome');
await rm(profile, { recursive: true, force: true });
const chrome = spawn(chromePath, ['--headless=new', `--remote-debugging-port=${port}`, `--user-data-dir=${profile}`, '--hide-scrollbars', '--force-device-scale-factor=1', 'about:blank'], { stdio: 'ignore' });
const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

let targets;
for (let attempt = 0; attempt < 50 && !targets; attempt++) {
  try {
    targets = await (await fetch(`http://127.0.0.1:${port}/json`)).json();
  } catch {
    await sleep(200);
  }
}
const socket = new WebSocket(targets.find((target) => target.type === 'page').webSocketDebuggerUrl);
await new Promise((resolve) => socket.addEventListener('open', resolve, { once: true }));
let nextId = 0;
const pending = new Map();
socket.addEventListener('message', (event) => {
  const message = JSON.parse(event.data);
  if (message.id && pending.has(message.id)) {
    pending.get(message.id)(message.result ?? {});
    pending.delete(message.id);
  }
});
const send = (method, params = {}) =>
  new Promise((resolve) => {
    const id = ++nextId;
    pending.set(id, resolve);
    socket.send(JSON.stringify({ id, method, params }));
  });
const evaluate = async (expression) => (await send('Runtime.evaluate', { expression, returnByValue: true, awaitPromise: true })).result?.value;
const show = async (url, width, height) => {
  await send('Emulation.setDeviceMetricsOverride', { width, height, deviceScaleFactor: 1, mobile: false });
  await send('Page.navigate', { url });
  await sleep(1200);
};
const capture = async (path, clip) => {
  const { data } = await send('Page.captureScreenshot', { format: 'png', ...(clip ? { clip: { scale: 1, ...clip } } : {}) });
  await writeFile(path, Buffer.from(data, 'base64'));
  console.log(path.slice(root.length + 1));
};

await send('Page.enable');
await send('Runtime.enable');

// The extension's settings page, as it is, with storage standing in for the extension's.
await mkdir(join(here, 'build'), { recursive: true });
const stub = await send('Page.addScriptToEvaluateOnNewDocument', {
  source: 'globalThis.chrome = { storage: { sync: { get: async () => ({}), set: async () => {} }, onChanged: { addListener() {} } } };',
});
await show(pathToFileURL(join(root, 'browser-extension', 'dist', 'options.html')).href, 560, 900);
const box = await evaluate('(() => { const r = document.body.getBoundingClientRect(); return { x: 0, y: 0, width: Math.ceil(r.width), height: Math.ceil(Math.max(r.height, document.body.scrollHeight)) }; })()');
await capture(join(here, 'build', 'options.png'), { ...box, scale: 1.35 }); // sharp at the size it is shown
await send('Page.removeScriptToEvaluateOnNewDocument', { identifier: stub.identifier });

for (const image of images) {
  await show(pathToFileURL(join(here, 'pages', image.page)).href, image.width, image.height);
  if (image.lens) {
    const { selector, x, y } = image.lens;
    await evaluate(`(() => { const r = document.querySelector(${JSON.stringify(selector)}).getBoundingClientRect(); CircleLens.lens.open(r.left + r.width * ${x}, r.top + r.height * ${y}); })()`);
    await sleep(1400); // the lens springs open in 560 ms; let it settle
  }
  await capture(join(here, image.out));
}

socket.close();
chrome.kill();
await rm(profile, { recursive: true, force: true }).catch(() => {});
process.exit(0);
