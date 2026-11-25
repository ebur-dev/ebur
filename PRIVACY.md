# Privacy policy

**Short version: Ebur does not collect, store or send any data about you, your
computer or your projects.** It has no telemetry, no analytics, no accounts and
no crash reporting.

This policy covers the Ebur software (the tray app, the `ebur` command and its
helper). It does not cover the websites or services Ebur downloads from, which
have their own policies.

## What stays on your computer

Everything Ebur works with lives in its own folder (for example `C:\ebur`) and
never leaves your computer: your sites, databases, snapshots, logs, settings,
generated passwords and the local certificate authority's private key. Ebur
does not read or upload your source code, databases or files.

## What Ebur downloads

Ebur makes outgoing connections only to fetch software and to check for
updates. It sends no information about you or your computer in them. As with
any web request, the server you connect to can see your IP address and the
file you asked for.

- **Runtimes** (PHP, Nginx, MariaDB, MySQL, Node.js, Composer, Redis, Mailpit
  and similar) are downloaded from the official upstream hosts listed in the
  signed runtime manifest. Ebur checks each file's SHA-256 before using it.
- **The runtime manifest** is a public file fetched about once a day from this
  project's GitHub Releases, and its Ed25519 signature is verified. Ebur does
  not report which versions you have installed.
- **Share links** are optional and start only when you ask for one. Ebur then
  runs `cloudflared`, which makes a temporary public address (`*.trycloudflare.com`)
  for the site you chose. Visitors to that address, and Cloudflare, can see the
  traffic to that site. The link ends after the time limit you set (one hour by
  default). See [Cloudflare's privacy policy](https://www.cloudflare.com/privacypolicy/).

## Download counts

GitHub counts how many times each release file is downloaded. That count is
the only usage figure the project sees. It is kept by GitHub, not by Ebur, and
has no information about who downloaded the file.

## Code signing

If release builds are code-signed through the SignPath Foundation, the signing
service receives the release build from this project's build pipeline. It
never receives information about people who use Ebur.

## Children

Ebur is a developer tool and collects no personal data from anyone.

## Changes

If this ever changes, the change will be made in this file, in the open, and
the history is visible in the repository. Any feature that sends data would be
off by default and described here first.

## Contact

Questions about privacy: open an issue at
<https://github.com/ebur-dev/ebur/issues>. For security problems, use the
private route in [SECURITY.md](SECURITY.md).
