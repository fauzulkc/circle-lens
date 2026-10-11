import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

// Saves Circle Lens's public download numbers into a folder, which the stats workflow
// (.github/workflows/stats.yml) checks out from the `stats` branch once a day:
//   node scripts/stats.mjs <folder>
// GitHub keeps only lifetime totals of release downloads, and repository traffic for 14 days, so these
// files are the history. Each run adds to them and rewrites README.md, a summary to read on GitHub.
//   releases.csv  date,tag,asset,downloads            an asset's lifetime count, whenever it changes
//   npm.csv       date,downloads                      npm's daily downloads of the website package
//   jsdelivr.csv  date,hits                           jsDelivr's daily requests for it
//   repo.csv      date,stars,forks,watchers
//   traffic.csv   date,views,visitors,clones,cloners  GitHub's daily traffic (needs TRAFFIC_TOKEN)
// Dates are UTC. GITHUB_TOKEN is optional (the numbers are public; a token only raises the rate limit).
// TRAFFIC_TOKEN is a fine-grained token with read access to the repository's administration, which
// GitHub requires for traffic; without it, traffic is skipped.
const PACKAGE = 'circle-lens';

/** What a release asset's downloads stand for, from its name (scripts/package.mjs makes them). */
export function kind(asset) {
  if (asset === 'CircleLens-Setup.exe') return 'app'; // the download links: someone getting the app
  if (asset === 'circle-lens-extension.zip') return 'extension';
  if (/^CircleLens-Setup-\d+\.\d+\.\d+\.exe$/.test(asset)) return 'update'; // installed copies updating, and winget
  if (/^circle-lens-extension-\d+\.\d+\.\d+\.zip$/.test(asset)) return 'extension-versioned';
  if (/^circle-lens-\d+\.\d+\.\d+\.min\.js$/.test(asset)) return 'script'; // sites hosting the script themselves
  return 'other';
}

/** Each asset's last releases.csv row at the end of `date` ('' is before any). */
export function countsAsOf(rows, date) {
  const counts = new Map();
  for (const row of rows) if (row.date <= date) counts.set(`${row.tag} ${row.asset}`, row);
  return counts;
}

/** Downloads of one kind of asset between the ends of two dates: the lifetime counts at `to` less those at `from`. */
export function downloadsBetween(rows, assetKind, from, to) {
  const total = (date) => {
    let sum = 0;
    for (const row of countsAsOf(rows, date).values()) if (kind(row.asset) === assetKind) sum += row.downloads;
    return sum;
  };
  return total(to) - total(from);
}

/** The releases.csv rows to add: each asset whose count isn't the one last recorded. */
export function changes(rows, releases, date) {
  const last = countsAsOf(rows, '9999');
  return releases.flatMap((release) =>
    release.assets
      .filter((asset) => last.get(`${release.tag} ${asset.name}`)?.downloads !== asset.downloads)
      .map((asset) => ({ date, tag: release.tag, asset: asset.name, downloads: asset.downloads })));
}

/** A daily series with `fresh` merged in by date (the newer answer wins), oldest first. */
export function merge(rows, fresh) {
  const byDate = new Map(rows.map((row) => [row.date, row]));
  for (const row of fresh) byDate.set(row.date, row);
  return [...byDate.values()].sort((a, b) => a.date.localeCompare(b.date));
}

/** The total of `field` over the days after `from`, up to and including `to`. */
export function sumDays(rows, field, from, to) {
  return rows.reduce((sum, row) => (row.date > from && row.date <= to ? sum + row[field] : sum), 0);
}

const addDays = (date, days) => new Date(Date.parse(date) + days * 864e5).toISOString().slice(0, 10);

async function main(folder) {
  const repo = process.env.GITHUB_REPOSITORY || 'fauzulkc/circle-lens';
  const today = new Date().toISOString().slice(0, 10);
  const file = (name) => join(folder, name);
  await mkdir(folder, { recursive: true });

  const releases = await githubReleases(repo);
  const rows = await readCsv(file('releases.csv'));
  rows.push(...changes(rows, releases, today));
  await writeCsv(file('releases.csv'), ['date', 'tag', 'asset', 'downloads'], rows);

  const info = await github(`/repos/${repo}`);
  const repoRows = merge(await readCsv(file('repo.csv')), [
    { date: today, stars: info.stargazers_count, forks: info.forks_count, watchers: info.subscribers_count },
  ]);
  await writeCsv(file('repo.csv'), ['date', 'stars', 'forks', 'watchers'], repoRows);

  let npm = await readCsv(file('npm.csv'));
  await optional('npm', async () => {
    const answer = await get(`https://api.npmjs.org/downloads/range/last-month/${PACKAGE}`);
    npm = merge(npm, answer.downloads.map(({ day, downloads }) => ({ date: day, downloads })));
    await writeCsv(file('npm.csv'), ['date', 'downloads'], npm);
  });

  let jsdelivr = await readCsv(file('jsdelivr.csv'));
  await optional('jsDelivr', async () => {
    const answer = await get(`https://data.jsdelivr.com/v1/stats/packages/npm/${PACKAGE}?period=month`);
    jsdelivr = merge(jsdelivr, Object.entries(answer.hits.dates).map(([date, hits]) => ({ date, hits })));
    await writeCsv(file('jsdelivr.csv'), ['date', 'hits'], jsdelivr);
  });

  // This run's 14-day totals and referrers for the summary, or why there are none.
  let traffic = 'Traffic (views, visitors, referrers) isn\'t saved: it needs the `TRAFFIC_TOKEN` secret '
    + `([RELEASING.md](https://github.com/${repo}/blob/main/RELEASING.md#download-numbers)).`;
  const token = process.env.TRAFFIC_TOKEN;
  if (!token) console.log('::notice::No TRAFFIC_TOKEN secret, so repository traffic was not saved (RELEASING.md).');
  else {
    traffic = 'Traffic couldn\'t be read this time.';
    await optional('traffic', async () => {
      const views = await github(`/repos/${repo}/traffic/views?per=day`, token);
      const clones = await github(`/repos/${repo}/traffic/clones?per=day`, token);
      const referrers = await github(`/repos/${repo}/traffic/popular/referrers`, token);
      const days = new Map();
      const day = (timestamp) => {
        const date = timestamp.slice(0, 10);
        if (!days.has(date)) days.set(date, { date, views: 0, visitors: 0, clones: 0, cloners: 0 });
        return days.get(date);
      };
      for (const entry of views.views) Object.assign(day(entry.timestamp), { views: entry.count, visitors: entry.uniques });
      for (const entry of clones.clones) Object.assign(day(entry.timestamp), { clones: entry.count, cloners: entry.uniques });
      const trafficRows = merge(await readCsv(file('traffic.csv')), [...days.values()]);
      await writeCsv(file('traffic.csv'), ['date', 'views', 'visitors', 'clones', 'cloners'], trafficRows);
      traffic = { views, clones, referrers };
    });
  }

  await writeFile(file('README.md'), summary({ repo, today, rows, releases, stats: repoRows.at(-1), npm, jsdelivr, traffic }));
  console.log(`Saved the numbers for ${today} to ${folder}.`);
}

/** README.md: the numbers worth reading, with what each one means. */
function summary({ repo, today, rows, releases, stats, npm, jsdelivr, traffic }) {
  const n = (value) => (value == null ? '—' : value.toLocaleString('en-US'));
  const of = (value, word) => `${n(value)} ${word}${value === 1 ? '' : 's'}`;
  const start = rows[0]?.date ?? today;
  // Release counts are lifetime totals, so a window needs history reaching back to its start.
  const assets = (assetKind) => (days) => {
    if (!days) return downloadsBetween(rows, assetKind, '', today);
    return start <= addDays(today, -days) ? downloadsBetween(rows, assetKind, addDays(today, -days), today) : null;
  };
  // npm and jsDelivr report each day, and the first run fetches the 30 before it.
  const daily = (series, field) => (days) => (series.length ? sumDays(series, field, days ? addDays(today, -days) : '', today) : null);
  const line = (label, count) => `| ${label} | ${n(count(7))} | ${n(count(30))} | ${n(count(0))} |`;

  const ordered = [...releases].sort((a, b) => a.published.localeCompare(b.published));
  const census = ordered.map((release, i) => {
    const next = ordered[i + 1];
    const hours = (Date.parse(next?.published ?? new Date().toISOString()) - Date.parse(release.published)) / 36e5;
    const span = hours < 1 ? 'under 1 h' : hours < 48 ? `${Math.round(hours)} h` : `${Math.round(hours / 24)} days`;
    const count = (assetKind) => release.assets.reduce((sum, asset) => (kind(asset.name) === assetKind ? sum + asset.downloads : sum), 0);
    return `| ${release.tag} | ${release.published.slice(0, 10)} | ${span}${next ? '' : ' so far'} | ${n(count('update'))} | ${n(count('app'))} | ${n(count('extension'))} |`;
  }).reverse().slice(0, 10);

  let repository = `${of(stats.stars, 'star')}, ${of(stats.forks, 'fork')}, ${n(stats.watchers)} watching.\n\n`;
  if (typeof traffic === 'string') repository += `${traffic}\n`;
  else {
    const { views, clones, referrers } = traffic;
    repository += `Last 14 days, by GitHub's count: ${of(views.count, 'view')} from ${of(views.uniques, 'visitor')}, and ${of(clones.count, 'clone')} from ${of(clones.uniques, 'cloner')}.\n\n`;
    repository += referrers.length
      ? `| Referrer | Views | Visitors |\n|---|--:|--:|\n${referrers.map((r) => `| ${r.referrer} | ${n(r.count)} | ${n(r.uniques)} |`).join('\n')}\n`
      : 'No referrers in the last 14 days.\n';
  }

  return `# Circle Lens download numbers

Read on ${today} (UTC) by the [stats workflow](https://github.com/${repo}/blob/main/.github/workflows/stats.yml), once a day. GitHub keeps only lifetime totals, so the CSV files here are the history, which starts on ${start}.

## Downloads

| | Last 7 days | Last 30 days | All time |
|---|--:|--:|--:|
${line('Windows app, from the download links', assets('app'))}
${line('Browser extension, from the download links', assets('extension'))}
${line('Website script, from the releases', assets('script'))}
${line('Website package, from npm', daily(npm, 'downloads'))}
${line('Website script, loads through jsDelivr', daily(jsdelivr, 'hits'))}

- **The download links** are \`releases/latest/download/CircleLens-Setup.exe\` and \`circle-lens-extension.zip\`, the ones the website, README and llms.txt give: someone getting it, or getting it again.
- The release rows show a week or a month once the history goes back that far.
- npm counts include mirrors and bots. jsDelivr counts loads of the script on other sites' pages, not sites. Both report a day or two late.

## Installed apps still in use

A copy with "Update automatically" on (the default) downloads each new version once, as \`CircleLens-Setup-<version>.exe\`. So once a version has been the latest for a week or so, its update downloads are about the number of copies used that week. A copy that skips a version, because it was off or a newer one came out first, isn't in that version's count. The counts also include winget installs, winget's validation bots, downloads from the release page and the maintainer's own copy.

| Version | Released | Latest for | Update downloads | App, from the links | Extension, from the links |
|---|---|--:|--:|--:|--:|
${census.join('\n')}

## Repository

${repository}`;
}

async function githubReleases(repo) {
  const releases = [];
  for (let page = 1; ; page++) {
    const batch = await github(`/repos/${repo}/releases?per_page=100&page=${page}`);
    for (const release of batch) {
      if (release.draft) continue;
      releases.push({
        tag: release.tag_name,
        published: release.published_at,
        assets: release.assets.map((asset) => ({ name: asset.name, downloads: asset.download_count })),
      });
    }
    if (batch.length < 100) return releases;
  }
}

const github = (path, token = process.env.GITHUB_TOKEN) => get(`https://api.github.com${path}`, token);

async function get(url, token) {
  const headers = { Accept: url.startsWith('https://api.github.com/') ? 'application/vnd.github+json' : 'application/json', 'User-Agent': 'circle-lens-stats' };
  if (token) headers.Authorization = `Bearer ${token}`;
  const response = await fetch(url, { headers });
  if (!response.ok) throw new Error(`${url.split('?')[0]} answered ${response.status}: ${(await response.text()).slice(0, 200)}`);
  return response.json();
}

/** Runs one optional source: a failure is a warning in the workflow, not a failed run. */
async function optional(name, work) {
  try {
    await work();
  } catch (error) {
    console.log(`::warning::${name}: ${error.message}`);
  }
}

/** Rows from one of these CSV files, numbers as numbers. A Windows checkout may have given it CRLF line ends. */
export function parseCsv(text) {
  const [header, ...lines] = text.trim().split(/\r?\n/);
  const fields = header.split(',');
  return lines.map((line) => Object.fromEntries(line.split(',').map((value, i) => [fields[i], /^\d+$/.test(value) ? Number(value) : value])));
}

async function readCsv(path) {
  try {
    return parseCsv(await readFile(path, 'utf8'));
  } catch (error) {
    if (error.code === 'ENOENT') return [];
    throw error;
  }
}

function writeCsv(path, fields, rows) {
  return writeFile(path, [fields.join(','), ...rows.map((row) => fields.map((field) => row[field]).join(','))].join('\n') + '\n');
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  if (!process.argv[2]) {
    console.error('Usage: node scripts/stats.mjs <folder>');
    process.exit(1);
  }
  await main(process.argv[2]);
}
