# Contributing to Ebur

Thanks for helping. Bug reports, fixes, documentation and ideas are all
welcome.

- **Found a bug?** [Open an issue](https://github.com/ebur-dev/ebur/issues/new/choose)
  with the output of `ebur doctor`.
- **Found a security problem?** Do not open an issue; see
  [SECURITY.md](SECURITY.md).
- **Want to add a feature?** Open an issue first, so we can agree on the
  approach before you spend time on it.

## How the project is organised

What Ebur must do is written down in the
[Software Requirements Specification](docs/SRS.md). Each requirement has
an ID such as `SITE-02` or `PHP-04`; use it in issues, pull requests,
commit messages and code comments, so the reasons behind code stay easy to
find. If a change needs a new requirement or changes one, update the SRS in
the same pull request.

| Folder | What it holds |
| --- | --- |
| `core/` | All logic, in Go. The CLI and the tray app are thin layers over it. |
| `cli/` | The `ebur` command |
| `app/` | Tray app and window: Wails v3 with a Svelte 5 frontend ([app/README.md](app/README.md)) |
| `helper/` | `ebur-helper.exe`, the only elevated part; keep it small |
| `manifest/` | Runtime versions, URLs and SHA-256 checksums |
| `build/` | Release script, installer and manifest tools |

## Building and testing

You need Windows 10 or 11, Go 1.27+ and Node.js 22+.

```
go test ./...                      # unit tests
go test -tags integration ./core/runtimes   # downloads real runtimes
powershell -ExecutionPolicy Bypass -File build\build.ps1 -Version 0.1.0-dev   # full build
```

To try a change without touching a real install, point Ebur at a
throwaway folder (folders named `.devroot*` are ignored by git):

```
$env:EBUR_ROOT = "$PWD\.devroot"
go run ./cli setup
go run ./cli start
```

Set `"manual_hosts": true` in `.devroot\ebur.json` while testing, so
creating sites does not ask for administrator rights each time.

## Pull requests

- Keep them focused: one fix or feature per pull request.
- Run `gofmt` and `go vet ./...`, and add or update tests. CI runs the
  tests on Windows and must pass.
- Anything the window can do, the CLI should do too, and the other way
  round.
- Error messages say what went wrong and what to do next (NFR-14).
- Write for people new to local PHP development: plain words, no jargon
  where a short explanation works.

## Rules that keep Ebur trustworthy

- **No telemetry.** Ebur must not send usage data anywhere (NFR-17).
- **Services listen on 127.0.0.1 only** unless the user explicitly shares a
  site.
- **Every download is checked** against a SHA-256 in the manifest. Runtime
  updates go through the manifest (the daily update job proposes them as
  pull requests); never download unchecked files at run time.
- **Dependencies:** add only what is needed, and pin versions that have
  been released for at least 14 days, unless a newer one fixes a security
  problem.
- **The elevated helper** only edits Ebur's own block in the hosts file.
  Changes to it get extra review.

## Licence

By contributing, you agree that your contribution is licensed under the
[MIT licence](LICENSE), like the rest of Ebur.
