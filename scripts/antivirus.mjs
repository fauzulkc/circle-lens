import { appendFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

// Watches the newest release's files on VirusTotal, which runs about 70 antivirus engines, Microsoft
// Defender's among them. The antivirus workflow (.github/workflows/antivirus.yml) runs it every hour:
//   node scripts/antivirus.mjs
// A new, unsigned file can scan clean when it's built and be flagged an hour later, once the antivirus
// clouds have seen it spread, so a check before release can't catch it. This catches it after:
//   - a file VirusTotal hasn't seen is uploaded, which also hands it to the antivirus vendors;
//   - a file is scanned again every hour for its first day, then once a day (results don't refresh alone);
//   - if Microsoft flags it, or three or more engines do, it opens an issue (one per file), updates it when
//     the detections change, and closes it once they clear.
// Environment: VT_API_KEY (without it, nothing is checked), GITHUB_TOKEN, GITHUB_REPOSITORY,
// GITHUB_STEP_SUMMARY (optional). The free VirusTotal API allows 4 requests a minute and 500 a day; a run
// makes at most two per file.
const FILES = [/^CircleLens-Setup-\d+\.\d+\.\d+\.exe$/, /^circle-lens-extension-\d+\.\d+\.\d+\.zip$/];
const HOUR = 3600_000;
const ISSUE_TITLE = (file) => `Antivirus flags ${file}`;

/** The engines that flag a file, from VirusTotal's last analysis: [{ engine, result }], sorted. */
export function detections(attributes) {
  return Object.values(attributes?.last_analysis_results ?? {})
    .filter((r) => r.category === 'malicious' || r.category === 'suspicious')
    .map((r) => ({ engine: r.engine_name, result: r.result ?? r.category }))
    .sort((a, b) => a.engine.localeCompare(b.engine));
}

/** Whether detections are worth an issue: Microsoft's (it blocks downloads on Windows), or three engines. */
export function alarming(found) {
  return found.some((d) => d.engine === 'Microsoft') || found.length >= 3;
}

/** Whether to ask VirusTotal for a fresh scan: hourly while the release is a day old or less, then daily. */
export function dueForScan(publishedAt, lastAnalysis, now) {
  if (!lastAnalysis) return true;
  const age = now - publishedAt;
  const since = now - lastAnalysis;
  return age <= 24 * HOUR ? since >= HOUR - 5 * 60_000 : since >= 20 * HOUR;
}

/** The issue's text for a flagged file. */
export function issueBody({ file, tag, sha256, found }) {
  const microsoft = found.find((d) => d.engine === 'Microsoft');
  const lines = [
    `VirusTotal reports that ${found.length} antivirus engine${found.length === 1 ? '' : 's'} flag \`${file}\` from [${tag}](https://github.com/${process.env.GITHUB_REPOSITORY ?? 'fauzulkc/circle-lens'}/releases/tag/${tag}).`,
    '',
    '| Engine | Detection |',
    '|---|---|',
    ...found.map((d) => `| ${d.engine} | \`${d.result}\` |`),
    '',
    `- VirusTotal: https://www.virustotal.com/gui/file/${sha256}`,
    `- SHA-256: \`${sha256}\``,
  ];
  if (microsoft) {
    lines.push(
      '',
      '**Microsoft flags it, so Windows blocks the download.** To fix:',
      '1. Report the false positive at https://www.microsoft.com/wdsi/filesubmission (as a software developer), with the SHA-256 and the release link.',
      '2. Until it\'s cleared, consider marking the last clean release as the latest, so download links give that.',
    );
  }
  lines.push('', 'This issue is updated when the detections change and closed when they clear.');
  return lines.join('\n');
}

const sleep = (ms) => new Promise((done) => setTimeout(done, ms));

async function virusTotal(path, init = {}) {
  const response = await fetch(`https://www.virustotal.com/api/v3${path}`, {
    ...init,
    headers: { 'x-apikey': process.env.VT_API_KEY, ...init.headers },
  });
  if (response.status === 404) return null;
  if (!response.ok) throw new Error(`VirusTotal answered ${response.status} for ${path}: ${(await response.text()).slice(0, 200)}`);
  return response.json();
}

async function github(path, init = {}) {
  const response = await fetch(`https://api.github.com${path}`, {
    ...init,
    headers: {
      accept: 'application/vnd.github+json',
      ...(process.env.GITHUB_TOKEN ? { authorization: `Bearer ${process.env.GITHUB_TOKEN}` } : {}),
      ...init.headers,
    },
  });
  if (!response.ok) throw new Error(`GitHub answered ${response.status} for ${path}`);
  return response.json();
}

/** Opens, updates or closes the file's issue to match what VirusTotal found. */
async function report(repository, { file, tag, sha256, found }) {
  const issues = await github(`/repos/${repository}/issues?state=open&per_page=100`);
  const issue = issues.find((i) => !i.pull_request && i.title === ISSUE_TITLE(file));
  const body = issueBody({ file, tag, sha256, found });
  const json = (value) => ({ method: 'POST', body: JSON.stringify(value), headers: { 'content-type': 'application/json' } });
  if (alarming(found)) {
    if (!issue) {
      const created = await github(`/repos/${repository}/issues`, json({ title: ISSUE_TITLE(file), body }));
      return `opened ${created.html_url}`;
    }
    if ((issue.body ?? '').replace(/\r/g, '') !== body) {
      await github(`/repos/${repository}/issues/${issue.number}`, { ...json({ body }), method: 'PATCH' });
      await github(`/repos/${repository}/issues/${issue.number}/comments`, json({ body: `The detections changed: ${found.map((d) => d.engine).join(', ')}.` }));
      return `updated ${issue.html_url}`;
    }
    return `still flagged, ${issue.html_url}`;
  }
  if (issue) {
    await github(`/repos/${repository}/issues/${issue.number}/comments`, json({ body: found.length ? `Down to ${found.length} minor detection${found.length === 1 ? '' : 's'} (${found.map((d) => d.engine).join(', ')}), so closing.` : 'No engine flags it any more, so closing.' }));
    await github(`/repos/${repository}/issues/${issue.number}`, { ...json({ state: 'closed' }), method: 'PATCH' });
    return `cleared, closed ${issue.html_url}`;
  }
  return '';
}

async function main() {
  const repository = process.env.GITHUB_REPOSITORY ?? 'fauzulkc/circle-lens';
  if (!process.env.VT_API_KEY) {
    console.log('::notice::No VT_API_KEY secret, so nothing was checked on VirusTotal (RELEASING.md).');
    return;
  }
  const release = await github(`/repos/${repository}/releases/latest`);
  const now = Date.now();
  const rows = [];
  let first = true;
  for (const asset of release.assets.filter((a) => FILES.some((pattern) => pattern.test(a.name)))) {
    if (!first) await sleep(31_000); // two requests per file, four a minute at most
    first = false;
    const sha256 = (asset.digest ?? '').replace(/^sha256:/, '');
    if (!sha256) continue;
    const known = await virusTotal(`/files/${sha256}`);
    if (!known) {
      const form = new FormData();
      const download = await fetch(asset.browser_download_url);
      form.append('file', new Blob([await download.arrayBuffer()]), asset.name);
      await virusTotal('/files', { method: 'POST', body: form });
      rows.push(`| ${asset.name} | uploaded, results next run | |`);
      continue;
    }
    const attributes = known.data.attributes;
    const lastAnalysis = (attributes.last_analysis_date ?? 0) * 1000;
    if (dueForScan(Date.parse(release.published_at), lastAnalysis, now)) {
      await virusTotal(`/files/${sha256}/analyse`, { method: 'POST' });
    }
    const found = detections(attributes);
    const action = await report(repository, { file: asset.name, tag: release.tag_name, sha256, found });
    const scanned = lastAnalysis ? new Date(lastAnalysis).toISOString().slice(0, 16).replace('T', ' ') + 'Z' : 'never';
    rows.push(`| [${asset.name}](https://www.virustotal.com/gui/file/${sha256}) | ${found.length ? found.map((d) => `${d.engine}: \`${d.result}\``).join('<br>') : 'clean'} | ${scanned} ${action} |`);
    if (alarming(found)) console.log(`::warning::${asset.name} is flagged by ${found.map((d) => d.engine).join(', ')}. ${action}`);
  }
  const summary = [`### Antivirus check of ${release.tag_name}`, '', '| File | Detections | Last scan |', '|---|---|---|', ...rows, ''].join('\n');
  console.log(summary);
  if (process.env.GITHUB_STEP_SUMMARY) await appendFile(process.env.GITHUB_STEP_SUMMARY, summary);
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  await main();
}
