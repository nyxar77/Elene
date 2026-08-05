# Elene

Elene is a local Android application installer and updater built around ADB.
It does not host APKs or require an account.

## Development

```bash
nix develop
nix run .#check
nix run .#elene -- devices
```

See [the project charter](docs/PROJECT_CHARTER.md) and
[development rules](docs/DEVELOPMENT.md). Repository protection and merge
requirements are documented in the
[repository policy](docs/REPOSITORY_POLICY.md).
