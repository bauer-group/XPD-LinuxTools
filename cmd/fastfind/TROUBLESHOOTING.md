# fastfind — Troubleshooting / Operator-Runbook

`fastfind` ist read-only, birgt also keine Datenrisiken — die typischen Fragen drehen sich um
**langsame Läufe** (I/O-bound auf RAIDZ/HDD) und **unerwartete Treffermengen**.

## 1. Läuft nur langsam — wieso?

Wie bei [fastchown](../fastchown/TROUBLESHOOTING.md)/fastchmod dominiert auf einem HDD-RAIDZ-Vdev die
**Metadaten-IOPS** (Traversal + `lstat` pro Eintrag), nicht die Bandbreite.

```bash
ps aux | grep fastfind          # STAT=D -> wartet auf I/O
cat /proc/<PID>/stack           # root: aktueller Kernel-Call (VFS/ZFS)
strace -f -p <PID>              # live: newfstatat/getdents64 = Traversal
iostat -x 1                     # %util->100 + await steigt = Platte am Limit
zpool iostat -v 1               # pro-Vdev Ops/s
```

Auf einem einzelnen Spinning-Vdev bringt mehr `-j` selten etwas (mehr Seeks); auf NVMe/mehreren Vdevs
skaliert es. Da fastfind Treffer laufend auf stdout streamt, ist die Ausgabe selbst der beste
Fortschrittsindikator.

## 2. Zu viele / zu wenige Treffer

- **Prädikate sind UND-verknüpft.** `--type f --setuid` findet nur setuid-**Dateien**, nicht Dirs.
  Zu wenige Treffer? Ein Prädikat zu viel. Zu viele? Grenze das mit `--type`/`--name` ein.
- **`--older`/`--newer`** beziehen sich auf **mtime**, nicht atime/ctime. `--older 30d` = älter als
  30 Tage; `--newer 7d` = in den letzten 7 Tagen geändert.
- **Größen** sind Basis 1024 (`1M` = 1048576). `--min-size` auf Verzeichnisse ist meist sinnlos
  (Dir-Größe ≠ Inhaltsgröße) — mit `--type f` kombinieren.

## 3. `--orphan` liefert nichts / zu viel

- Die Auflösung geht gegen die **lokale** `/etc/passwd`/`/etc/group`. Läuft die User-Verwaltung über
  LDAP/SSSD, können „verwaiste" Owner in Wahrheit zentral existieren (dann ggf. False Positives) —
  je nach Build wird NSS konsultiert oder nicht. Auf Standard-Hosting-Boxen mit lokalen Usern stimmt
  die Auflösung.
- Zum Gegenprüfen einer konkreten UID: `getent passwd <uid>` (leer = existiert lokal/NSS nicht).

## 4. Fehlerzähler / Permission denied

Läuft der Report mit `-v` und zeigt `fehler: N > 0`, sind meist Verzeichnisse ohne Leserecht die
Ursache (der Walk kann nicht absteigen) oder Einträge, die während des Laufs verschwanden. Mit `-v`
werden die betroffenen Pfade einzeln auf stderr geloggt. Für vollständige Bäume als root laufen lassen.

## 5. Pipen mit Sonderzeichen im Dateinamen

Enthalten Pfade Leerzeichen oder Newlines, bricht `... | xargs fastchmod` möglicherweise falsch um.
Dafür `-0` nutzen und downstream `xargs -0` / `--null`:

```bash
fastfind -0 --uid 0 /srv | xargs -0r fastchmod 640
```
