import { mkdir, readdir, readFile, rm, copyFile, writeFile } from 'node:fs/promises';
import { join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';
import { crc32, deflateRawSync } from 'node:zlib';

// Makes the files to hand out, in dist/ (after `npm run build`):
//   CircleLens-Setup-<version>.exe     the desktop app; run from anywhere, it offers to install itself
//   circle-lens-extension-<version>.zip  the browser extension, as the Chrome Web Store, Edge Add-ons and
//                                        Firefox Add-ons each take it (one manifest serves all three)
//   circle-lens-<version>.min.js         the website script, for sites that host it themselves (it is
//                                        also on npm as `circle-lens`, and so on jsDelivr and unpkg)
// plus CircleLens-Setup.exe and circle-lens-extension.zip, the same files without the version, so
// https://github.com/fauzulkc/circle-lens/releases/latest/download/<name> always gives the newest. (The
// app's updater and winget use the versioned names.)
const root = fileURLToPath(new URL('..', import.meta.url));
const { version } = JSON.parse(await readFile(join(root, 'package.json'), 'utf8'));
const out = join(root, 'dist');
await rm(out, { recursive: true, force: true });
await mkdir(out, { recursive: true });

await copyFile(join(root, 'desktop', 'dist', `CircleLens-Setup-${version}.exe`), join(out, `CircleLens-Setup-${version}.exe`));
await writeFile(join(out, `circle-lens-extension-${version}.zip`), await zip(join(root, 'browser-extension', 'dist')));
await copyFile(join(root, 'web', 'dist', 'circle-lens.min.js'), join(out, `circle-lens-${version}.min.js`));
await copyFile(join(out, `CircleLens-Setup-${version}.exe`), join(out, 'CircleLens-Setup.exe'));
await copyFile(join(out, `circle-lens-extension-${version}.zip`), join(out, 'circle-lens-extension.zip'));
for (const name of await readdir(out)) console.log(`dist/${name}`);

/** A zip of every file under `folder` (deflated, paths relative to it, forward slashes). */
async function zip(folder) {
  const files = [];
  const walk = async (dir) => {
    for (const entry of await readdir(dir, { withFileTypes: true })) {
      const path = join(dir, entry.name);
      if (entry.isDirectory()) await walk(path);
      else files.push(path);
    }
  };
  await walk(folder);
  files.sort();
  const local = [];
  const central = [];
  let offset = 0;
  for (const path of files) {
    const name = Buffer.from(relative(folder, path).split('\\').join('/'));
    const data = await readFile(path);
    const packed = deflateRawSync(data, { level: 9 });
    const checksum = crc32(data);
    const header = Buffer.alloc(30);
    header.writeUInt32LE(0x04034b50, 0); // local file header
    header.writeUInt16LE(20, 4); // version needed
    header.writeUInt16LE(0x0800, 6); // UTF-8 names
    header.writeUInt16LE(8, 8); // deflate
    header.writeUInt32LE(0, 10); // time and date: left at zero so builds are repeatable
    header.writeUInt32LE(checksum, 14);
    header.writeUInt32LE(packed.length, 18);
    header.writeUInt32LE(data.length, 22);
    header.writeUInt16LE(name.length, 26);
    local.push(header, name, packed);
    const entry = Buffer.alloc(46);
    entry.writeUInt32LE(0x02014b50, 0); // central directory header
    entry.writeUInt16LE(20, 4);
    entry.writeUInt16LE(20, 6);
    entry.writeUInt16LE(0x0800, 8);
    entry.writeUInt16LE(8, 10);
    entry.writeUInt32LE(0, 12);
    entry.writeUInt32LE(checksum, 16);
    entry.writeUInt32LE(packed.length, 20);
    entry.writeUInt32LE(data.length, 24);
    entry.writeUInt16LE(name.length, 28);
    entry.writeUInt32LE(offset, 42);
    central.push(entry, name);
    offset += header.length + name.length + packed.length;
  }
  const directory = Buffer.concat(central);
  const end = Buffer.alloc(22);
  end.writeUInt32LE(0x06054b50, 0);
  end.writeUInt16LE(files.length, 8);
  end.writeUInt16LE(files.length, 10);
  end.writeUInt32LE(directory.length, 12);
  end.writeUInt32LE(offset, 16);
  return Buffer.concat([...local, directory, end]);
}
