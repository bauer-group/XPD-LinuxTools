# fastchown

Paralleler, rekursiver `chown` mit **skip-if-unchanged** — ein Ein-Prozess-Ersatz für
`chown -R UID:GID PATH` auf sehr großen Filetrees (Millionen Dateien), wie sie z. B. auf
ZFS/RAIDZ-Storage vorkommen.

Klassisches `chown -R` ist auf riesigen Bäumen langsam: serielle Traversal plus ein
`lchown()` pro Eintrag ohne jede Parallelität. `fastchown` läuft als **ein** Prozess mit
**einem** Verzeichnis-Walk, der die Pfade an *N* parallele Worker speist, und **überspringt
Einträge, die bereits die Ziel-UID/GID haben** — dadurch sind Wiederholungsläufe billig.

## Eigenschaften

- **Parallel** — `-j` Worker rufen `lchown()` gleichzeitig auf.
- **Idempotent** — bereits korrekte Einträge werden übersprungen (skip-if-unchanged).
- **Symlink-sicher** — nutzt `lchown()`, wirkt auf den Symlink selbst, folgt nicht dem Ziel.
- **Dry-run** — `-n` zählt nur, ändert nichts.
- **Fortschritt** — `-v` gibt periodisch Rate & Zähler auf stderr aus.
- **Keine Dependencies** — reine Go-Standardbibliothek.

## Nutzung

```text
fastchown [flags] UID[:GID] PATH...
```

Die Ownership-Angabe folgt der `chown`-Syntax (nur numerische IDs):

| Angabe        | Wirkung                                   |
| ------------- | ----------------------------------------- |
| `1000:1000`   | UID **und** GID setzen                     |
| `1000`        | nur UID setzen                            |
| `:1000`       | nur GID setzen (wie `chown :1000`)         |
| `1000:`       | nur UID setzen (leere GID = unverändert)   |

### Flags

| Flag                  | Default | Bedeutung                                            |
| --------------------- | ------- | ---------------------------------------------------- |
| `-j`                  | `8`     | Anzahl paralleler Worker                             |
| `-n`                  | `false` | Dry-run: nur zählen/anzeigen, nichts ändern          |
| `-v`                  | `false` | Fortschritt periodisch auf stderr                    |
| `-progress-interval`  | `5s`    | Intervall der Fortschrittsausgabe (mit `-v`)         |
| `--version`           |         | Version ausgeben und beenden                         |

### Beispiele

```bash
fastchown 1000:1000 /data/stack            # rekursiv, 8 Worker (Default)
fastchown :1000 /data/stack                # nur GID ändern
fastchown -n -v 1000:1000 /data/stack      # Dry-run mit Live-Fortschritt
fastchown -j 12 -v 1000:1000 /a /b /c      # mehrere Pfade, 12 Worker
```

Exit-Code ist `0`, wenn alles fehlerfrei durchlief, sonst `1` (Fehler oder Abbruch via
SIGINT/SIGTERM). Ein abgebrochener Lauf ist unkritisch: bereits geänderte Einträge sind
idempotent, ein Re-Run überspringt sie.

## Tuning für RAIDZ/HDD

Ein RAIDZ-Vdev liefert bei Random-I/O ungefähr die **IOPS einer einzelnen Platte** (nicht die
Summe aller Platten im Vdev). Zu viele Worker erzeugen dann nur Seek-Thrashing statt Speedup.

- Starte mit `-j 8` bis `-j 16` und beobachte die `-v`-Rate (Einträge/s).
- Steigt die Rate mit mehr Workern nicht mehr (oder sinkt), bist du an der IOPS-Grenze des
  Vdevs — dann `-j` reduzieren.
- Faustregel: `-j` grob in der Größenordnung der Anzahl **Vdevs**, nicht der Platten.
- Vorab `-n` laufen lassen, um abzuschätzen, wie viele Einträge überhaupt geändert würden.

Details zur Fehlersuche bei hängenden/langsamen Läufen: siehe
[TROUBLESHOOTING.md](TROUBLESHOOTING.md).

## Installation

Siehe [Repo-README](../../README.md#installation) — `.deb`/`.rpm` von den GitHub-Releases oder
Build aus dem Quellcode (`go build ./cmd/fastchown`, nur Linux/Unix).
