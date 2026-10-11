# Security

Circle Lens hooks the mouse and, for a double-tap shortcut, the keyboard, and the desktop app installs itself. Reports about any of that are especially welcome.

## Reporting a vulnerability

Please **don't open a public issue**. Use GitHub's private reporting instead: go to the repository's **Security** tab, then **Report a vulnerability**. Only the maintainer sees it.

Include what you found, how to reproduce it, and what an attacker could do with it. You'll get a reply within a week. We'll agree on a fix and on when to publish it, with credit to you unless you'd rather not have it.

## What it does and doesn't do

- **The keyboard hook** runs only while a double-tap shortcut is set, or while you're recording a shortcut. It looks at which keys go down and up, and never logs, stores or sends them.
- **The screen capture** covers only the area around the pointer, and only while the lens is open. It stays in memory, is never saved, and isn't sent anywhere.
- **Network:** the desktop app makes no network connections. The browser extension makes none either, and asks for host access only so it can capture the visible tab.
- **Installing:** it installs per user, under `%LOCALAPPDATA%\Programs`, and needs no administrator rights.
