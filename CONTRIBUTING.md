# Contributing

## Ein neues Tool hinzufügen

Jedes Tool ist ein eigenständiges CLI unter `cmd/<tool>/` im selben Go-Modul. Um `foo` anzulegen:

1. **Code**: `cmd/foo/main.go` mit `package main`. Plattformneutrale Kernlogik gehört nach
   `internal/<paket>/`, damit sie ohne Build-Tags auf jeder Plattform testbar bleibt (siehe
   [`internal/ownership`](internal/ownership) als Vorlage).
   - Nutzt das Tool unix-spezifische Syscalls, versieh `main.go` mit `//go:build unix` und lege einen
     `main_unsupported.go` (`//go:build !unix`) an, der auf anderen Plattformen sauber abbricht —
     sonst schlägt `go build ./...` z. B. unter Windows fehl.
   - Versions-Variable für `--version`: `var version = "dev"`, per ldflags injizierbar.
2. **Tests**: mindestens Table-Tests der Kernlogik in `internal/`. Integrationstests, die Syscalls
   brauchen, hinter `//go:build unix` (siehe `cmd/fastchown/main_test.go`).
3. **Paketierung**: in [`.goreleaser.yaml`](.goreleaser.yaml) je einen Block an `builds`, `archives`
   und `nfpms` anhängen — analog zum `fastchown`-Block, nur `id`/`main`/`binary`/`package_name` auf
   `foo` ändern. Damit entstehen automatisch `foo_<v>_<arch>.deb` und `foo-<v>.<arch>.rpm`.
4. **Doku**: `cmd/foo/README.md` (Synopsis, Flags, Beispiele). Bei Operations-relevanten Tools
   zusätzlich `cmd/foo/TROUBLESHOOTING.md`. Die `nfpms.contents` so ergänzen, dass diese Docs nach
   `/usr/share/doc/foo/` paketiert werden.
5. **Registrieren**: Tool-Zeile in die Tabelle in [README.md](README.md#tools) eintragen.

Kein Boilerplate darüber hinaus: die CI baut, testet, lintet und validiert die Paketierung
automatisch für alle `cmd/*`.

## Lokale Prüfungen

```bash
go test -race ./...                                  # Tests + Race-Detector
go vet ./...                                         # statische Analyse
golangci-lint run ./...                              # Lint (golangci-lint v2)
goreleaser release --snapshot --clean --skip=publish # Paketbau lokal -> dist/
```

Auf einem Nicht-Linux-Rechner (z. B. Windows) prüfst du die unix-getaggten Dateien per
Cross-Check: `GOOS=linux go vet ./...`. Ein echter Testlauf der unix-Integrationstests braucht Linux
(nativ, WSL oder Container: `docker run --rm -v "$PWD:/src" -w /src golang:1.26 go test -race ./...`).

## Commits & Releases

Versionierung ist vollautomatisch über **[Conventional Commits](https://www.conventionalcommits.org/)**.
Der Commit-Typ bestimmt den SemVer-Sprung:

| Typ | Wirkung |
| --- | ------- |
| `feat:` | MINOR (neues Feature) |
| `fix:` / `perf:` | PATCH |
| `feat!:` oder Footer `BREAKING CHANGE:` | MAJOR |
| `docs:` `chore:` `refactor:` `test:` `ci:` `build:` `style:` | kein Release |

Eine Version gilt fürs **ganze Repo** — ein `fix:` an einem Tool hebt die gemeinsame Version für alle.

Beim Merge nach `main` schneidet semantic-release Release + CHANGELOG, danach baut und
veröffentlicht `packages.yml` die `.deb`/`.rpm`-Artefakte. Nicht selbst taggen oder Releases von Hand
anlegen.
