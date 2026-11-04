# Local dashboard

Start the daemon from the checkout:

```sh
nix run .#elene -- serve
```

Open <http://127.0.0.1:18420>. If that port is already in use, set
`ELENE_PORT=18421` for a second local instance and open the printed URL. Keep the process running, or run the installed
`elene serve` command under your user service manager. Ctrl+C or SIGTERM stops
it. No hosting service, account, CDN, or network font is needed at runtime.
The Go binary embeds the built Svelte UI, shared styles, and server-rendered
no-JavaScript pages. ADB must be available;
the Nix package supplies it. `--adb PATH` and `ELENE_ADB_PATH` still work.

Connect a device with USB debugging enabled and accept its authorization prompt.
Select the device, choose one APK, and install. The dashboard reports installation
success or the ADB error. Installations update existing apps using `adb install -r`.
ADB remains responsible for validating package contents and Android signatures;
Elene does not currently verify APK authenticity or compatibility itself.
Split APK bundles are unsupported. Uploads are limited to 512 MiB, and one
installation runs at a time. Installation has a five-minute timeout. Uploads use
private temporary files, removed after the operation. The last 20 jobs are kept
in memory until the daemon stops; this is not a persistent app library.

## Navigation and sources

Home, Installed apps, Updates, Activity, and Settings are separate routes. The
source picker is a single dropdown in the top navigation. Local APK installation
and ADB-backed installed-package listing work now. F-Droid shows apps from its public index, with search, categories, and direct installation. The first load downloads the index and later views use a
one-hour memory cache. Packages are downloaded securely from official F-Droid mirrors and verified with SHA-256 before installation via ADB.
Aurora still shows an explicit unavailable state. Automatic update discovery is
not implemented. Installed apps use Android package
IDs and generic icons because display names and icons are not extracted yet.

## Frontend development

```sh
nix develop
cd web
bun install --frozen-lockfile
bun run dev
```

Run the Go daemon separately. Vite binds to loopback and proxies API requests to
`127.0.0.1:18420`; this development server is not part of the production runtime.
Production assets are generated with `bun run build` and embedded from
`internal/dashboard/assets/dist`. These generated assets are committed so the Go
and Nix package builds do not need a JavaScript registry. CI rebuilds and rejects
stale assets. The development standards are in [DEVELOPMENT.md](DEVELOPMENT.md).

## Appearance and JavaScript

Appearance settings provide:

- Elene light and dark.
- Dracula.
- Catppuccin Latte, Frappé, Macchiato, and Mocha, each with all 14 accents.
- Gruvbox light and dark, each with soft, medium, and hard contrast.

Palette references: [Catppuccin](https://catppuccin.com/palette/),
[Dracula](https://draculatheme.com/contribute), and
[Gruvbox](https://github.com/morhetz/gruvbox/blob/master/colors/gruvbox.vim).

The default enhanced interface refreshes devices and job status automatically.
Select **No JavaScript** to omit the script entirely and send a CSP blocking
scripts. The same layout, themes, device picker, uploads, activity, and settings
work through server-rendered forms. Refresh manually to see changes. Disabling
JavaScript in your browser also leaves these forms usable in enhanced mode.

Preferences are shared by browsers accessing this daemon and saved atomically to
`$XDG_CONFIG_HOME/elene/config.json`, defaulting to `~/.config/elene/config.json`.
The file is created on the first save.
For example:

```json
{
  "theme": "mocha",
  "accent": "mauve",
  "contrast": "medium",
  "interface": "basic"
}
```

Stop the daemon before editing this file manually. Invalid settings produce a
startup error instead of silently overwriting the file. No browser storage is
required, and credentials are not stored in this configuration.

## Running as a user service

With `elene` installed at `~/.local/bin/elene`, a user service can use:

```ini
[Unit]
Description=Elene local Android dashboard

[Service]
ExecStart=%h/.local/bin/elene serve
Restart=on-failure
RestartSec=3

[Install]
WantedBy=default.target
```

Adjust `ExecStart` to your installed executable, and ensure ADB is on the service
PATH or set `Environment=ELENE_ADB_PATH=/absolute/path/to/adb`. Save as
`~/.config/systemd/user/elene.service`, then run `systemctl --user daemon-reload`
and `systemctl --user enable --now elene`. No service is installed automatically.

## Local access boundary

The server binds only to IPv4 loopback, validates the request Host and Origin,
and requires a per-process token for changes. Use the printed `127.0.0.1` URL;
other hostnames are deliberately rejected. These checks prevent other websites
from directly driving the local API; they are not authentication against other
processes or users already on the same computer. Do not expose this port through
a reverse proxy or tunnel.
