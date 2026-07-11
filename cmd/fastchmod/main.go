//go:build unix

// fastchmod - paralleler, rekursiver chmod mit skip-if-unchanged.
//
// Analog zu fastchown: ein Prozess, ein Walk, N Worker rufen Chmod() auf,
// bereits korrekte Einträge werden übersprungen (siehe internal/parwalk).
//
// Modi werden oktal angegeben (inkl. setuid/setgid/sticky). Entweder ein
// einzelner Modus für alles, oder getrennt für Verzeichnisse (-d) und Dateien
// (-f) in EINEM Durchlauf - das ersetzt den üblichen Doppelpass mit
// `find -type d -exec chmod` / `find -type f -exec chmod`.
//
// Symlinks werden übersprungen: auf Linux gibt es kein lchmod, und die
// Permissions eines Symlinks sind bedeutungslos (immer 0777). WalkDir folgt
// Symlinks ohnehin nicht in Verzeichnisse.
//
// Usage:
//
//	fastchmod [flags] MODE PATH...          # ein Modus für Dirs und Files
//	fastchmod [flags] -d DIRMODE -f FILEMODE PATH...
//
// Beispiele:
//
//	fastchmod 0644 /data/stack               # alles 0644
//	fastchmod -d 755 -f 644 /srv/www          # Dirs 755, Files 644
//	fastchmod -d 2775 /srv/shared             # nur Dirs -> 2775 (Files unangetastet)
//	fastchmod -n -v 750 /data/stack           # Dry-run mit Fortschrittsanzeige
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bauer-group/xpd-linuxtools/internal/filemode"
	"github.com/bauer-group/xpd-linuxtools/internal/parwalk"
)

// version wird beim Release per -ldflags "-X main.version=..." gesetzt.
var version = "dev"

func main() {
	workers := flag.Int("j", 8, "Anzahl paralleler Worker (bei RAIDZ-HDDs niedrig halten)")
	dryRun := flag.Bool("n", false, "Dry-run: nur zählen/anzeigen, nichts ändern")
	verbose := flag.Bool("v", false, "Fortschritt periodisch auf stderr ausgeben")
	interval := flag.Duration("progress-interval", 5*time.Second, "Intervall für Fortschrittsausgabe (mit -v)")
	dirFlag := flag.String("d", "", "Oktal-Modus nur für Verzeichnisse (z. B. 755, 2775)")
	fileFlag := flag.String("f", "", "Oktal-Modus nur für Dateien (z. B. 644)")
	showVersion := flag.Bool("version", false, "Version ausgeben und beenden")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "fastchmod %s\n\n", version)
		fmt.Fprintf(os.Stderr, "Usage: %s [flags] MODE PATH...\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "       %s [flags] -d DIRMODE -f FILEMODE PATH...\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Rekursiver, paralleler chmod mit skip-if-unchanged (oktale Modi, inkl. setuid/setgid/sticky).\n")
		fmt.Fprintf(os.Stderr, "Symlinks werden übersprungen (kein lchmod auf Linux).\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *showVersion {
		fmt.Printf("fastchmod %s\n", version)
		return
	}

	dirMode, fileMode, roots, err := resolveModes(*dirFlag, *fileFlag, flag.Args())
	if err != nil {
		fmt.Fprintln(os.Stderr, "Fehler:", err)
		flag.Usage()
		os.Exit(1)
	}
	for _, r := range roots {
		if _, err := os.Lstat(r); err != nil {
			fmt.Fprintf(os.Stderr, "Fehler: Pfad nicht erreichbar: %s (%v)\n", r, err)
			os.Exit(1)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	start := time.Now()
	st, interrupted := parwalk.Run(ctx, parwalk.Config{
		Roots:    roots,
		Workers:  *workers,
		Verbose:  *verbose,
		Progress: *verbose,
		Interval: *interval,
	}, chmodAction(dirMode, fileMode, *dryRun, *verbose))
	stop()

	parwalk.PrintSummary("chmod", *dryRun, st, interrupted, time.Since(start))
	if st.Errors() > 0 || interrupted {
		os.Exit(1)
	}
}

// resolveModes ermittelt aus Flags und Argumenten die Ziel-Modi und Pfade.
//
//	-d/-f gesetzt  -> Split-Modus: Dirs/Files je Flag, ungesetzter Typ bleibt
//	                  unverändert; alle Argumente sind Pfade.
//	sonst          -> Uniform-Modus: args[0] ist der Modus für alles, Rest Pfade.
//
// Ein nil-Rückgabemodus bedeutet "diesen Typ nicht anfassen".
func resolveModes(dirFlag, fileFlag string, args []string) (dirMode, fileMode *os.FileMode, roots []string, err error) {
	if dirFlag != "" || fileFlag != "" {
		if dirFlag != "" {
			m, e := filemode.ParseOctal(dirFlag)
			if e != nil {
				return nil, nil, nil, fmt.Errorf("-d: %w", e)
			}
			dirMode = &m
		}
		if fileFlag != "" {
			m, e := filemode.ParseOctal(fileFlag)
			if e != nil {
				return nil, nil, nil, fmt.Errorf("-f: %w", e)
			}
			fileMode = &m
		}
		if len(args) < 1 {
			return nil, nil, nil, fmt.Errorf("kein Pfad angegeben")
		}
		return dirMode, fileMode, args, nil
	}

	if len(args) < 2 {
		return nil, nil, nil, fmt.Errorf("erwartet MODE PATH... (oder -d/-f MODE PATH...)")
	}
	m, e := filemode.ParseOctal(args[0])
	if e != nil {
		return nil, nil, nil, e
	}
	return &m, &m, args[1:], nil
}

// chmodAction setzt den passenden Ziel-Modus (dir/file) für einen Eintrag,
// sofern er abweicht. Symlinks werden übersprungen; ist für den Typ kein Modus
// gesetzt (nil), bleibt der Eintrag unverändert.
func chmodAction(dirMode, fileMode *os.FileMode, dryRun, verbose bool) parwalk.Action {
	return func(path string, d os.DirEntry) parwalk.Result {
		if d.Type()&os.ModeSymlink != 0 {
			return parwalk.Skipped
		}

		target := fileMode
		if d.IsDir() {
			target = dirMode
		}
		if target == nil {
			return parwalk.Skipped
		}

		fi, err := d.Info()
		if err != nil {
			if verbose {
				fmt.Fprintf(os.Stderr, "stat error %s: %v\n", path, err)
			}
			return parwalk.Errored
		}
		if !filemode.Needs(fi.Mode(), *target) {
			return parwalk.Skipped
		}
		if dryRun {
			return parwalk.Changed
		}
		if err := os.Chmod(path, *target); err != nil {
			if verbose {
				fmt.Fprintf(os.Stderr, "chmod error %s: %v\n", path, err)
			}
			return parwalk.Errored
		}
		return parwalk.Changed
	}
}
