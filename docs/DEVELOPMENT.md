# Development rules

Enter the development environment with:

```bash
nix develop
```

The flake owns the required Go, Android platform tools, Node.js, Bun, SQLite,
formatting, and analysis tools. It also exposes project commands:

```bash
nix run .#fmt
nix run .#test
nix run .#lint
nix run .#check
nix run .#elene -- devices
```

Run both checks before every push. CI repeats them in a clean environment:

```bash
nix run .#check
nix flake check --accept-flake-config
```

## Branches

`master` is the protected mainline. Use a short-lived branch:

| Prefix | Use |
| --- | --- |
| `feature/*` | New behavior |
| `fix/*` | Bug fixes |
| `chore/*` | Tooling or maintenance |
| `docs/*` | Documentation-only work |
| `refactor/*` | Internal restructuring |
| `test/*` | Test-only work |
| `perf/*` | Measured performance work |
| `spike/*` | Disposable experiments |

Open a pull request into `master`; direct pushes are blocked. See the
[repository policy](REPOSITORY_POLICY.md) for the required checks and merge
rules.

## Commits

Commits are small, connected, and written in plain imperative language.

```text
add ADB device inspection
reject malformed ADB device lists
run race tests in CI
```

Avoid generated summaries, vague messages such as `updates`, and commits that
mix unrelated formatting, refactors, and features. Do not use conventional
commit prefixes unless the repository later adopts them deliberately.

## Frontend standards

The application frontend lives in `web/` and uses Bun for dependency management
and scripts, Svelte 5 with strict TypeScript, Vite for production builds,
and Lucide for icons. Commit `bun.lock`; use `bun install --frozen-lockfile`
for reproducible installs. Do not introduce another package manager or duplicate
an existing library's functionality.

Keep shared components in `src/components`, route pages in `src/pages`, and API
contracts and utilities in `src/lib`. Components must not invoke ADB, parse source
indexes, or own credentials. The Go backend owns those responsibilities. Use
semantic CSS variables for every theme; do not hard-code palette colors in page
components. Use established navigation and forms, clear labels, and keyboard
accessible controls. Avoid marketing copy and invented statistics in application
screens. Unsupported source operations must be visibly unavailable.

The enhanced UI and server-rendered no-JavaScript interface use the same backend
operations and theme tokens. Basic mode must remain a useful, styled application,
not a placeholder or redirect to the JavaScript app. Do not require external
fonts, scripts, analytics, or a second runtime server in production.

Before submitting frontend changes, run `bun run check`, `bun run test`, and
`bun run build` from `web/`, then the Go/Nix gates above. Built assets in
`internal/dashboard/assets/dist` are committed so Go and Nix builds work offline
without a package registry. Regenerate them whenever frontend source changes.
