# Project scope

Elene is a local Android application installer and updater. It runs on the
user's computer, communicates with Android devices through the official ADB
executable, and opens a UI on the local machine.

Elene is not an APK host, app store, cloud service, or Google Play client.

## Product boundary

Elene is responsible for:

- detecting and inspecting USB-debugging devices
- fetching metadata from supported application sources
- selecting a compatible single-file APK
- downloading, verifying, and caching that APK
- installing through ADB
- showing local job progress and useful errors

The Go backend owns ADB, downloads, storage, secrets, and source credentials.
The browser UI only uses the local API.

## Engineering rules

- Bind the local server only to `127.0.0.1`.
- Pass ADB values as process arguments. Never construct a shell command.
- Give every blocking operation a context and a timeout.
- Validate external data before using it.
- Keep source-specific parsing out of the frontend.
- Prefer small concrete packages over future-facing abstractions.
- Make ambiguous release selection visible to the user.
- Document unsupported package formats instead of guessing.
- Keep security claims narrower than the implemented verification.

## Quality rules

Every behavior change needs tests at the same time as the implementation.
Production files containing parsing, state changes, command construction, or
error mapping need focused tests. Thin wiring files may share a package-level
test when that better tests their observable behavior.

Before every push, run:

```bash
nix run .#check
nix flake check --accept-flake-config
```

The checks require clean Go and Nix formatting, whitespace checks, Go and Nix
static analysis, workflow linting, race-enabled tests, and a production build.
Pull requests run the same gate in CI.

## Current scope

The first increment is the ADB CLI prototype. Web UI, SQLite persistence,
remote sources, and APK downloading remain outside this increment.
