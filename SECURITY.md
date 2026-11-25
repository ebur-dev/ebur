# Security policy

Ebur installs a local certificate authority, runs a small helper with
administrator rights, downloads and runs other software, and can expose a
site to the internet through a share link. We take reports about any of
these seriously.

## Reporting a vulnerability

**Please do not open a public issue.** Report it privately through GitHub:

1. Go to the [Security tab](https://github.com/ebur-dev/ebur/security) of
   this repository.
2. Click **Report a vulnerability**
   ([direct link](https://github.com/ebur-dev/ebur/security/advisories/new)).
3. Describe the problem, how to reproduce it, the Ebur version
   (`ebur --version`) and your Windows version. A proof of concept helps.

Only the maintainers can see the report. We aim to:

- acknowledge it within **7 days**;
- tell you within **14 days** whether we accept it and how we plan to fix it;
- release a fix, then publish an advisory that credits you (unless you
  prefer not to be named).

Please give us a reasonable chance to release a fix before you disclose
the problem publicly. We will keep you informed along the way.

## Supported versions

Ebur is in early development. Only the **latest release** receives security
fixes; please update before reporting.

## In scope

- `ebur-helper.exe`, the only part that runs as administrator: anything
  that lets another user or program make it write more than Ebur's own
  hosts-file entries.
- The local certificate authority: anything that lets its certificates be
  trusted for names other than `.test`, `.localhost` and `localhost`, or
  exposes its private key to other users.
- Downloads: anything that makes Ebur run a file whose SHA-256 does not
  match the manifest, or accept a runtime manifest that is not signed with
  the project's key (or is older than the one it has).
- The control channel between the CLI, the tray app and the background
  host (a per-user named pipe): access from another user account.
- Services listening on anything other than `127.0.0.1` without the user
  asking for it, and share links that stay open after they should have
  ended.
- The release and manifest-signing workflows in `.github/workflows/`.

## Out of scope

- Vulnerabilities in PHP, Nginx, MariaDB, MySQL, Redis, Node.js and the
  other tools Ebur downloads: please report them to those projects. Tell us
  if Ebur's default configuration makes one worse, or if the manifest
  should stop offering an affected version.
- Attacks that need administrator rights, or full control of the user's
  own account, on the computer Ebur runs on.
- Weaknesses in the user's own PHP code that a share link exposes: share
  links publish the site as it is, by design, and end after an hour by
  default.
- Windows SmartScreen and "Unknown publisher" warnings: release builds are
  not code-signed yet.
