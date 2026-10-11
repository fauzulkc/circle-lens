import { spawnSync } from 'node:child_process';
import { readFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';

// Builds the desktop app for Windows (from any OS: it is standard-library Go, no cgo), stamped with the
// version in the root package.json. macOS and Linux targets join here once their backends exist.
const desktop = fileURLToPath(new URL('.', import.meta.url));
const { version } = JSON.parse(await readFile(new URL('../package.json', import.meta.url), 'utf8'));
const result = spawnSync(
  'go',
  ['build', '-trimpath', '-ldflags', `-H windowsgui -s -w -X main.version=${version}`, '-o', 'dist/circle-lens.exe', './cmd/circle-lens'],
  { cwd: desktop, stdio: 'inherit', env: { ...process.env, GOOS: 'windows', GOARCH: 'amd64', CGO_ENABLED: '0' } },
);
if (result.status !== 0) process.exit(result.status ?? 1);
console.log(`Built desktop/dist/circle-lens.exe (${version})`);
