# Development rules

Enter the development environment with:

```bash
nix develop
```

The flake owns the required Go, Android platform tools, Node.js, SQLite,
formatting, and analysis tools. It also exposes project commands:

```bash
nix run .#fmt
nix run .#test
nix run .#lint
nix run .#check
nix run .#elene -- devices
```

`nix run .#check` is the required pre-push command. CI runs that command and
then `nix flake check`, which rebuilds the package and Go checks in a clean
environment.

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
