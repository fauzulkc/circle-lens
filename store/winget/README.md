# winget

`manifests/` holds the first submission of Circle Lens to the [Windows Package Manager community repository](https://github.com/microsoft/winget-pkgs), in the layout that repository uses. Once it's accepted, anyone can install it with:

```
winget install circle-lens
```

or `winget install FauzulChowdhury.CircleLens`, and `winget upgrade` finds new versions. The app also updates itself, so either way works.

## How the installer behaves under winget

- `CircleLens-Setup-<version>.exe -install` installs per user, without asking or needing administrator rights, then starts the app. The setup process exits straight after, which is when winget reports success.
- winget recognises the installed app by its Settings → Apps entry (`ProductCode: CircleLens`). It uninstalls through that entry's `QuietUninstallString` (`circle-lens.exe -uninstall -quiet`).
- When the app updates itself, it rewrites that entry's version, so `winget list` stays accurate.

## First submission (by hand, once)

1. Check the manifests: `winget validate --manifest store\winget\manifests\f\FauzulChowdhury\CircleLens\0.6.0`
2. Optionally, try an install from them. This needs `winget settings --enable LocalManifestFiles`, run once as administrator. Then: `winget install --manifest store\winget\manifests\f\FauzulChowdhury\CircleLens\0.6.0`
3. Submit them. Either:
   - **with wingetcreate:** `wingetcreate submit --token <token> store\winget\manifests\f\FauzulChowdhury\CircleLens\0.6.0`, where the token is a GitHub classic token with the `public_repo` scope. wingetcreate forks winget-pkgs to your account and opens the pull request; or
   - **by hand:** fork `microsoft/winget-pkgs`, copy `manifests/f/FauzulChowdhury/CircleLens/0.6.0/` into the same path in your fork, and open a pull request.
4. Microsoft's bots validate and test-install it, usually within a day. A moderator then merges it. If a bot asks a question, it's on the pull request.

## Later versions (automatic)

Once the package is in winget-pkgs, the release workflow submits each new version itself with Microsoft's `wingetcreate update`. It needs the `WINGET_TOKEN` secret ([RELEASING.md](../../RELEASING.md#winget)); without it, that job is skipped. Only the first submission needs these files; later ones are made from the published package.
