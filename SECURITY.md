# Security

Circle Lens hooks the mouse and, for a double-tap shortcut, the keyboard, and the desktop app installs itself. Reports about any of that are especially welcome.

## Reporting a vulnerability

Please **don't open a public issue**. Use GitHub's private reporting instead: go to the repository's **Security** tab, then **Report a vulnerability**. Only the maintainer sees it.

Include what you found, how to reproduce it, and what an attacker could do with it. You'll get a reply within a week. We'll agree on a fix and on when to publish it, with credit to you unless you'd rather not have it.

## What it does and doesn't do

- **The keyboard hook** runs only while a double-tap shortcut is set, or while you're recording a shortcut. It looks at which keys go down and up, and never logs, stores or sends them.
- **The screen capture** covers only the area around the pointer, and only while the lens is open. It stays in memory, is never saved, and isn't sent anywhere.
- **Network:** the desktop app's only connections are for updates, and only once installed. With "Update automatically" on (the default), it asks GitHub's API about the latest release of this repository about once a day. When there is a newer one, it downloads that release's setup from this repository's GitHub releases. The requests carry nothing about you beyond what any download does, plus the app's name and version as its user agent. With the switch off, it connects only when you press "Check for updates". The browser extension makes no connections, and asks for host access only so it can capture the visible tab.
- **Updates are checked before they run.** A download must come from this repository's releases, and must match the size and SHA-256 digest GitHub publishes for that release, or it's deleted unrun. The new program must also report the expected version. The exe isn't code-signed yet, so this check, over HTTPS, is what stands between a release and your computer. Anyone who can publish releases to this repository can ship an update.
- **Installing:** it installs per user, under `%LOCALAPPDATA%\Programs`, and needs no administrator rights.

## If your antivirus flags it

A new, unsigned build is sometimes flagged by antivirus machine learning (a false positive). Report it here with the detection name and the file's SHA-256. The README's "Your antivirus flagged it?" section shows how to check that a file is the real release: its checksum, and its signed build attestation.
