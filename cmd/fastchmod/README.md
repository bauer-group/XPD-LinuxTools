# fastchmod

Paralleler, rekursiver `chmod` mit **skip-if-unchanged** — das chmod-Gegenstück zu
[fastchown](../fastchown/README.md), für sehr große Filetrees (ZFS/RAIDZ).

Wie `fastchown` läuft es als **ein** Prozess mit **einem** Verzeichnis-Walk, der die Pfade an *N*
Worker speist, und **überspringt Einträge, die bereits den Ziel-Modus haben** — Wiederholungsläufe
sind dadurch billig. Beide Tools teilen sich die Engine [`internal/parwalk`](../../internal/parwalk).

## Eigenschaften

- **Parallel** — `-j` Worker rufen `chmod()` gleichzeitig auf.
- **Idempotent** — bereits korrekte Einträge werden übersprungen.
- **Dir/File getrennt** — unterschiedliche Modi für Verzeichnisse und Dateien in **einem** Durchlauf
  (ersetzt den `find -type d` / `find -type f`-Doppelpass).
- **setuid/setgid/sticky** — die Sonderbits werden unterstützt (`2775`, `1777`, `4755`).
- **Symlink-sicher** — Symlinks werden übersprungen (auf Linux gibt es kein `lchmod`, und
  Symlink-Permissions sind bedeutungslos). Der Walk folgt Symlinks nicht in Verzeichnisse.
- **Dry-run / Fortschritt** — `-n` bzw. `-v`. Keine externen Dependencies.

## Nutzung

```text
fastchmod [flags] MODE PATH...                       # ein Modus für Dirs und Files
fastchmod [flags] -d DIRMODE -f FILEMODE PATH...     # getrennt
```

Modi sind **oktal** (nur numerisch), inkl. der Sonderbits:

| Angabe | Wirkung |
| ------ | ------- |
| `fastchmod 644 /p` | alle Einträge (Dirs + Files) → `644` |
| `fastchmod -d 755 -f 644 /p` | Verzeichnisse → `755`, Dateien → `644` |
| `fastchmod -d 2775 /p` | nur Verzeichnisse → `2775` (Dateien unangetastet) |
| `fastchmod -f 640 /p` | nur Dateien → `640` (Verzeichnisse unangetastet) |

Ein positionaler `MODE` und `-d`/`-f` schließen sich aus: entweder ein einzelner Modus für alles,
oder die getrennte Form. Bei der getrennten Form bleibt ein Typ, für den kein Flag gesetzt ist,
unverändert.

### Flags

| Flag | Default | Bedeutung |
| ---- | ------- | --------- |
| `-d` | – | Oktal-Modus nur für Verzeichnisse |
| `-f` | – | Oktal-Modus nur für Dateien |
| `-j` | `8` | Anzahl paralleler Worker |
| `-n` | `false` | Dry-run: nur zählen/anzeigen, nichts ändern |
| `-v` | `false` | Fortschritt periodisch auf stderr |
| `-progress-interval` | `5s` | Intervall der Fortschrittsausgabe (mit `-v`) |
| `--version` | | Version ausgeben und beenden |

### Beispiele

```bash
fastchmod 0644 /data/stack               # alles 0644
fastchmod -d 755 -f 644 /srv/www          # Web-Root: Dirs traversierbar, Files nur lesbar
fastchmod -d 2775 /srv/shared             # setgid auf allen Verzeichnissen (Gruppen-Vererbung)
fastchmod -n -v -d 750 -f 640 /data       # Dry-run mit Live-Fortschritt
```

Exit-Code `0` bei fehlerfreiem Lauf, sonst `1` (Fehler oder Abbruch via SIGINT/SIGTERM). Ein
abgebrochener Lauf ist unkritisch — bereits gesetzte Modi sind idempotent, ein Re-Run überspringt sie.

## Tuning für RAIDZ/HDD

Wie bei fastchown gilt: `chmod` ist eine Metadaten-Operation, ein HDD-RAIDZ-Vdev sättigt bei
Random-IOPS. Mit `-j 8`…`16` starten und die `-v`-Rate beobachten; steigt sie mit mehr Workern nicht,
`-j` reduzieren. Details zur Fehlersuche: [TROUBLESHOOTING.md](TROUBLESHOOTING.md).

## Installation

Siehe [Repo-README](../../README.md#installation) — `.deb`/`.rpm` von den GitHub-Releases oder
Build aus dem Quellcode (`go build ./cmd/fastchmod`, nur Linux/Unix).
