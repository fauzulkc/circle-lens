import { build } from 'esbuild';
import { cp, mkdir, readFile, rm, writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';

const root = fileURLToPath(new URL('.', import.meta.url));
const out = `${root}dist/`;
const watch = process.argv.includes('--watch');

await rm(out, { recursive: true, force: true });
await mkdir(out, { recursive: true });

const options = {
  entryPoints: { content: `${root}src/content.js`, background: `${root}src/background.js`, options: `${root}src/options.js` },
  outdir: out,
  bundle: true,
  format: 'iife',
  target: ['chrome114', 'firefox128'],
  loader: { '.css': 'text' },
  legalComments: 'none',
};

if (watch) {
  const { context } = await import('esbuild');
  await (await context(options)).watch();
} else await build(options);

// The manifest takes its version from the root package.json, so there is one place to bump.
const { version } = JSON.parse(await readFile(`${root}../package.json`, 'utf8'));
const manifest = JSON.parse(await readFile(`${root}manifest.json`, 'utf8'));
await writeFile(`${out}manifest.json`, `${JSON.stringify({ ...manifest, version }, null, 2)}\n`);
await cp(`${root}src/options.html`, `${out}options.html`);
await cp(`${root}icons`, `${out}icons`, { recursive: true });
console.log(`Built the extension into ${out}${watch ? ' (watching)' : ''}`);
