# Ebur — Software Requirements Specification

Nov 24, 2026 · @Ivan

## 1. Introduction

Ebur is a free, open-source local PHP development environment: one portable folder, one tray app and one CLI that run PHP, a web server and a database with per-project versions and automatic `.test` domains.

### 1.1 Purpose

This SRS defines what Ebur must do for its MVP, v1 and v2 releases. It is the reference for contributors, reviewers and testers, and each requirement carries an ID so issues and pull requests can cite it.

### 1.2 Scope

In scope: downloading and switching PHP, web server and database binaries; generating configs; managing services; virtual hosts and local HTTPS; project scaffolding; developer tools such as mail capture, log viewing and public sharing.

Out of scope: production hosting, deployment pipelines, container orchestration, and bundling a code editor.

### 1.3 Definitions

| Term | Meaning |
| --- | --- |
| Ebur root | The single install folder, default `C:\ebur\` |
| Runtime | A downloaded binary package (PHP, Nginx, Apache, MariaDB, Node, etc.) at a specific version |
| Active runtime | The version used by default when a project does not pin one |
| Site | A folder under `www\` served at `<folder>.test` |
| `.ebur` file | Optional per-project config that pins runtime versions and settings |
| Core | The Go package holding all business logic, shared by the CLI and GUI |
| MVP / v1 / v2 | Release stages defined in section 7 |

### 1.4 Conventions

Requirement IDs use the pattern `AREA-NN` (for example `PHP-03`). Priority is **Must** (required for the stated release), **Should** (planned for that release, can slip) or **Could** (nice to have). "Shall" marks a binding requirement.

### 1.5 References

- PHP for Windows official builds: windows.php.net
- Nginx Windows builds: nginx.org
- Apache Windows builds: apachelounge.com (Apache publishes no official Windows binaries)
- MariaDB downloads: mariadb.org
- curl CA bundle (Mozilla roots for PHP, SSL-04): curl.se/docs/caextract.html
- Mailpit (mail capture): github.com/axllent/mailpit
- Wails (Go desktop framework): wails.io

## 2. Overall description

Ebur replaces Laragon for developers who want the same one-folder convenience under an open-source licence, and adds per-project runtime versions, mail capture and public sharing.

### 2.1 Product perspective

Ebur is a standalone desktop application. It does not compile PHP or servers itself; it downloads official portable builds, writes their config files from templates, and supervises their processes.

| Capability | Laragon | Ebur |
| --- | --- | --- |
| Licence | Paid for commercial use | Open source, free for all use |
| PHP version | One active version globally | Global default plus per-project pinning |
| Auto `.test` hosts | Yes | Yes |
| Local HTTPS | Yes | Yes, via mkcert |
| Mail capture | No (plugin) | Built in (Mailpit) |
| Public share link | Via ngrok setup | One click via tunnel |
| CLI | Limited | Full CLI with the same features as the GUI |
| Platforms | Windows | Windows first; macOS and Linux later |

### 2.2 User classes

| User | Needs | Priority |
| --- | --- | --- |
| PHP / Laravel developer | Fast project setup, version switching, HTTPS | Primary |
| WordPress / CMS builder | One-click WordPress, database, little config | Primary |
| Student or beginner | Works out of the box, clear errors | Secondary |
| Team lead | Same environment on every machine via `.ebur` files | Secondary |
| Contributor | Readable code, clear module boundaries | Secondary |

### 2.3 Operating environment

- Windows 10 (21H2+) and Windows 11, x64 and ARM64. Ebur itself builds natively for ARM64, but official PHP and MariaDB Windows builds are x64 only, so on ARM64 those runtimes run under Windows x64 emulation.
- WebView2 runtime (bundled with Windows 11; bootstrapped on Windows 10 if missing).
- Internet access only for downloading runtimes and for tunnels; everything else works offline.
- macOS 13+ and Ubuntu 22.04+ are targeted for v2.

### 2.4 Design constraints

- Core and CLI written in Go; GUI built with Wails using the system webview.
- Licence: MIT (decided), compatible with redistributing the bundled tools under their own licences. Repository: `github.com/ebur-dev/ebur`.
- The product name and binaries shall not use the word "PHP" (PHP licence naming restriction).
- No writes to the Windows registry or system `PATH`; only the hosts file needs elevation. One deliberate exception: the installer writes the standard per-user uninstall entry (HKCU) so Ebur appears in Settings > Apps; the portable zip writes nothing. The local CA goes into the current user's certificate store, which needs no admin rights but shows its own Windows confirmation dialog.

### 2.5 Assumptions and dependencies

- Official binary sources keep publishing portable zips with checksums.
- Users accept a one-time admin prompt for hosts-file edits and CA installation.
- Mailpit and cloudflared remain available under permissive licences.

## 3. Functional requirements

The MVP needs 23 Must requirements across runtimes, services, sites and the two interfaces; everything else layers on top of that core.

### 3.1 Runtime manager (RUN)

| ID | Requirement | Priority | Release |
| --- | --- | --- | --- |
| RUN-01 | The system shall list available versions of each runtime from a versioned manifest hosted in the Ebur repo. | Must | MVP |
| RUN-02 | The system shall download a runtime zip from its official source and verify its SHA-256 checksum before extracting. | Must | MVP |
| RUN-03 | The system shall extract each runtime to `bin\<runtime>\<version>\` without touching system folders. | Must | MVP |
| RUN-04 | The system shall remove an installed version on request, refusing if a running site uses it. | Must | MVP |
| RUN-05 | The system shall show download progress and resume interrupted downloads. Implemented Nov 25, 2026: a download writes to `data\downloads\<runtime>-<version>.zip.part` with a note naming its URL and SHA-256; a dropped connection is retried up to 3 times within the install (2, 4, 6 s apart) and resumed with an HTTP Range request, and a later install (after Ctrl+C, a crash or a reboot) resumes the same file. A partial from another release is discarded; a server that ignores ranges restarts cleanly; "not found" and full-disk errors are not retried. The whole file is still SHA-256-checked before anything is installed, and a file that fails is deleted. Verified live: a 248 MB MySQL download killed at 74 MB resumed from 74 MB. | Should | v1 |
| RUN-06 | The system shall let users add a runtime from a local zip or folder. | Could | v1 |

### 3.2 PHP management (PHP)

| ID | Requirement | Priority | Release |
| --- | --- | --- | --- |
| PHP-01 | The system shall let the user set a global active PHP version from the tray, GUI or `ebur php <version>`. | Must | MVP |
| PHP-02 | Switching versions shall regenerate web server config and restart affected services within 5 seconds. | Must | MVP |
| PHP-03 | The system shall generate `php.ini` from a template on first install, preserving user edits on later switches. | Must | MVP |
| PHP-04 | A project's `.ebur` file or `composer.json` PHP constraint shall select its PHP version, running multiple versions at once via separate PHP-FPM / CGI pools. Implemented Nov 25, 2026: `.ebur` first, then `composer.json` (the default version is kept when it satisfies the constraint, so fewer pools run), then the default; one php-cgi pool per version in use; `ebur exec` and the terminal use the project's version inside a site folder; a version that cannot be met falls back to the default with a warning. | Must | v1 |
| PHP-05 | The system shall list, enable and disable PHP extensions without manual ini editing. | Should | v1 |
| PHP-06 | The system shall toggle Xdebug on or off per PHP version in one action. | Should | v1 |
| PHP-07 | The system shall bundle Composer and expose it on the Ebur terminal PATH. | Must | MVP |

### 3.3 Services (SVC)

| ID | Requirement | Priority | Release |
| --- | --- | --- | --- |
| SVC-01 | The system shall start, stop and restart the web server, MariaDB/MySQL and PHP processes individually or all at once. The MVP ships Nginx only. Nov 25, 2026: Nginx on Windows takes about 5 s to exit after `nginx -s stop` while any connection is mid-request, so Ebur ends it (and its workers) if it has not exited 1 s after stop; a restart with half-sent or slow requests open now takes 2.7 s instead of 6.7 s. MySQL added Nov 25, 2026, side by side with MariaDB (owner's choice): MySQL 8.4 LTS, plus 8.0 and 5.7 as end-of-life legacy versions, from Oracle's official Windows ZIPs (each checked against Oracle's MD5 and GPG signature before its SHA-256 was pinned; debug files trimmed, about 400 MB installed). MariaDB and one MySQL version run at once, each on its own port (MySQL takes the first free port from 3306), with the shared root password; `ebur mysql <version>` switches MySQL versions, each series keeping its own data folder; `ebur db default mysql` (or the wizard's MySQL choice) makes new projects use MySQL, and `ebur new --db mysql` picks per project. Snapshots, `ebur db list` and the Databases tab tell the servers apart by each site's DB_PORT. | Must | MVP |
| SVC-02 | The system shall let the user choose Nginx or Apache as the web server. | Must | v1 |
| SVC-03 | The system shall detect port conflicts (80, 443, 3306) before start and name the process holding the port. | Must | MVP |
| SVC-04 | The system shall restart a crashed service up to 3 times, then report the failure with its last log lines. | Should | MVP |
| SVC-05 | The system shall support PostgreSQL, Redis and Memcached as optional services. Redis implemented Nov 25, 2026 (PostgreSQL and Memcached still to do): Redis 8.10.3, the MSYS2 build by the redis-windows project (built in public by GitHub Actions from the official source; SHA-256 from the release), opt-in through `ebur install redis`, the Settings card or the setup wizard (`--redis`). It runs on 127.0.0.1:6379 only from `etc\redis\redis.conf` (relative paths: the MSYS2 build cannot read `C:/` paths), keeps data in `data\redis\`, and saves it on stop. The phpredis 6.3.0 extension (PECL; php.net publishes no checksums, so Ebur pins its own) is installed per PHP series 7.4–8.5 and loaded through `conf.d\ebur-redis.ini`; PHP 8.6 has only a release candidate, so the doctor and the Settings card point to predis instead. | Should | v1 |
| SVC-06 | The system shall optionally start chosen services when Ebur launches. | Should | MVP |

### 3.4 Sites and virtual hosts (SITE)

| ID | Requirement | Priority | Release |
| --- | --- | --- | --- |
| SITE-01 | Every folder in `www\` shall be served at `<folder>.test` with no manual setup. | Must | MVP |
| SITE-02 | The system shall add and remove hosts-file entries through a small elevated helper, prompting once per change batch. If another process locks the file, the helper shall retry, then name the locking process and explain how to allow Ebur. The helper accepts only names ending in `.test` or `.localhost`, always maps them to 127.0.0.1, and edits only the `# ebur begin` / `# ebur end` block. After an automatic attempt is declined or blocked, starts do not prompt again for the same names. | Must | MVP |
| SITE-03 | The system shall detect a site's document root (`public\`, `web\` or the folder itself). | Must | MVP |
| SITE-04 | The user shall be able to change the TLD (default `.test`) and add custom domains per site. Needs a design for the elevated helper, which only accepts `.test` and `.localhost` so it cannot be used to redirect real sites; and the local CA is name-constrained to the TLD, so a TLD change means a new CA. TLD choice implemented Nov 25, 2026 (custom domains per site still to do): `.test` (default) or `.localhost`, via `ebur tld`, `ebur setup --tld`, the setup wizard and Settings. `.localhost` needs no hosts file (browsers send `*.localhost` to 127.0.0.1 themselves) and no new CA (the existing CA already permits `localhost`), so it suits PCs where security software locks the hosts file; Ebur detects that case (Webroot's WRSVC, a protected holder, or a blocked sync) and the wizard then suggests `.localhost`. Limit, shown in the wizard, Settings, `ebur tld` and the doctor: Windows' resolver (used by PHP) does not resolve `.localhost`, so a site cannot call its own address. Switching warns that WordPress sites keep their old address in the database. | Should | v1 |
| SITE-05 | The system shall watch `www\` and apply new or removed sites without a restart. Implemented Nov 25, 2026: also reacts to a new `public\` folder and to `.ebur` and `composer.json` changes; Nginx reloads gracefully, only affected PHP pools start or stop (each version keeps a fixed port block), unused pools retire after a grace period, and MariaDB is never restarted. | Should | v1 |
| SITE-06 | A project kept outside `www\` shall be servable without moving it. Implemented Nov 25, 2026: `ebur link [folder] [--name]` and the window's *Add existing project* make a directory junction in `www\`; `ebur unlink` (or *Unlink*) removes only the junction and refuses real folders; links to network shares, into `www\` or around the Ebur folder are refused; a missing target is reported, not served; `.ebur`, `composer.json`, `ebur exec` and the terminal work from the project's real folder. | Should | v1 |

### 3.5 Local HTTPS (SSL)

| ID | Requirement | Priority | Release |
| --- | --- | --- | --- |
| SSL-01 | The system shall create and install a local CA on first run, with user consent. The CA is generated by Ebur itself and name-constrained to the local TLD, localhost and loopback addresses, so a leaked key cannot impersonate public sites. | Must | MVP |
| SSL-02 | The system shall issue a certificate per site so `https://<site>.test` loads without browser warnings. | Must | MVP |
| SSL-03 | The system shall let the user uninstall the local CA. | Must | MVP |
| SSL-04 | Generated `php.ini` files shall set `curl.cainfo` and `openssl.cafile` to a bundle of public root CAs plus the Ebur CA, so PHP code and Composer trust local sites. PHP uses OpenSSL, not the Windows certificate store. | Must | MVP |

### 3.6 Projects (PROJ)

| ID | Requirement | Priority | Release |
| --- | --- | --- | --- |
| PROJ-01 | The system shall create a new Laravel, WordPress, Symfony or blank project with `ebur new <name> --<type>` or from the GUI. Implemented Nov 25, 2026: `ebur new <name> [--laravel\|--wordpress\|--symfony\|--blank] [--php 8.3]` and the Sites tab's *New project…* dialog. Laravel (`laravel/laravel`) and Symfony (`symfony/skeleton`) come through Composer; WordPress is the latest release from wordpress.org, checked against its published SHA-1 and cached in `data\downloads`. The project is built in `www\.ebur-new-<name>` (never served) and moved into place only when complete; a failure or cancel removes it. `--php` pins the version in `.ebur`. | Should | v1 |
| PROJ-02 | Project creation shall create a database and fill `.env` / `wp-config.php` with its credentials. Implemented Nov 25, 2026: a MariaDB database named after the site (hyphens become underscores); Ebur refuses to reuse an existing database and drops only one it created itself when a later step fails. Laravel: `.env` DB settings, `APP_URL` and Mailpit SMTP, migrated into MariaDB by Laravel's own setup script. Symfony: `DATABASE_URL`, `DEFAULT_URI` and `MAILER_DSN` in `.env.local`. WordPress: `wp-config.php` with fresh secret keys and `WP_ENVIRONMENT_TYPE` `local`; the user finishes the install (title, admin) in the browser. | Should | v1 |
| PROJ-03 | The system shall bundle Node.js and let users switch Node versions like PHP. Implemented Nov 25, 2026: LTS lines 26, 24 and 22 from nodejs.org (SHA-256 from SHASUMS256.txt), 20 as legacy; `ebur node [version\|install\|list\|remove]`, a Node card in Settings and an optional wizard step. A site's Node comes from `.ebur` (`node: 22`, `ebur sites node`), then `.nvmrc` / `.node-version` (an exact patch maps to the same major line), then `package.json` `engines.node`, then the default. Node is put on PATH only in Ebur's terminal and `ebur exec`, never the system PATH. New Laravel projects get `VITE_DEV_SERVER_KEY`/`_CERT` pointing at the site's certificate, so `npm run dev` serves over HTTPS. | Should | v1 |
| PROJ-04 | The system shall support community project templates from a manifest. | Could | v2 |

### 3.7 Developer tools (TOOL)

| ID | Requirement | Priority | Release |
| --- | --- | --- | --- |
| TOOL-01 | The system shall bundle Adminer and serve it at `db.test`. | Must | MVP |
| TOOL-02 | The system shall show live-tailing logs for PHP, the web server and the database in one viewer. | Should | MVP |
| TOOL-03 | The system shall run Mailpit and configure PHP's mail settings so all mail lands at `mail.test`. Implemented Nov 25, 2026: Mailpit 1.34.0 bound to 127.0.0.1 only (its own default is all interfaces), no online version checks, any SMTP login accepted, mail kept across restarts; PHP's `mail()` is pointed at it through a managed `ebur-mail.ini`; `ebur mail` and the window show Laravel and Symfony settings. | Should | v1 |
| TOOL-04 | The system shall expose a site publicly through a Cloudflare tunnel with `ebur share <site>` and show the URL. Implemented Nov 25, 2026: Cloudflare quick tunnels (no account; random `*.trycloudflare.com` address, new each time) via `cloudflared` 2026.11.1, downloaded on first use and checked by SHA-256, auto-update off. `ebur share <site> [--for 1h] [--password]`, `ebur share` (list), `ebur share stop <site\|all>`, and Share/Stop on the Sites tab. Decided with the owner: shares end after 1 hour by default (max 24 h), when stopped, or when the web server stops or restarts; `--password` adds an Nginx login with a generated password. Each share has its own loopback-only Nginx server, so only that site is reachable (not Adminer, Mailpit, the database or other sites); PHP sees HTTPS, the public host name, `EBUR_SHARE=1` and `APP_DEBUG=false` (Laravel and Symfony debug pages stay off for visitors). WordPress sites get `wp-content/mu-plugins/ebur-share.php`, which switches WordPress to the share address only for shared visits and never touches the database or `wp-config.php`. | Should | v1 |
| TOOL-05 | The system shall save and restore named database snapshots. Implemented Nov 25, 2026: `ebur db snapshot <site\|database> [label]`, `ebur db snapshots`, `ebur db restore <site\|database> <snapshot\|label\|latest>`, `ebur db delete-snapshot`, `ebur db list`, and a Databases tab. Snapshots are gzip-compressed `mariadb-dump --single-transaction` files (with routines, triggers and events) in `data\snapshots\<database>\`, named by date, time and label. A site maps to its database through its own settings (Laravel `.env`, Symfony `DATABASE_URL`, WordPress `wp-config.php`). A restore checks the file first, saves the current data as a `before-restore` snapshot (so every restore can be undone), and puts that data back by itself if loading fails. MariaDB's system databases are refused. | Should | v1 |
| TOOL-06 | The system shall capture `dump()` / `dd()` output in a side panel. | Could | v2 |

### 3.8 Tray app and GUI (GUI)

| ID | Requirement | Priority | Release |
| --- | --- | --- | --- |
| GUI-01 | The system shall run as a tray icon whose colour shows whether all services are running, some are, or none. | Must | MVP |
| GUI-02 | The tray menu shall start/stop all, switch PHP version, and open a site, the `www\` folder or a terminal. | Must | MVP |
| GUI-03 | The main window shall list sites with their URL, PHP version and status. | Must | MVP |
| GUI-04 | The system shall open a terminal with Ebur runtimes on PATH, without changing the system PATH. | Must | MVP |
| GUI-05 | The GUI shall support light and dark themes following the OS setting. | Should | v1 |

### 3.9 Command-line interface (CLI)

| ID | Requirement | Priority | Release |
| --- | --- | --- | --- |
| CLI-01 | Every GUI action shall have an equivalent `ebur` command. | Must | MVP |
| CLI-02 | Commands shall support `--json` output for scripts and AI coding tools. Implemented Nov 25, 2026 for the commands that report state: `status`, `sites`, `doctor`, `db`, `db list`, `db snapshots`, `share` (list, and the new share with its address), `php list`, `node list`. Stdout carries exactly one JSON document (progress text goes to stderr); errors are `{"error", "next_step"}` with exit code 1; `doctor --json` keeps exit code 1 when it finds problems; other commands refuse `--json` and name the ones that support it. | Should | v1 |
| CLI-03 | `ebur doctor` shall check ports, certificates, hosts entries and runtimes, and suggest fixes. | Should | MVP |

### 3.10 Updates (UPD)

| ID | Requirement | Priority | Release |
| --- | --- | --- | --- |
| UPD-01 | Ebur shall check for its own updates and install them only with user approval. | Should | v1 |
| UPD-02 | Updates shall be signed and verified before replacing the binary. | Must | v1 |

### 3.11 Security advisories (SEC)

| ID | Requirement | Priority | Release |
| --- | --- | --- | --- |
| SEC-01 | The runtime manifest shall record each version's support status (active, security-only, end of life), end-of-life date, minimum safe patch and related advisories. Partly implemented Nov 25, 2026: every PHP release carries its end-of-life date; legacy PHP 8.1, 8.0 and 7.4 are installable for old projects with warnings at install, in `ebur php list`, `ebur sites`, the Sites tab and `ebur doctor`, and are not offered by first-run setup. Advisories and minimum safe patch remain. | Should | v1 |
| SEC-02 | Ebur shall fetch the manifest on startup and daily, cache it, and use the cached copy when offline. Implemented Nov 25, 2026 (core/catalog): the host checks 2 minutes after start and then daily; `ebur manifest` shows the list in use, `ebur manifest refresh` checks now, and `ebur doctor` reports it. A downloaded list is used only if its Ed25519 signature verifies against a public key compiled into Ebur and it is newer (`generated`) than the built-in and cached ones, so tampered or replayed lists are refused; the cache in `data\manifest\` is re-verified on every load. A missing list (nothing published yet, or a private repository) is not an error. Only a plain HTTPS GET is sent. `"manifest_url": "off"` turns checking off. | Should | v1 |
| SEC-03 | Ebur shall badge every installed runtime as supported, security-only / end of life within 6 months, or vulnerable / end of life, and show a warning dot on the tray icon for the last case. | Should | v1 |
| SEC-04 | Ebur shall offer a one-click update to the latest patch of the same minor version and repoint affected sites. | Should | v1 |
| SEC-05 | `ebur audit` and the site list shall report vulnerable project packages using `composer audit` and `npm audit`. | Should | v1 |
| SEC-06 | `ebur share` shall ask for confirmation before exposing a site that runs a vulnerable or end-of-life runtime. | Must | v1 |
| SEC-07 | Ebur shall never upgrade a runtime without user approval, and shall let users snooze warnings per runtime. | Must | v1 |
| SEC-08 | A scheduled CI job shall refresh security data from endoflife.date, official release notes and OSV.dev / NVD, open a pull request for maintainer review, and publish a signed manifest that clients verify. Signing and publishing implemented Nov 25, 2026: `.github/workflows/publish-manifest.yml` signs the manifest with the Ed25519 key in the `MANIFEST_SIGNING_KEY` secret (made by `build/manifest-sign keygen`; it refuses a key that does not match core/catalog's public key) and publishes it with its signature as assets of the "manifest" pre-release whenever it changes on main; clients verify it (SEC-02). OSV.dev/NVD data is still to do. Earlier, Nov 25, 2026: `.github/workflows/manifest-update.yml` runs `build/manifest-update` daily. It updates Node (LTS lines only, nodejs.org + SHASUMS256.txt), PHP (windows releases.json SHA-256, php.net "security" tag), Composer, Mailpit, cloudflared, Redis and Adminer (GitHub release digests), with end-of-life dates from endoflife.date; a release waits 14 days unless marked as a security fix; each series keeps its newest eligible patch, and new series are added only for supported Node LTS lines and new PHP minors. MySQL, MariaDB and Nginx updates are reported for a manual, signature-checked update. It opens or updates one pull request; every run's report is on its summary page. | Must | v1 |
| SEC-09 | The repo shall include `SECURITY.md` and accept private vulnerability reports through GitHub Security Advisories. | Should | MVP |

Security checks download a public file only; Ebur never sends installed versions or project data anywhere (NFR-17).

## 4. External interface requirements

Ebur talks to users through a tray app, a window and a CLI, and to the OS through processes, config files, the hosts file and the certificate store.

### 4.1 User interfaces

- **Tray icon:** status colour (green all running, amber partial, grey stopped) and a menu for the most common actions.
- **Main window:** tabs for Sites, Runtimes, Services, Logs and Settings. Every error shows a plain-language message and a suggested fix.
- **First-run wizard:** choose web server, PHP version and database, then approve the CA and hosts-file prompts.

### 4.2 Command-line interface

The `ebur` binary follows `ebur <noun|verb> [args] [--flags]`. Exit code 0 means success; non-zero codes are documented per command.

```
ebur start | stop | restart [service]
ebur php [version]            # show or set global PHP
ebur php install 8.3
ebur sites                    # list sites, URLs, versions
ebur link D:\code\shop        # serve a folder outside www\
ebur new shop --laravel
ebur share shop               # public tunnel URL
ebur db snapshot shop
ebur doctor
```

### 4.3 Software interfaces

| Interface | Used for | Direction |
| --- | --- | --- |
| Runtime manifest (JSON over HTTPS) | Versions, URLs and SHA-256 hashes | Read |
| Official download hosts | Runtime zips | Read |
| Nginx / Apache / php-cgi / MariaDB | Process start, stop and logs. Windows has no Unix signals, so each service stops its own way: `nginx -s quit`, `mysqladmin shutdown`, process termination for php-cgi | Control |
| Windows certificate store (current user, Root) | Trust or remove the Ebur CA | Control |
| cloudflared | Public tunnels | Control |
| Mailpit | SMTP capture on 127.0.0.1:1025, UI on mail.test | Control |

### 4.4 OS interfaces

- **Hosts file** (`C:\Windows\System32\drivers\etc\hosts`): Ebur writes only inside a marked `# ebur begin` / `# ebur end` block.
- **Certificate store:** the local CA is added to the current user's trusted roots (no elevation; Windows shows its own confirmation).
- **Processes:** started as child processes of the Ebur service supervisor, never as Windows services, so nothing runs after Ebur quits unless the user opts in. Children are placed in a Windows Job Object that kills them if Ebur crashes, so orphaned processes never keep ports 80 or 3306 busy.
- **Network:** services bind to 127.0.0.1 by default; LAN binding is an explicit setting.

### 4.5 Visual design

Ebur bundles Inter for UI text and JetBrains Mono for code, so it looks the same on Windows, macOS and Linux; all fonts are SIL Open Font License, compatible with MIT.

| Use | Font | Fallback stack | Notes |
| --- | --- | --- | --- |
| UI text (tray menus, window, settings) | Inter (variable) | `Inter, "Segoe UI Variable", system-ui, sans-serif` | Tabular numbers on for version strings; about 300 KB |
| Code, logs, terminal, paths | JetBrains Mono | `"JetBrains Mono", Consolas, ui-monospace, monospace` | Ligatures off by default in logs |
| Logo and wordmark | Space Grotesk or Manrope | Inter | Logo and window title only, never body text |

- Fonts ship inside the app as WOFF2 files; nothing loads from the network.
- Font licences are listed in `THIRD_PARTY.md` (NFR-16).

The colour palette pairs warm ivory with deep grey and one gold accent, tying the brand to the name (ebur is Latin for ivory).

| Role | Name | Hex | Use |
| --- | --- | --- | --- |
| Base | Ebur Ivory | `#F6F1E4` | Light backgrounds, logo on dark |
| Ink | Elephant Grey | `#2B2F33` | Text, logo on light, dark-mode background |
| Accent | Savanna Gold | `#D89B2B` | Buttons, links, the running highlight |
| Status | Grass Green | `#3E9B5F` | Tray icon when all services run |

Alternative accent under consideration: teal `#1F8A8A`, for a cooler developer-tool feel.

- [ ] Pick the wordmark font: Space Grotesk or Manrope.

* [ ] Confirm the accent colour: Savanna Gold or teal.

## 5. Non-functional requirements

Ebur's selling point is being light: under 20 MB to download, under 60 MB of RAM for the app itself, and every service up in under 3 seconds.

| ID | Category | Requirement | Measure |
| --- | --- | --- | --- |
| NFR-01 | Footprint | Ebur installer (without runtimes). Measured Nov 25, 2026: installer 6.9 MB, portable zip 9.5 MB | ≤ 20 MB |
| NFR-02 | Footprint | Idle memory (working set, as Task Manager shows it) of tray app with the GUI closed. Closing the window must release the WebView2 instance, not just hide it | ≤ 60 MB |
| NFR-03 | Performance | Cold start of all MVP services on an SSD, excluding first-run database initialisation | ≤ 3 s |
| NFR-04 | Performance | Global PHP version switch, including restart | ≤ 5 s |
| NFR-05 | Performance | New site reachable after folder creation (v1 watcher), excluding time spent on the hosts-file admin prompt. Measured Nov 25, 2026: new site 1.2–1.4 s; pinning or unpinning a site's PHP 1.2–1.6 s, with no failed or hanging requests | ≤ 2 s |
| NFR-06 | Security | Every downloaded runtime verified against SHA-256 before extraction | 100% of downloads |
| NFR-07 | Security | Services bind to 127.0.0.1 unless the user enables LAN access | Default on |
| NFR-08 | Security | Elevation limited to a separate helper for hosts-file changes | No elevated main process |
| NFR-09 | Security | Database root password generated per install, not blank | Default on |
| NFR-10 | Reliability | Failed config generation rolls back to the last working config | No broken state after a failed switch |
| NFR-11 | Portability | Entire install relocatable by moving the Ebur root | Paths resolved at runtime |
| NFR-12 | Portability | Core builds for Windows, macOS and Linux from one codebase | CI builds all three |
| NFR-13 | Usability | A new user serves a site over HTTPS within 10 minutes of download | Usability test with 5 users |
| NFR-14 | Usability | Every error message names the cause and a next step | Reviewed per release |
| NFR-15 | Maintainability | Core package unit test coverage | ≥ 70% |
| NFR-16 | Licensing | Ebur code MIT; bundled tools keep their licences, listed in `THIRD_PARTY.md` | Checked per release |
| NFR-17 | Privacy | No telemetry by default; any future telemetry is opt-in | Default off |

## 6. Architecture and data layout

All logic lives in one Go core package; the tray app and the CLI are thin layers over it, so every feature ships to both at once.

&#91;embedded content: Ebur architecture · 2 interfaces, 6 core modules, 3 external systems\]

The core is the only part that downloads, writes configs, starts processes or touches the OS; the elevated helper is a separate tiny binary so the main app never runs as admin.

### 6.1 Install folder

```
C:\ebur\
├── ebur.exe           # CLI (console program)
├── ebur-app.exe       # tray app and window (GUI program; Windows needs separate binaries)
├── ebur-helper.exe    # elevated hosts-file helper
├── ebur.json          # global settings and active versions
├── bin\               # runtimes: php\8.3.x\, nginx\1.x\, mariadb\11.x\ ...
├── templates\         # editable config templates
├── etc\               # generated configs (php.ini per version, site files)
├── data\              # database data and snapshots
├── ssl\               # local CA and site certificates
├── logs\
└── www\               # one folder per site
```

### 6.2 Per-project `.ebur` file

Optional, committed with the project so every teammate gets the same setup. Values not set fall back to the global defaults.

```
php: "8.3"
node: "20"
server: nginx
docroot: public
domains: [shop.test, api.shop.test]
xdebug: false
```

### 6.3 Repository layout

```
ebur/
├── core/        # Go: runtimes, configs, supervisor, sites, certs, projects
├── cli/         # Go: ebur command (cobra)
├── app/         # Wails tray app + frontend
├── helper/      # elevated hosts-file and CA helper
├── templates/   # default config templates
└── manifest/    # runtime versions, URLs, SHA-256
```

### 6.4 Runtime manifest security fields

Each runtime version in the signed manifest carries its support and security status, which the client compares with what is installed (SEC-01 to SEC-03).

```json
"php": {
  "8.3": {
    "status": "active",
    "eol": "YYYY-MM-DD",
    "latest": "8.3.x",
    "vulnerable_below": "8.3.y",
    "advisories": ["CVE-XXXX-XXXX"]
  },
  "8.1": { "status": "eol", "eol": "YYYY-MM-DD" }
}
```

## 7. Release plan

The MVP is the current focus: a reliable Laragon replacement on Windows, with per-project PHP and the other differentiators following in v1.

&#91;embedded content: Release roadmap, 3 phases and 2 gates\]

A phase ends only when its gate passes; Should items that miss a gate move to the next phase rather than delaying it. The per-project PHP spike (section 8) runs during the MVP so v1 is not blocked by it.

## 8. Open questions and risks

The biggest risk is per-project PHP on Windows, where PHP-FPM is not native; it needs a spike before v1 is committed.

| Risk | Impact | Mitigation |
| --- | --- | --- |
| Running several PHP versions at once on Windows (no native FPM) | Delays PHP-04, the headline v1 feature | Spike passed (Nov 24, 2026, `spikes/phppool`): PHP 8.3 and 8.4 served side by side, each by a pool of 4 `php-cgi.exe` behind an Nginx `upstream` with `least_conn` and `max_fails=0`. Each worker's `PHP_FCGI_MAX_REQUESTS` limit must be staggered, otherwise the whole pool expires at once and Nginx returns 502s; with staggering, 300 requests across restarts had 0 failures. A Job Object killed all children when the supervisor was force-killed. A request still waits when every worker in a pool is busy, so the pool size should be configurable |
| WebView2 memory breaks NFR-02 | Ebur looks as heavy as the tools it replaces | Spike passed (Nov 24, 2026, `spikes/traymem`, Wails v3.1.0): tray only 19 MB working set; window open about 380 MB across 7 processes; after closing, WebView2 exits fully and the app returns to 34–36 MB working set (52–56 MB private bytes). Idle memory rose about 1 MB per open/close cycle, so v1 needs a longer leak test. Stripped binary is 12 MB. Re-measured with the full app on Nov 25, 2026 (embedded host, all features): window open 231–256 MB private / 420–470 MB working set, flat over 10 minutes of tab switching and idling (page heap 2–5 MB, back to 1.9 MB after garbage collection: no leak); window closed: WebView2 exits, 43 MB working set / 60 MB private, 0.3% of one core. NFR-02 met. An earlier "1 GB" reading did not reproduce |
| Antivirus flags unsigned binaries or the hosts helper | Install fails or scares users | Code-sign releases (Azure Trusted Signing costs a few dollars a month); publish checksums; document allow-listing |
| Antivirus hosts-file protection blocks Ebur even when elevated | No `.test` domains; SITE-01 fails | Seen in the HTTPS spike (Nov 24, 2026): Webroot SecureAnywhere (`WRSVC`) holds the hosts file so it cannot be written, even with admin rights, for more than 10 s. Reading still works. The Restart Manager API names the locking process when called from PowerShell, but an unsigned Ebur build gets "handle is invalid" for Webroot's protected service, so `ebur doctor` reports "held open by a protected program, often security software". Retest with a code-signed build |
| First launch of a newly installed runtime is very slow under antivirus | First `ebur start` or PHP switch after an install looks hung, or serves 502s | Measured Nov 24, 2026 with Webroot: PHP 8.3's php-cgi workers took about 35 s to start listening on their first run, then 0.6 s on later runs. The host now reports success only once every port accepts connections (up to 45 s). Next: warm up new runtimes during install (run `php -m` with the generated php.ini) so the delay happens behind the progress bar |
| Another local stack (e.g. Laragon) already uses port 3306 | MariaDB cannot start; common for people switching from Laragon | Seen Nov 24, 2026: Laragon's MySQL held 3306. Ebur's port check names the process; `db_port` in ebur.json (`ebur db port 3307`) lets both run side by side |
| Official download hosts are slow | Installs take many minutes | Measured Nov 24, 2026: archive.mariadb.org gave 0.07–0.17 MB/s; the downloads.mariadb.org redirector reached a mirror at 61 MB/s. The manifest now uses the redirector (SHA-256 still verified). Resumable downloads (RUN-05) and per-release fallback URLs would remove the remaining risk |
| Local HTTPS spike results | Shapes SSL-01 to SSL-04 | Spike passed apart from hosts (Nov 24, 2026, `spikes/https`): mkcert CA install took one Windows confirmation; certificate issued in under 200 ms; Nginx served HTTP/2 over TLS 1.3; Go and headless Edge trusted it with no warning. Windows `curl.exe` rejects it unless run with `--ssl-no-revoke`, because mkcert certificates have no revocation endpoint. PHP rejects it until SSL-04 is applied. Resolved in the product build (Nov 24, 2026): site certificates point to an empty, CA-signed CRL that Nginx serves on 127.0.0.1, and `curl.exe` then accepts them with no flags; PHP trusts both local and public sites through a bundle of curl's Mozilla roots plus the Ebur CA |
| Existing tools such as FlyEnv (open source) already cover this space | Weak reason to switch | Position on footprint, full CLI and `.ebur` team files; compare before launch |
| Official download URLs change or old versions disappear | Installs break | Manifest updated by CI; mirror fallback in the Ebur release assets |
| Port 80/443 taken by IIS, Skype or another stack | Sites fail to load | `ebur doctor` names the process; fallback ports 8080/8443 |
| Single maintainer burnout | Project stalls | Clear module ownership, good-first-issue labels, contributor guide from day one |

- [x] Final licence: MIT (decided Nov 25, 2026). Matches the PHP ecosystem (Laravel, Symfony, Composer); all built-in libraries (MIT, Apache-2.0, BSD) are compatible. Copyright line: "Ebur contributors".
- [x] Repo and domain: `github.com/ebur` is taken (a personal account), so the project uses `github.com/ebur-dev/ebur`, which is also the Go module path. `ebur.dev` showed no registration on Nov 25, 2026; register it to secure it.
- [x] Default web server for new installs: Nginx (MVP ships Nginx only; Apache arrives in v1 with SVC-02).
- [ ] How to avoid an admin prompt for every new site: accept the prompt, pre-authorise the helper once with an elevated scheduled task, or offer `.localhost`? Tested: browsers and `curl.exe` resolve `*.localhost` themselves, but the Windows resolver (used by PHP, .NET and most programs) fails, so a site calling its own URL breaks (WordPress cron and Site Health loopback requests). `.localhost` can only be a fallback where the hosts file is locked.
- [x] Keep mkcert or generate the CA in Go? Go (`core/certs`): no extra download, and the CA carries name constraints mkcert does not add. mkcert's last release is v1.4.4 (April 2022) with no published checksums.
- [x] Wails v2 or v3: v3 (stable since v3.1.0; native tray, windows that can be fully closed while the app keeps running).
- [ ] GUI frontend framework inside Wails: Vue, Svelte or React?
- [ ] Should Ebur import existing Laragon `www\` folders and databases on first run?
- [ ] Where is the runtime manifest hosted, and who can update it?
