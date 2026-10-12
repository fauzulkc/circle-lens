import { spawnSync } from 'node:child_process';
import { mkdir, readFile, rm, writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { gzipSync } from 'node:zlib';

// Builds the Linux and macOS previews into desktop/dist:
//   node desktop/build-unix.mjs linux [version]   CircleLens-<version>-linux-x86_64.tar.gz and -aarch64.tar.gz,
//                                                  from any OS (pure Go, no cgo, so it cross-compiles)
//   node desktop/build-unix.mjs mac [version]     CircleLens-<version>-mac.zip: one universal Circle Lens.app,
//                                                  ad-hoc signed. Only on a Mac (cgo, AppKit).
// The version defaults to package.json's; a preview passes its own ("1.3.0-preview.1").
const desktop = fileURLToPath(new URL('.', import.meta.url));
const [target, given] = process.argv.slice(2);
const version = given ?? JSON.parse(await readFile(new URL('../package.json', import.meta.url), 'utf8')).version;
const dist = `${desktop}dist/`;
await mkdir(dist, { recursive: true });

const run = (command, args, env = {}) => {
  const result = spawnSync(command, args, { cwd: desktop, stdio: 'inherit', env: { ...process.env, ...env } });
  if (result.status !== 0) process.exit(result.status ?? 1);
};

if (target === 'linux') {
  for (const [goarch, arch] of [['amd64', 'x86_64'], ['arm64', 'aarch64']]) {
    const folder = `CircleLens-${version}-linux-${arch}`;
    const program = `${dist}${folder}/circle-lens`;
    await rm(`${dist}${folder}`, { recursive: true, force: true });
    run('go', ['build', '-trimpath', '-ldflags', `-X main.version=${version}`, '-o', program, './cmd/circle-lens'],
      { GOOS: 'linux', GOARCH: goarch, CGO_ENABLED: '0' });
    await writeFile(`${dist}${folder}/README.txt`, linuxReadme(version));
    await writeFile(`${dist}${folder}.tar.gz`, gzipSync(await tar(dist, folder, ['circle-lens', 'README.txt']), { level: 9 }));
    await rm(`${dist}${folder}`, { recursive: true, force: true });
    console.log(`Built desktop/dist/${folder}.tar.gz`);
  }
} else if (target === 'mac') {
  const app = `${dist}Circle Lens.app`;
  await rm(app, { recursive: true, force: true });
  await mkdir(`${app}/Contents/MacOS`, { recursive: true });
  await mkdir(`${app}/Contents/Resources`, { recursive: true });
  for (const goarch of ['arm64', 'amd64']) {
    const arch = goarch === 'amd64' ? 'x86_64' : 'arm64';
    run('go', ['build', '-trimpath', '-ldflags', `-X main.version=${version}`, '-o', `${dist}circle-lens-${goarch}`, './cmd/circle-lens'],
      { GOOS: 'darwin', GOARCH: goarch, CGO_ENABLED: '1', CC: `clang -arch ${arch}`, MACOSX_DEPLOYMENT_TARGET: '14.0' });
  }
  run('lipo', ['-create', '-output', `${app}/Contents/MacOS/circle-lens`, `${dist}circle-lens-arm64`, `${dist}circle-lens-amd64`]);
  await rm(`${dist}circle-lens-arm64`);
  await rm(`${dist}circle-lens-amd64`);
  await writeFile(`${app}/Contents/Info.plist`, infoPlist(version));
  // The icon: every size macOS asks for, drawn by make-icons, packed by iconutil.
  const iconset = `${dist}AppIcon.iconset`;
  await rm(iconset, { recursive: true, force: true });
  run('go', ['run', './cmd/make-icons', '-mac', iconset]);
  run('iconutil', ['-c', 'icns', iconset, '-o', `${app}/Contents/Resources/AppIcon.icns`]);
  await rm(iconset, { recursive: true, force: true });
  // Ad-hoc signed: Apple silicon runs nothing unsigned. It is not notarized, so macOS asks once (README).
  run('codesign', ['--force', '--deep', '--sign', '-', app]);
  const zip = `${dist}CircleLens-${version}-mac.zip`;
  await rm(zip, { force: true });
  run('ditto', ['-c', '-k', '--keepParent', app, zip]);
  console.log(`Built desktop/dist/CircleLens-${version}-mac.zip`);
} else {
  console.error('usage: node desktop/build-unix.mjs linux|mac [version]');
  process.exit(2);
}

function infoPlist(version) {
  const plain = version.split('-')[0];
  return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleName</key><string>Circle Lens</string>
	<key>CFBundleDisplayName</key><string>Circle Lens</string>
	<key>CFBundleIdentifier</key><string>com.fauzul.circlelens</string>
	<key>CFBundleExecutable</key><string>circle-lens</string>
	<key>CFBundleIconFile</key><string>AppIcon</string>
	<key>CFBundlePackageType</key><string>APPL</string>
	<key>CFBundleShortVersionString</key><string>${plain}</string>
	<key>CFBundleVersion</key><string>${version}</string>
	<key>LSMinimumSystemVersion</key><string>14.0</string>
	<key>LSUIElement</key><true/>
	<key>NSHighResolutionCapable</key><true/>
	<key>NSHumanReadableCopyright</key><string>MIT licence. https://github.com/fauzulkc/circle-lens</string>
</dict>
</plist>
`;
}

function linuxReadme(version) {
  return `Circle Lens ${version} for Linux (preview)

Draw a circle twice with the mouse, anywhere, to open a magnifying lens at the pointer.

Run it:        ./circle-lens
Install it:    ./circle-lens -install      (to ~/.local/bin, the app menu, and start at login)
Uninstall:     circle-lens -uninstall
Quit:          circle-lens -quit, or its tray menu

It works in an X11 session. GNOME on Wayland needs the GNOME extension instead (coming). The tray icon
shows on KDE, Xfce, Cinnamon, MATE and Budgie, and on GNOME with the AppIndicator extension (Ubuntu has it).

Source, issues and the Windows app: https://github.com/fauzulkc/circle-lens
`;
}

/** A tar of `names` under `root/folder` (ustar, owned by nobody, executable where they are). */
async function tar(root, folder, names) {
  const blocks = [];
  for (const name of names) {
    const data = await readFile(`${root}${folder}/${name}`);
    const header = Buffer.alloc(512);
    const path = `${folder}/${name}`;
    header.write(path, 0, 100);
    header.write(name === 'circle-lens' ? '0000755\0' : '0000644\0', 100);
    header.write('0000000\0', 108);
    header.write('0000000\0', 116);
    header.write(data.length.toString(8).padStart(11, '0') + '\0', 124);
    header.write('00000000000\0', 136); // time zero, so builds are repeatable
    header.write('        ', 148);
    header.write('0', 156);
    header.write('ustar\0', 257);
    header.write('00', 263);
    let sum = 0;
    for (const byte of header) sum += byte;
    header.write(sum.toString(8).padStart(6, '0') + '\0 ', 148);
    blocks.push(header, data, Buffer.alloc((512 - (data.length % 512)) % 512));
  }
  blocks.push(Buffer.alloc(1024));
  return Buffer.concat(blocks);
}

