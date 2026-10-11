import { spawnSync } from 'node:child_process';
import { copyFile, readFile, writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';

// Builds the desktop app for Windows (from any OS: it is standard-library Go, no cgo), stamped with the
// version in the root package.json:
//   0. this version's CHANGELOG section goes into cmd/circle-lens/whatsnew.md, which the app shows as
//      "What's new" (a Go test checks it matches the changelog);
//   1. go-winres embeds the app icon, version information and the Windows manifest (per-monitor DPI
//      awareness, no admin) from cmd/circle-lens/winres;
//   2. go build makes dist/circle-lens.exe. Symbols are kept (no -s -w): stripped Go programs are scored
//      as more suspicious by antivirus heuristics, which flagged a stripped release as a trojan;
//   3. that same exe is copied to dist/CircleLens-Setup-<version>.exe, the file to hand out: run from
//      anywhere, it offers to install itself (see install_windows.go).
// macOS and Linux targets join here once their backends exist.
const desktop = fileURLToPath(new URL('.', import.meta.url));
const { version } = JSON.parse(await readFile(new URL('../package.json', import.meta.url), 'utf8'));
const windows = { ...process.env, GOOS: 'windows', GOARCH: 'amd64', CGO_ENABLED: '0' };

const run = (command, args) => {
  const result = spawnSync(command, args, { cwd: desktop, stdio: 'inherit', env: windows });
  if (result.status !== 0) process.exit(result.status ?? 1);
};

const changelog = await readFile(new URL('../CHANGELOG.md', import.meta.url), 'utf8');
await writeFile(`${desktop}cmd/circle-lens/whatsnew.md`, releaseNotes(changelog, version));

const fourPart = `${version.split('-')[0]}.0`;
run('go', [
  'run', 'github.com/tc-hib/go-winres@v0.3.3', 'make',
  '--in', 'cmd/circle-lens/winres/winres.json', '--out', 'cmd/circle-lens/rsrc', '--arch', 'amd64',
  '--product-version', fourPart, '--file-version', fourPart,
]);
run('go', ['build', '-trimpath', '-ldflags', `-H windowsgui -X main.version=${version}`, '-o', 'dist/circle-lens.exe', './cmd/circle-lens']);
await copyFile(`${desktop}dist/circle-lens.exe`, `${desktop}dist/CircleLens-Setup-${version}.exe`);
console.log(`Built desktop/dist/circle-lens.exe and CircleLens-Setup-${version}.exe (${version})`);

/** The CHANGELOG section for `version` (between its "## version" heading and the next "## "). */
export function releaseNotes(changelog, version) {
  const lines = changelog.split('\n');
  const start = lines.findIndex((line) => line.trim() === `## ${version}`);
  if (start < 0) return '';
  const end = lines.findIndex((line, i) => i > start && line.startsWith('## '));
  return `${lines.slice(start + 1, end < 0 ? undefined : end).join('\n').trim()}\n`;
}
