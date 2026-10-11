# Releasing

## Each release

1. Bump `version` in `package.json` and `web/package.json` to the same `X.Y.Z`, and add a `## X.Y.Z` section to CHANGELOG.md. Its text becomes the release notes.
2. Commit, then tag and push: `git tag -a vX.Y.Z -m "Circle Lens X.Y.Z"` and `git push origin main vX.Y.Z`.

The tag starts [the release workflow](.github/workflows/release.yml), which tests, builds and publishes everywhere below. A channel that hasn't been set up yet is skipped with a notice, and never fails the release.

| Channel | What happens | One-time setup |
|---|---|---|
| GitHub release | The setup exe, the extension zip and the website script, each twice: with the version in the name, and without (`/releases/latest/download/CircleLens-Setup.exe` always gives the newest) | none |
| Installed Windows apps | Find the release within a day and update themselves | none |
| npm (`circle-lens`) | The website package is published, with provenance | [trusted publishing](#npm) |
| winget | A pull request with the new version goes to microsoft/winget-pkgs; Microsoft's bots check and merge it | [`WINGET_TOKEN`](#winget) |
| Chrome Web Store | The extension is uploaded and submitted for review, and published once it passes | [`CWS_*` secrets](#chrome-web-store) |

Pinned CDN snippets in the docs (`circle-lens@X.Y.Z`) change only when the website package itself changes, and only once that version is on npm.

## One-time setup

### npm

Trusted publishing lets the workflow publish without a stored token.

1. On npmjs.com, open the `circle-lens` package → **Settings** → **Trusted Publisher** → **GitHub Actions**.
2. Enter: user `fauzulkc`, repository `circle-lens`, workflow filename `release.yml`, and no environment.
3. Optionally, under **Publishing access**, choose "Require two-factor authentication and disallow tokens". From then on, only this workflow (and you, with 2FA) can publish.

An `NPM_TOKEN` secret (a granular token that can publish `circle-lens`) also works, but isn't needed.

### winget

This works once the first version is in winget-pkgs (`store/winget/`, [microsoft/winget-pkgs#450626](https://github.com/microsoft/winget-pkgs/pull/450626)).

1. Create a GitHub **classic** personal access token with only the `public_repo` scope: Settings → Developer settings → Personal access tokens → Tokens (classic). Give it an expiry you'll remember to renew.
2. Save it as the repository secret `WINGET_TOKEN`:
   ```
   gh secret set WINGET_TOKEN --repo fauzulkc/circle-lens
   ```

Each release, Microsoft's `wingetcreate` pushes the updated manifest to your `winget-pkgs` fork and opens the pull request.

### Chrome Web Store

This works once the extension has been published there by hand ([store/chrome-web-store/LISTING.md](store/chrome-web-store/LISTING.md)).

1. In the [Google Cloud console](https://console.cloud.google.com), create a project and enable the **Chrome Web Store API**.
2. **OAuth consent screen:** user type External, with yourself as a test user. Then set its publishing status to **In production**; a refresh token issued while it's in testing stops working after 7 days.
3. **Credentials** → Create credentials → **OAuth client ID** → application type **Desktop app**. Note the client ID and client secret.
4. Get a refresh token. Open this address, with your client ID filled in, and approve:
   ```
   https://accounts.google.com/o/oauth2/auth?response_type=code&access_type=offline&prompt=consent&scope=https://www.googleapis.com/auth/chromewebstore&redirect_uri=http://localhost:8818&client_id=CLIENT_ID
   ```
   The browser then goes to `http://localhost:8818/?code=…`. The page won't load, which is fine: copy the `code` from the address bar and exchange it:
   ```
   curl https://oauth2.googleapis.com/token -d client_id=CLIENT_ID -d client_secret=CLIENT_SECRET -d code=CODE -d grant_type=authorization_code -d redirect_uri=http://localhost:8818
   ```
   The answer's `refresh_token` is the one to keep.
5. Find two IDs in the [developer dashboard](https://chrome.google.com/webstore/devconsole): the **publisher ID** (Account → Publisher ID) and the extension's **item ID**.
6. Save the five secrets:
   ```
   gh secret set CWS_CLIENT_ID --repo fauzulkc/circle-lens
   gh secret set CWS_CLIENT_SECRET --repo fauzulkc/circle-lens
   gh secret set CWS_REFRESH_TOKEN --repo fauzulkc/circle-lens
   gh secret set CWS_PUBLISHER_ID --repo fauzulkc/circle-lens
   gh secret set CWS_EXTENSION_ID --repo fauzulkc/circle-lens
   ```

Each release uploads `circle-lens-extension-X.Y.Z.zip` and submits it for review. Google publishes it once the review passes, usually within a few days. The manifest's version comes from `package.json`, so it always goes up.

### Edge Add-ons and Firefox Add-ons

Not automated yet. Both take the same extension zip, and both have publishing APIs that a job like the Chrome one could use.

## Download numbers

[The stats workflow](.github/workflows/stats.yml) reads the public numbers once a day and saves them to the [`stats` branch](https://github.com/fauzulkc/circle-lens/tree/stats), whose README sums them up. GitHub keeps only lifetime totals, so that branch is the history. Run it by hand from the Actions tab, or locally with `node scripts/stats.mjs <folder>`.

- **New downloads** are the assets without a version in the name (`CircleLens-Setup.exe`, `circle-lens-extension.zip`): every download link gives those.
- **Installed copies still in use:** each copy with automatic updates on downloads `CircleLens-Setup-<version>.exe` once per version. So a week or so after a release, that file's count is roughly the copies used that week, with no ping from the app. The count also includes winget, the release page and your own copy.
- **npm and jsDelivr** have their own daily counts, which the workflow saves too.

Repository traffic (views, visitors, referrers) needs one more secret, because the workflow's own token can't read it:

1. Create a fine-grained personal access token: Settings → Developer settings → Personal access tokens → Fine-grained tokens. Repository access: only `circle-lens`. Permissions: **Administration: Read-only**. Give it an expiry you'll remember to renew.
2. Save it as the repository secret `TRAFFIC_TOKEN`:
   ```
   gh secret set TRAFFIC_TOKEN --repo fauzulkc/circle-lens
   ```
