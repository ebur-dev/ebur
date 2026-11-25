# Third-party software in Ebur

Ebur itself is released under the MIT licence (see `LICENSE`). Every
component below keeps its own licence (SRS NFR-16). This file is checked
per release.

## Built into Ebur's programs

`ebur.exe`, `ebur-app.exe` and `ebur-helper.exe` contain:

| Component | Version | Licence |
| --- | --- | --- |
| Go standard library | 1.27 | BSD-3-Clause |
| github.com/wailsapp/wails/v3 | 3.1.0 | MIT |
| github.com/go-ole/go-ole | 1.3.0 | MIT |
| github.com/Microsoft/go-winio | 0.6.4 | MIT |
| github.com/spf13/cobra | 1.10.2 | Apache-2.0 |
| github.com/spf13/pflag | 1.0.10 | BSD-3-Clause |
| github.com/inconshreveable/mousetrap | 1.1.0 | Apache-2.0 |
| golang.org/x/sys | 0.50.0 | BSD-3-Clause |
| Svelte (window user interface) | 5.57.3 | MIT |
| Inter (UI font, via @fontsource-variable/inter) | 5.3.0 | OFL-1.1 (`licenses\Inter-OFL.txt`) |
| JetBrains Mono (code font, via @fontsource-variable/jetbrains-mono) | 5.3.0 | OFL-1.1 (`licenses\JetBrainsMono-OFL.txt`) |

`ebur-helper.exe`, the only program that runs as administrator, contains only
the Go standard library, `golang.org/x/sys` and Ebur's own hosts-file code.

## Downloaded when you choose them

Ebur does not ship these. It downloads them from their official sources when
you install them, checks each download's SHA-256, and keeps them in `bin\`.

| Component | Source | Licence |
| --- | --- | --- |
| PHP | windows.php.net | PHP License 3.01 |
| Nginx | nginx.org | BSD-2-Clause |
| MariaDB | mariadb.org | GPL-2.0 |
| Composer | getcomposer.org | MIT |
| Mailpit (mail capture) | github.com/axllent/mailpit | MIT |
| Node.js (with npm and Corepack) | nodejs.org | MIT (Node.js; bundled parts under their own licences, see its LICENSE) |
| Adminer | github.com/vrana/adminer | Apache-2.0 or GPL-2.0 |
| CA certificate bundle (Mozilla roots, via curl) | curl.se | MPL-2.0 |

Run `ebur doctor` or open the window's Health tab to see which are installed.

## Project starters

`ebur new` and the window's *New project…* fetch these into your new project
folder; they become part of your project, under their own licences.

| Starter | Source | Licence |
| --- | --- | --- |
| WordPress (latest release, SHA-1 checked against wordpress.org) | wordpress.org | GPL-2.0-or-later |
| Laravel (`laravel/laravel` and its packages, via Composer) | packagist.org | MIT (Laravel) |
| Symfony (`symfony/skeleton` and its packages, via Composer) | packagist.org | MIT (Symfony) |
