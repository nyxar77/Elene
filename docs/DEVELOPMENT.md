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
