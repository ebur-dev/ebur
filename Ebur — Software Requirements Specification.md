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
- Licence: MIT, compatible with redistributing the bundled tools under their own licences.
- The product name and binaries shall not use the word "PHP" (PHP licence naming restriction).
- No writes to the Windows registry or system `PATH`; only the hosts file needs elevation. The local CA goes into the current user's certificate store, which needs no admin rights but shows its own Windows confirmation dialog.

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
| RUN-05 | The system shall show download progress and resume interrupted downloads. | Should | v1 |
| RUN-06 | The system shall let users add a runtime from a local zip or folder. | Could | v1 |

### 3.2 PHP management (PHP)

| ID | Requirement | Priority | Release |
| --- | --- | --- | --- |
| PHP-01 | The system shall let the user set a global active PHP version from the tray, GUI or `ebur php <version>`. | Must | MVP |
| PHP-02 | Switching versions shall regenerate web server config and restart affected services within 5 seconds. | Must | MVP |
| PHP-03 | The system shall generate `php.ini` from a template on first install, preserving user edits on later switches. | Must | MVP |
| PHP-04 | A project's `.ebur` file or `composer.json` PHP constraint shall select its PHP version, running multiple versions at once via separate PHP-FPM / CGI pools. | Must | v1 |
| PHP-05 | The system shall list, enable and disable PHP extensions without manual ini editing. | Should | v1 |
| PHP-06 | The system shall toggle Xdebug on or off per PHP version in one action. | Should | v1 |
| PHP-07 | The system shall bundle Composer and expose it on the Ebur terminal PATH. | Must | MVP |

### 3.3 Services (SVC)

| ID | Requirement | Priority | Release |
| --- | --- | --- | --- |
| SVC-01 | The system shall start, stop and restart the web server, MariaDB/MySQL and PHP processes individually or all at once. The MVP ships Nginx only. | Must | MVP |
| SVC-02 | The system shall let the user choose Nginx or Apache as the web server. | Must | v1 |
| SVC-03 | The system shall detect port conflicts (80, 443, 3306) before start and name the process holding the port. | Must | MVP |
| SVC-04 | The system shall restart a crashed service up to 3 times, then report the failure with its last log lines. | Should | MVP |
| SVC-05 | The system shall support PostgreSQL, Redis and Memcached as optional services. | Should | v1 |
| SVC-06 | The system shall optionally start chosen services when Ebur launches. | Should | MVP |

### 3.4 Sites and virtual hosts (SITE)

| ID | Requirement | Priority | Release |
| --- | --- | --- | --- |
| SITE-01 | Every folder in `www\` shall be served at `<folder>.test` with no manual setup. | Must | MVP |
| SITE-02 | The system shall add and remove hosts-file entries through a small elevated helper, prompting once per change batch. If another process locks the file, the helper shall retry, then name the locking process and explain how to allow Ebur. | Must | MVP |
| SITE-03 | The system shall detect a site's document root (`public\`, `web\` or the folder itself). | Must | MVP |
| SITE-04 | The user shall be able to change the TLD (default `.test`) and add custom domains per site. | Should | v1 |
| SITE-05 | The system shall watch `www\` and apply new or removed sites without a restart. | Should | v1 |

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
| PROJ-01 | The system shall create a new Laravel, WordPress, Symfony or blank project with `ebur new <name> --<type>` or from the GUI. | Should | v1 |
| PROJ-02 | Project creation shall create a database and fill `.env` / `wp-config.php` with its credentials. | Should | v1 |
| PROJ-03 | The system shall bundle Node.js and let users switch Node versions like PHP. | Should | v1 |
| PROJ-04 | The system shall support community project templates from a manifest. | Could | v2 |

### 3.7 Developer tools (TOOL)

| ID | Requirement | Priority | Release |
| --- | --- | --- | --- |
| TOOL-01 | The system shall bundle Adminer and serve it at `db.test`. | Must | MVP |
| TOOL-02 | The system shall show live-tailing logs for PHP, the web server and the database in one viewer. | Should | MVP |
| TOOL-03 | The system shall run Mailpit and configure PHP's mail settings so all mail lands at `mail.test`. | Should | v1 |
| TOOL-04 | The system shall expose a site publicly through a Cloudflare tunnel with `ebur share <site>` and show the URL. | Should | v1 |
| TOOL-05 | The system shall save and restore named database snapshots. | Should | v1 |
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
| CLI-02 | Commands shall support `--json` output for scripts and AI coding tools. | Should | v1 |
| CLI-03 | `ebur doctor` shall check ports, certificates, hosts entries and runtimes, and suggest fixes. | Should | MVP |

### 3.10 Updates (UPD)

| ID | Requirement | Priority | Release |
| --- | --- | --- | --- |
| UPD-01 | Ebur shall check for its own updates and install them only with user approval. | Should | v1 |
| UPD-02 | Updates shall be signed and verified before replacing the binary. | Must | v1 |

### 3.11 Security advisories (SEC)

| ID | Requirement | Priority | Release |
| --- | --- | --- | --- |
| SEC-01 | The runtime manifest shall record each version's support status (active, security-only, end of life), end-of-life date, minimum safe patch and related advisories. | Should | v1 |
| SEC-02 | Ebur shall fetch the manifest on startup and daily, cache it, and use the cached copy when offline. | Should | v1 |
| SEC-03 | Ebur shall badge every installed runtime as supported, security-only / end of life within 6 months, or vulnerable / end of life, and show a warning dot on the tray icon for the last case. | Should | v1 |
| SEC-04 | Ebur shall offer a one-click update to the latest patch of the same minor version and repoint affected sites. | Should | v1 |
| SEC-05 | `ebur audit` and the site list shall report vulnerable project packages using `composer audit` and `npm audit`. | Should | v1 |
| SEC-06 | `ebur share` shall ask for confirmation before exposing a site that runs a vulnerable or end-of-life runtime. | Must | v1 |
| SEC-07 | Ebur shall never upgrade a runtime without user approval, and shall let users snooze warnings per runtime. | Must | v1 |
| SEC-08 | A scheduled CI job shall refresh security data from endoflife.date, official release notes and OSV.dev / NVD, open a pull request for maintainer review, and publish a signed manifest that clients verify. | Must | v1 |
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
| NFR-01 | Footprint | Ebur installer (without runtimes) | ≤ 20 MB |
| NFR-02 | Footprint | Idle memory (working set, as Task Manager shows it) of tray app with the GUI closed. Closing the window must release the WebView2 instance, not just hide it | ≤ 60 MB |
| NFR-03 | Performance | Cold start of all MVP services on an SSD, excluding first-run database initialisation | ≤ 3 s |
| NFR-04 | Performance | Global PHP version switch, including restart | ≤ 5 s |
| NFR-05 | Performance | New site reachable after folder creation (v1 watcher), excluding time spent on the hosts-file admin prompt | ≤ 2 s |
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
| WebView2 memory breaks NFR-02 | Ebur looks as heavy as the tools it replaces | Spike passed (Nov 24, 2026, `spikes/traymem`, Wails v3.1.0): tray only 19 MB working set; window open about 380 MB across 7 processes; after closing, WebView2 exits fully and the app returns to 34–36 MB working set (52–56 MB private bytes). Idle memory rose about 1 MB per open/close cycle, so v1 needs a longer leak test. Stripped binary is 12 MB |
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

- [ ] Final licence: MIT, or Apache-2.0 for its patent grant?
- [ ] Repo and domain: confirm `github.com/ebur` (or fallback) and `ebur.dev` availability.
- [x] Default web server for new installs: Nginx (MVP ships Nginx only; Apache arrives in v1 with SVC-02).
- [ ] How to avoid an admin prompt for every new site: accept the prompt, pre-authorise the helper once with an elevated scheduled task, or offer `.localhost`? Tested: browsers and `curl.exe` resolve `*.localhost` themselves, but the Windows resolver (used by PHP, .NET and most programs) fails, so a site calling its own URL breaks (WordPress cron and Site Health loopback requests). `.localhost` can only be a fallback where the hosts file is locked.
- [x] Keep mkcert or generate the CA in Go? Go (`core/certs`): no extra download, and the CA carries name constraints mkcert does not add. mkcert's last release is v1.4.4 (April 2022) with no published checksums.
- [x] Wails v2 or v3: v3 (stable since v3.1.0; native tray, windows that can be fully closed while the app keeps running).
- [ ] GUI frontend framework inside Wails: Vue, Svelte or React?
- [ ] Should Ebur import existing Laragon `www\` folders and databases on first run?
- [ ] Where is the runtime manifest hosted, and who can update it?
