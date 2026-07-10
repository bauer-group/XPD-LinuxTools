# fastchown — Troubleshooting / Operator-Runbook

Fehlersuche bei `fastchown`-Läufen, die **hängen** oder **quälend langsam** sind. Der häufigste
Verdächtige ist ein **blockierender I/O-Syscall** (`lchown`/`getdents`) auf einem langsamen
RAIDZ-/HDD-Vdev — kein Bug im Tool, sondern die Platte an ihrer IOPS-Grenze. Die folgenden
Schritte machen sichtbar, *was genau* gerade blockiert.

> Für die tiefen Schritte (Kernel-Stacks, `strace`) brauchst du **root** bzw. `CAP_SYS_PTRACE`.

## 0. Hängt es wirklich — oder ist es nur langsam?

```bash
ps aux | grep fastchown          # läuft der Prozess? (PID, CPU%, STAT-Spalte)
```

- `STAT` = `D` → **Uninterruptible Sleep**: der Prozess wartet auf I/O (klassisch bei langsamem
  Storage). Das ist *nicht* hängen, sondern warten.
- Läuft `fastchown` mit `-v`, lies die Fortschrittszeile: steht die `rate=…/s` bei > 0, arbeitet
  es — nur langsam. `rate` fallend/niedrig ⇒ du bist I/O-bound, siehe Schritt 4.

## 1. Exakter Kernel-Call (Momentaufnahme)

```bash
cat /proc/<PID>/stack            # als root: zeigt den aktuellen Kernel-Call-Stack
```

Wenn der Prozess in einem blockierenden I/O-Syscall auf dem RAIDZ steht, siehst du hier die
VFS-/ZFS-Kette (z. B. `zfs_*`, `zpl_*`, `txg_wait_*`). Steht dort `txg_wait_open`/`txg_wait_synced`,
wartet der Prozess auf die nächste ZFS-Transaction-Group — reines Storage-Warten.

## 2. Welcher Worker blockiert? (Per-Thread-Stacks)

`fastchown` verteilt die Arbeit auf `-j` Worker, die als OS-Threads laufen. Jeder Thread hat einen
eigenen Stack unter `/proc/<PID>/task/`:

```bash
ls /proc/<PID>/task/                       # ein Verzeichnis pro Thread (TID)
for t in /proc/<PID>/task/*/stack; do echo "== $t =="; cat "$t"; done
```

**Deutung:** *Ein* Thread blockiert in einem I/O-Call, während die anderen idle sind ⇒ langsames
Vdev / IOPS-Limit, **kein Deadlock**. *Alle* Threads am selben Lock ohne Fortschritt über Minuten
⇒ ungewöhnlich, dann Issue mit diesem Output aufmachen.

## 3. Live: welcher Syscall blockiert gerade?

```bash
strace -p <PID>                  # kurz laufen lassen, dann Ctrl-C
strace -f -p <PID>               # -f: allen Worker-Threads folgen
```

Du siehst den exakten Syscall, der gerade steht — typischerweise `fchownat(...)` (das `lchown`)
oder `newfstatat(...)`/`getdents64(...)` (Traversal). Hängt es sichtbar an *einem* `fchownat`
für Sekunden, ist der Storage der Flaschenhals.

Alternativ, ohne Threads einzeln zu verfolgen:

```bash
cat /proc/<PID>/wchan; echo      # Kernel-Funktion, in der der Prozess schläft
```

## 4. Ist das Vdev IOPS-gesättigt?

```bash
iostat -x 1                      # %util →100 und steigendes await = Platte am Limit
zpool iostat -v 1                # pro-Vdev Ops/s und Latenz (das RAIDZ-Vdev ansehen)
```

Ein HDD-RAIDZ sättigt bei **kleinen Metadaten-Operationen** (genau das macht `chown`) lange bevor
die Bandbreite ausgereizt ist — der Engpass sind Random-IOPS, nicht MB/s. `%util` dauerhaft bei
100 % bei niedriger Bandbreite ⇒ klassisch IOPS-bound.

## 5. `-v`-Rate deuten und `-j` justieren

- **Fallende `rate=…/s`** über die Zeit ⇒ IOPS-bound; mit `iostat` gegenprüfen.
- Auf **einem einzelnen Spinning-Vdev** hilft mehr `-j` selten — es erzeugt nur mehr Seeks. Eher
  `-j` **senken**.
- Auf **NVMe/SSD** oder **mehreren Vdevs** kann mehr `-j` skalieren — hochtasten, bis die Rate
  nicht mehr steigt.
- Faustregel: `-j` grob in der Größenordnung der **Vdev-Anzahl**.

## 6. Sicher abbrechen und fortsetzen

```bash
kill -INT <PID>                  # oder Ctrl-C: sauberer Abbruch
```

`fastchown` fängt `SIGINT`/`SIGTERM` ab, stoppt den Walk, lässt die Worker die Queue leeren und
gibt die Zusammenfassung aus (Exit-Code `1`). **Bereits geänderte Einträge sind idempotent** —
ein erneuter Lauf mit denselben Argumenten überspringt sie dank skip-if-unchanged und macht dort
weiter, wo es teuer wird.

## 7. Fehlerzähler in der Zusammenfassung

Endet der Lauf mit `fehler: N > 0`, mit `-v` erneut laufen lassen — dann werden die betroffenen
Pfade einzeln auf stderr geloggt (`stat error …` / `chown error …`). Häufige Ursachen: read-only
Mount, fehlende Rechte (kein root/`CAP_CHOWN`), oder ein Eintrag, der während des Walks entfernt
wurde.
