# fastchmod — Troubleshooting / Operator-Runbook

Fehlersuche bei `fastchmod`-Läufen, die **hängen** oder **langsam** sind. Wie bei
[fastchown](../fastchown/TROUBLESHOOTING.md) ist der häufigste Verdächtige ein **blockierender
I/O-Syscall** (`fchmodat`/`getdents`) auf einem langsamen RAIDZ-/HDD-Vdev an seiner IOPS-Grenze —
kein Bug, sondern die Platte am Limit. Die folgenden Schritte machen sichtbar, *was* blockiert.

> Die tiefen Schritte (Kernel-Stacks, `strace`) brauchen **root** bzw. `CAP_SYS_PTRACE`.

## 0. Hängt es wirklich — oder ist es nur langsam?

```bash
ps aux | grep fastchmod          # läuft der Prozess? (PID, CPU%, STAT-Spalte)
```

- `STAT` = `D` → **Uninterruptible Sleep**: wartet auf I/O — nicht hängen, sondern warten.
- Mit `-v`: steht die `rate=…/s` > 0, arbeitet es (nur langsam). Fallende Rate ⇒ I/O-bound (Schritt 4).

## 1. Exakter Kernel-Call (Momentaufnahme)

```bash
cat /proc/<PID>/stack            # als root: aktueller Kernel-Call-Stack
```

Bei blockierendem I/O auf ZFS siehst du die VFS-/ZFS-Kette (`zfs_*`, `zpl_*`, `txg_wait_*`).

## 2. Welcher Worker blockiert? (Per-Thread-Stacks)

`fastchmod` verteilt die Arbeit auf `-j` Worker-Threads:

```bash
ls /proc/<PID>/task/                       # ein Verzeichnis pro Thread (TID)
for t in /proc/<PID>/task/*/stack; do echo "== $t =="; cat "$t"; done
```

*Ein* Thread blockiert in I/O, andere idle ⇒ langsames Vdev / IOPS-Limit, **kein Deadlock**.

## 3. Live: welcher Syscall blockiert gerade?

```bash
strace -p <PID>                  # kurz laufen lassen, dann Ctrl-C
strace -f -p <PID>               # -f: allen Worker-Threads folgen
```

Typischerweise `fchmodat(...)` (das `chmod`) oder `newfstatat(...)`/`getdents64(...)` (Traversal).
Hängt es sichtbar an *einem* `fchmodat` für Sekunden, ist der Storage der Flaschenhals.

```bash
cat /proc/<PID>/wchan; echo      # Kernel-Funktion, in der der Prozess schläft
```

## 4. Ist das Vdev IOPS-gesättigt?

```bash
iostat -x 1                      # %util →100 und steigendes await = Platte am Limit
zpool iostat -v 1                # pro-Vdev Ops/s und Latenz (das RAIDZ-Vdev ansehen)
```

`chmod` ist eine kleine Metadaten-Operation — ein HDD-RAIDZ sättigt auf Random-IOPS, nicht auf
Bandbreite. `%util` dauerhaft bei 100 % bei niedriger Bandbreite ⇒ IOPS-bound.

## 5. `-v`-Rate deuten und `-j` justieren

- Fallende `rate=…/s` ⇒ IOPS-bound; mit `iostat` gegenprüfen.
- Auf **einem** Spinning-Vdev hilft mehr `-j` selten (mehr Seeks) → eher senken.
- Auf NVMe/SSD oder mehreren Vdevs kann mehr `-j` skalieren → hochtasten, bis die Rate stagniert.

## 6. Sicher abbrechen und fortsetzen

```bash
kill -INT <PID>                  # oder Ctrl-C
```

`fastchmod` fängt `SIGINT`/`SIGTERM`, stoppt den Walk, leert die Queue und gibt die Zusammenfassung
aus (Exit `1`). Bereits gesetzte Modi sind **idempotent** — ein Re-Run überspringt sie via
skip-if-unchanged.

## 7. Häufige inhaltliche Stolpersteine

- **Symlinks tauchen als „übersprungen" auf** — das ist gewollt: `fastchmod` chmod't Symlinks nicht
  (kein `lchmod` auf Linux) und folgt ihnen nicht.
- **`fehler: N > 0`** — mit `-v` erneut laufen; betroffene Pfade werden dann einzeln geloggt
  (`stat error …` / `chmod error …`). Häufig: read-only Mount, fehlende Rechte (nur der Eigentümer
  oder root darf chmod'en), oder ein während des Walks entfernter Eintrag.
- **setgid/sticky „verschwinden"** — ein oktaler Modus *ohne* führende Sonderziffer (z. B. `755`)
  löscht setuid/setgid/sticky. Willst du sie behalten/setzen, die 4-stellige Form nutzen
  (`2755` = setgid, `1777` = sticky).
