# Brewctl

A control center for Homebrew on macOS, by [vimukthid.dev](https://vimukthid.dev).

See what you installed and what came along as a dependency, what has updates,
how much disk each package uses, and remove or upgrade things without looking
up the commands. Every action shows the equivalent `brew` command, so the app
doubles as a cheat sheet.

## What it does

- **Overview**: counts, disk usage by category, largest packages, pending updates.
- **Installed**: searchable, sortable list. Click a package for its details,
  what it needs, what needs it, and upgrade / pin / uninstall.
- **Leftover check**: before uninstalling, lists what `brew uninstall` would
  leave behind (cached download, config under the Homebrew prefix, a cask's own
  files, same-named folders in your home) and clears what you tick. Everything
  except Homebrew's own download goes to the Trash.
- **Discover**: search every tool and app in Homebrew by name or description,
  see what else it would install, and install it.
- **Updates**: check for updates, upgrade one or all.
- **Services**: background services Homebrew runs (databases, daemons): see
  which are running, start, stop or restart them, and read their logs.
  Uninstalling a package stops its service first, which `brew uninstall`
  alone doesn't do.
- **Cleanup**: unused dependencies, old versions, the download cache.
- **Activity**: live output of every command the app runs.

Some apps need an admin password to install, upgrade or remove. When Homebrew
asks for it, Brewctl shows the standard macOS password dialog. The password
goes from that dialog straight to `sudo`; the app never sees, logs or stores
it. Cancel the dialog and the job stops with nothing changed.

## Requirements

- macOS 12+ on Apple silicon, with [Homebrew](https://brew.sh)
- To build: Go 1.26+, Node 20+, and the Wails v3 CLI

```sh
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0
```

## Develop

```sh
wails3 dev                 # the app, with hot reload
wails3 task run:server     # the same UI in a browser at http://127.0.0.1:9246
go test ./...
```

## Package

```sh
wails3 package                  # bin/brewctl.app (ad-hoc signed)
wails3 task darwin:package:dmg  # bin/brewctl.dmg
```

The bundle identifier is `dev.vimukthid.brewctl`. Builds are ad-hoc signed,
which is fine on the Mac that built them. To hand the app to someone else it
needs a Developer ID signature and notarization (`wails3 setup`, then
`wails3 task darwin:sign:notarize`); otherwise Gatekeeper blocks it.

## Layout

| Path | What |
|---|---|
| `internal/brew/` | Finds brew, reads the inventory, measures disk usage, runs jobs one at a time |
| `service.go` | The API the UI calls |
| `frontend/src/` | React UI: `state.tsx` holds the data, `views/` the screens |
| `build/` | Icons, Info.plist and packaging tasks |
| `tools/icon/` | Draws `build/appicon.png` (`go run ./tools/icon build/appicon.png`) |

## License

[MIT](LICENSE)
