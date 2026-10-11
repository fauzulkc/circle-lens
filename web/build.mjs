import { build } from 'esbuild';
import { copyFile, mkdir, readFile, rm } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';

// The website package (npm `circle-lens`): an ES module, a CommonJS copy, and a script-tag build that
// starts itself. Its version is the root package.json's, so there is one place to bump.
const root = fileURLToPath(new URL('.', import.meta.url));
const out = `${root}dist/`;
const { version } = JSON.parse(await readFile(`${root}../package.json`, 'utf8'));
const own = JSON.parse(await readFile(`${root}package.json`, 'utf8'));
if (own.version !== version) throw new Error(`web/package.json is ${own.version} but the root package.json is ${version}: make them the same`);

await rm(out, { recursive: true, force: true });
await mkdir(out, { recursive: true });

const common = {
  bundle: true,
  target: ['chrome111', 'firefox113', 'safari16.4'],
  loader: { '.css': 'text' },
  define: { __VERSION__: JSON.stringify(version) },
  legalComments: 'none',
  banner: { js: `/*! Circle Lens ${version} | MIT | https://github.com/fauzulkc/circle-lens */` },
};

await Promise.all([
  build({ ...common, entryPoints: [`${root}src/index.js`], format: 'esm', outfile: `${out}circle-lens.mjs` }),
  build({ ...common, entryPoints: [`${root}src/index.js`], format: 'cjs', outfile: `${out}circle-lens.cjs` }),
  build({ ...common, entryPoints: [`${root}src/script.js`], format: 'iife', minify: true, outfile: `${out}circle-lens.min.js` }),
]);
// npm only packs files inside the package folder.
await copyFile(`${root}../LICENSE`, `${root}LICENSE`);
console.log(`Built the website package into ${out}`);
