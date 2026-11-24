# ebur-app: tray app and main window

The tray icon and window (SRS GUI-01 to GUI-05), built with Wails v3 and a
Svelte frontend. The app embeds the same host as `ebur daemon`, so the CLI
and the window act on the same services.

## Build

The frontend must be built first, because the Go binary embeds
`frontend/dist`:

```
cd app/frontend
npm install
npm run build
cd ../..
go build -ldflags "-s -w -H windowsgui" -o ebur-app.exe ./app
```

`-H windowsgui` makes it a GUI program (no console window). The CLI is a
separate console program, `ebur.exe`, built from `./cli`.

## Run

`ebur-app.exe` opens the window; `ebur-app.exe --background` starts in the
tray only. Launching it again opens the existing window. Set `EBUR_ROOT`
to use an Ebur folder other than the one holding the executable.

## How it fits together

- `backend.go`: embeds the host, or uses one the CLI already started
- `service.go`: methods the page calls as `main.App.<Method>`
- `tray.go`, `icons.go`: tray menu and status icons (drawn in code)
- `frontend/`: Svelte 5 + Vite; calls Go through `/wails/runtime.js`

Closing the window destroys it, releasing WebView2 (about 40 MB idle with
the window closed, NFR-02). Quitting, from the tray or with `ebur quit`,
stops every service.
