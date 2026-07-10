# XPD-LinuxTools

Kleine, fokussierte Hilfstools für Sonderfälle auf Linux-Umgebungen — jedes als eigenständiges,
paketiertes CLI. Ein Monorepo, aus dem sich `.deb`- und `.rpm`-Pakete für alle Tools automatisch
bauen lassen.

[![CI](https://github.com/bauer-group/XPD-LinuxTools/actions/workflows/ci.yml/badge.svg)](https://github.com/bauer-group/XPD-LinuxTools/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/bauer-group/XPD-LinuxTools?sort=semver)](https://github.com/bauer-group/XPD-LinuxTools/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

## Tools

| Tool | Beschreibung | Doku |
| ---- | ------------ | ---- |
| **fastchown** | Paralleler, rekursiver `chown` mit skip-if-unchanged für sehr große Filetrees (ZFS/RAIDZ). | [README](cmd/fastchown/README.md) · [Troubleshooting](cmd/fastchown/TROUBLESHOOTING.md) |

## Installation

Für jedes Release werden `.deb`- und `.rpm`-Pakete für **amd64** und **arm64** als Assets
bereitgestellt. Es gibt (noch) keinen gehosteten apt/dnf-Repo-Server — die Pakete werden direkt
vom Release installiert.

### Debian / Ubuntu (`.deb`)

```bash
VERSION=1.0.0                        # gewünschte Release-Version
ARCH=amd64                           # amd64 oder arm64
curl -fsSLO "https://github.com/bauer-group/XPD-LinuxTools/releases/download/v${VERSION}/fastchown_${VERSION}_${ARCH}.deb"
sudo apt install "./fastchown_${VERSION}_${ARCH}.deb"
```

### RHEL / Fedora / Rocky (`.rpm`)

```bash
VERSION=1.0.0                        # gewünschte Release-Version
ARCH=x86_64                          # x86_64 oder aarch64
sudo dnf install "https://github.com/bauer-group/XPD-LinuxTools/releases/download/v${VERSION}/fastchown-${VERSION}.${ARCH}.rpm"
```

### Aus dem Quellcode

Die Tools sind Linux/Unix-only (nutzen `lchown()` u. ä.):

```bash
git clone https://github.com/bauer-group/XPD-LinuxTools.git
cd XPD-LinuxTools
go build -o fastchown ./cmd/fastchown        # auf Windows: GOOS=linux go build ./cmd/fastchown
```

## Neues Tool hinzufügen

Ein neues Tool = ein neuer Ordner unter `cmd/<tool>/` plus je ein Block in `.goreleaser.yaml`.
Die vollständige Checkliste steht in [CONTRIBUTING.md](CONTRIBUTING.md).

## Repository-Aufbau

```text
cmd/<tool>/        # je ein CLI (package main); Einstieg für neue Tools
internal/          # geteilte, plattformneutrale Logik (z. B. internal/ownership)
.goreleaser.yaml   # Build + .deb/.rpm-Paketierung aller Tools (GoReleaser v2)
.github/workflows/ # CI (Test/Lint/Packaging) + Release + Packages
```

Ein **einzelnes** Go-Modul (`github.com/bauer-group/xpd-linuxtools`) hält alle Tools; jedes wird zu
einem eigenständigen Binary und eigenen `.deb`/`.rpm`-Paketen.

## Entwicklung

```bash
go test -race ./...                                  # Tests inkl. Race-Detector
golangci-lint run ./...                              # Lint (golangci-lint v2)
goreleaser release --snapshot --clean --skip=publish # Paketbau lokal testen -> dist/
```

## Release-Prozess

Versionierung läuft über **[Conventional Commits](https://www.conventionalcommits.org/)** und
semantic-release (eine Version fürs ganze Repo):

1. Merge nach `main` mit `feat:`/`fix:`/… → `release.yml` schneidet `vX.Y.Z`, erzeugt CHANGELOG und
   GitHub-Release.
2. `packages.yml` (via `workflow_run`) baut per GoReleaser die `.deb`/`.rpm`/`.tar.gz`-Artefakte und
   hängt sie an das Release.

GoReleaser erzeugt dabei **kein** eigenes Release/Tag — semantic-release ist alleiniger Herr über
Version, Tag und CHANGELOG.

## Lizenz

[MIT](LICENSE) © BAUER GROUP
