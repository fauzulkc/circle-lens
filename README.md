# Circle Lens download numbers

Read on 2026-10-21 (UTC) by the [stats workflow](https://github.com/fauzulkc/circle-lens/blob/main/.github/workflows/stats.yml), once a day. GitHub keeps only lifetime totals, so the CSV files here are the history, which starts on 2026-10-11.

## Downloads

| | Last 7 days | Last 30 days | All time |
|---|--:|--:|--:|
| Windows app, from the download links | 80 | — | 201 |
| Browser extension, from the download links | 12 | — | 40 |
| Website script, from the releases | 0 | — | 2 |
| Website package, from npm | 18 | 700 | 700 |
| Website script, loads through jsDelivr | 89 | 110 | 110 |

- **The download links** are `releases/latest/download/CircleLens-Setup.exe` and `circle-lens-extension.zip`, the ones the website, README and llms.txt give: someone getting it, or getting it again.
- The release rows show a week or a month once the history goes back that far.
- npm counts include mirrors and bots. jsDelivr counts loads of the script on other sites' pages, not sites. Both report a day or two late.

## Installed apps still in use

A copy with "Update automatically" on (the default) downloads each new version once, as `CircleLens-Setup-<version>.exe`. So once a version has been the latest for a week or so, its update downloads are about the number of copies used that week. A copy that skips a version, because it was off or a newer one came out first, isn't in that version's count. The counts also include winget installs, winget's validation bots, downloads from the release page and the maintainer's own copy.

| Version | Released | Latest for | Update downloads | App, from the links | Extension, from the links |
|---|---|--:|--:|--:|--:|
| v1.3.0-preview.2 | 2026-10-13 | 8 days so far | 0 | 0 | 0 |
| v1.3.0-preview.1 | 2026-10-12 | 2 h | 0 | 0 | 0 |
| v1.2.3 | 2026-10-11 | 25 h | 19 | 161 | 32 |
| v1.2.2 | 2026-10-11 | under 1 h | 9 | 5 | 1 |
| v1.2.1 | 2026-10-11 | under 1 h | 5 | 10 | 2 |
| v1.2.0 | 2026-10-11 | 1 h | 6 | 7 | 2 |
| v1.1.0 | 2026-10-11 | under 1 h | 5 | 3 | 0 |
| v1.0.0 | 2026-10-11 | under 1 h | 4 | 3 | 0 |
| v0.9.0 | 2026-10-11 | under 1 h | 4 | 6 | 1 |
| v0.8.0 | 2026-10-11 | under 1 h | 5 | 2 | 0 |

## Repository

30 stars, 0 forks, 0 watching.

Traffic (views, visitors, referrers) isn't saved: it needs the `TRAFFIC_TOKEN` secret ([RELEASING.md](https://github.com/fauzulkc/circle-lens/blob/main/RELEASING.md#download-numbers)).
