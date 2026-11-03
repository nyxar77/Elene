<p align="center">
  <img src="docs/assets/logo.svg" width="128" height="128" alt="Elene Logo" />
</p>

<h1 align="center">Elene</h1>

<p align="center">
  <strong>Local Android application installer and updater built around ADB.</strong><br>
  Zero-cloud sideloading, in-place updates, and verified F-Droid &amp; IzzyOnDroid catalog support.
</p>

<p align="center">
  <a href="#local-dashboard">Dashboard</a> •
  <a href="#installation">Installation</a> •
  <a href="docs/DASHBOARD.md">Documentation</a> •
  <a href="docs/DEVELOPMENT.md">Development</a>
</p>

---

Elene communicates directly with your Android hardware over the official ADB wire protocol. It does not host APKs, proxy traffic through the cloud, or require user accounts.

## Local dashboard

```bash
nix run .#elene -- serve
```

Open <http://127.0.0.1:18420> for app navigation, device selection, APK installation,
and installed-package browsing.
The daemon serves everything locally, with light/dark palettes and a complete
no-JavaScript mode. See [dashboard usage and settings](docs/DASHBOARD.md).

## Installation

Add Elene to your system flake inputs:

```nix
inputs.elene.url = "github:nyxar77/elene";
```

### Home Manager

Import the Home Manager module:

```nix
{ inputs, ... }: {
  imports = [ inputs.elene.homeManagerModules.default ];

  programs.elene = {
    enable = true;
    # Optional: run background web station as a systemd user service
    # service.enable = true;
  };
}
```

Or add the package directly:

```nix
home.packages = [
  inputs.elene.packages.${pkgs.stdenv.hostPlatform.system}.elene
];
```

### NixOS

Add the package to your system packages:

```nix
environment.systemPackages = [
  inputs.elene.packages.${pkgs.stdenv.hostPlatform.system}.elene
];
```

## Development

Guidelines for contributing, quality checks, and local testing are detailed in [Development Rules](docs/DEVELOPMENT.md).
See also the [Project Charter](docs/PROJECT_CHARTER.md) and [Repository Policy](docs/REPOSITORY_POLICY.md).
