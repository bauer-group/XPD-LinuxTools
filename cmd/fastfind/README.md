# fastfind

Paralleler, rekursiver `find` mit genau den Prädikaten, die man beim Aufräumen großer Filetrees
braucht. **Read-only** — gibt passende Pfade auf stdout aus (ein Pfad pro Zeile) und ist damit die
ideale Vorstufe zu [fastchown](../fastchown/README.md) / [fastchmod](../fastchmod/README.md).

Teilt sich die Engine [`internal/parwalk`](../../internal/parwalk) mit den anderen Tools: ein Walk,
*N* Worker werten die Prädikate parallel aus. Prädikate werden **UND**-verknüpft; ohne Prädikat wird
der ganze Baum gelistet (ein schnelles paralleles `find PATH`).

## Nutzung

```text
fastfind [flags] PATH...
```

### Prädikate

| Flag | Wirkung |
| ---- | ------- |
| `--type f\|d\|l` | nur Dateien / Verzeichnisse / Symlinks |
| `--uid N` / `--gid N` | Owner-UID / -GID gleich N |
| `--orphan` | Owner-UID **oder** -GID ohne Eintrag in `/etc/passwd`/`/etc/group` |
| `--world-writable` | für andere schreibbar (`o+w`) |
| `--setuid` / `--setgid` | setuid- / setgid-Bit gesetzt |
| `--name GLOB` | Glob auf den Dateinamen (z. B. `'*.log'`) |
| `--older DUR` / `--newer DUR` | Mtime älter / jünger als (`30d`, `12h`, `45m`, `90s`) |
| `--min-size SZ` / `--max-size SZ` | Größe ≥ / ≤ (`100M`, `2G`, `500K`; Basis 1024) |

### Weitere Flags

| Flag | Default | Bedeutung |
| ---- | ------- | --------- |
| `-j` | `8` | Anzahl paralleler Worker |
| `-0` | `false` | Pfade mit NUL statt Newline trennen (für `xargs -0`) |
| `-v` | `false` | Zusammenfassung (geprüft/gefunden/Fehler) + Fehler auf stderr |
| `--version` | | Version ausgeben und beenden |

### Beispiele

```bash
fastfind --world-writable /var/www                 # Sicherheits-Check
fastfind --type f --setuid /usr                     # setuid-Binaries auflisten
fastfind --orphan /home                             # Dateien verwaister Owner
fastfind --older 90d --min-size 100M /data          # alte, große Dateien

# Als Vorstufe zu den anderen Tools (NUL-sicher gegen Sonderzeichen in Namen):
fastfind -0 --uid 0 /srv/www | xargs -0r fastchmod 640
```

Exit-Code `0` bei fehlerfreiem Lauf (auch ohne Treffer), sonst `1` (Fehler oder Abbruch). Die Reihenfolge
der Ausgabe ist durch die Parallelität nicht deterministisch — bei Bedarf durch `sort` pipen.

## Hinweise

- **`--orphan`** löst UIDs/GIDs gegen die lokale `/etc/passwd`/`/etc/group` auf (gecacht). Zentrale
  Verzeichnisdienste (LDAP/SSSD) werden je nach Build ggf. nicht konsultiert — für Hosting-Boxen mit
  lokalen Usern ist das der Normalfall.
- **Symlinks** werden nicht verfolgt (der Walk steigt nicht in ihr Ziel ab); `--type l` matcht den
  Symlink selbst.

Fehlersuche bei langsamen Läufen: [TROUBLESHOOTING.md](TROUBLESHOOTING.md).

## Installation

Siehe [Repo-README](../../README.md#installation) — `.deb`/`.rpm` von den GitHub-Releases oder
Build aus dem Quellcode (`go build ./cmd/fastfind`, nur Linux/Unix).
