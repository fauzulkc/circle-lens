# Privacy policy

Circle Lens is a magnifier. It doesn't collect, store, sell or share personal data, and it has no accounts, analytics, ads or tracking. This policy covers the browser extension, the Windows app and the website script. It was last updated on 11 October 2026.

## Browser extension

- **What it sees:** to draw the lens, it takes a picture of the visible part of the tab you're viewing, and only while the lens is open. The picture stays in that tab's memory, is never saved, and is gone when the lens closes. It is never sent anywhere.
- **What it stores:** your settings (circles to draw, zoom, lens size, the wait between open and close, zooming out when moving fast), in your browser's extension storage. If you use browser sync, your browser syncs them as it does other extension settings.
- **Network:** the extension makes no network requests of its own.
- **Pages:** it runs on the pages you visit so it can see the circle gesture (pointer movement) and draw the lens. It doesn't read or change page content beyond that, and it doesn't record which pages you visit.

## Windows app

- **What it sees:** pointer movement, to recognise the circle gesture. While the lens is open, it also sees the part of the screen around the pointer, to magnify it. Screen pictures stay in memory and are never saved or sent.
- **Keyboard:** if your shortcut is a double tap (such as Shift twice), it watches which keys go down and up, to recognise that tap. It never logs, stores or sends keystrokes.
- **What it stores:** your settings, in `%APPDATA%\CircleLens\settings.json` on your computer.
- **Network:** only for updates, and only once installed.
  - With "Update automatically" on (the default), it asks GitHub's API about the latest Circle Lens release about once a day. When there's a newer version, it downloads that release from GitHub.
  - With it off, it connects only when you press "Check for updates".
  - These requests send what any download sends (your IP address and standard request headers) plus "CircleLens/<version>" as the user agent. GitHub's [privacy statement](https://docs.github.com/en/site-policy/privacy-policies/github-general-privacy-statement) covers how GitHub handles requests it receives.

## Website script

A site that adds the Circle Lens script shows the lens on its own pages. The lens copies the page into itself, in the visitor's browser, while it's open. The script makes no network requests and stores nothing. Loading the script from a CDN (jsDelivr or unpkg) is a request to that CDN, under its own policy.

## Children

Circle Lens collects no data from anyone, children included.

## Changes and contact

Changes to this policy appear in this file, with the date above, and in the [project's history](https://github.com/fauzulkc/circle-lens/commits/main/PRIVACY.md). For questions, [open an issue](https://github.com/fauzulkc/circle-lens/issues). For anything sensitive, use [private security reporting](https://github.com/fauzulkc/circle-lens/security/advisories/new).
