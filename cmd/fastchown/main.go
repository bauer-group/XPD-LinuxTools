//go:build unix

// fastchown - paralleler, rekursiver chown mit skip-if-unchanged.
//
// Gedacht für sehr große Filetrees (>10M Dateien), wo klassisches
// `chown -R` durch fork/exec-Overhead und serielle Traversal ausbremst.
// Ein einziger Prozess, ein Walk, direkter Lchown()-Syscall über N Worker
// (siehe internal/parwalk für die gemeinsame Engine).
//
// Usage:
//
//	fastchown [flags] UID[:GID] PATH...
//
// Beispiele:
//
//	fastchown 1000:1000 /data/stack               # rekursiv, 8 Worker (Default)
//	fastchown -j 16 1000:1000 /data/a /data/b      # mehrere Pfade, 16 Worker
//	fastchown -n -v 1000:1000 /data/stack          # Dry-run mit Fortschrittsanzeige
//	fastchown :1000 /data/stack                    # nur GID ändern (wie chown :1000)
//
// Hinweis RAIDZ/HDD: -j nicht zu hoch setzen. RAIDZ liefert bei Random-IO
// ungefähr die IOPS EINER Disk (nicht Summe aller Disks im Vdev) - zu viele
// parallele Worker erzeugen nur Seek-Thrashing statt Speedup.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bauer-group/xpd-linuxtools/internal/ownership"
	"github.com/bauer-group/xpd-linuxtools/internal/parwalk"
)

// version wird beim Release per -ldflags "-X main.version=..." gesetzt.
var version = "dev"

func main() {
	workers := flag.Int("j", 8, "Anzahl paralleler Worker (bei RAIDZ-HDDs niedrig halten)")
	dryRun := flag.Bool("n", false, "Dry-run: nur zählen/anzeigen, nichts ändern")
	verbose := flag.Bool("v", false, "Fortschritt periodisch auf stderr ausgeben")
	interval := flag.Duration("progress-interval", 5*time.Second, "Intervall für Fortschrittsausgabe (mit -v)")
	showVersion := flag.Bool("version", false, "Version ausgeben und beenden")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "fastchown %s\n\n", version)
		fmt.Fprintf(os.Stderr, "Usage: %s [flags] UID[:GID] PATH...\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Rekursiver, paralleler chown mit skip-if-unchanged (Ersatz für 'chown -R UID:GID PATH').\n")
		fmt.Fprintf(os.Stderr, "Wirkt auf Symlinks selbst (lchown), folgt ihnen nicht.\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *showVersion {
		fmt.Printf("fastchown %s\n", version)
		return
	}

	args := flag.Args()
	if len(args) < 2 {
		flag.Usage()
		os.Exit(1)
	}

	spec, err := ownership.Parse(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, "Fehler:", err)
		os.Exit(1)
	}
	roots := args[1:]
	for _, r := range roots {
		if _, err := os.Lstat(r); err != nil {
			fmt.Fprintf(os.Stderr, "Fehler: Pfad nicht erreichbar: %s (%v)\n", r, err)
			os.Exit(1)
		}
	}

	// SIGINT/SIGTERM sauber behandeln: Walk stoppt, Worker leeren die Queue,
	// Zusammenfassung wird noch ausgegeben. Bereits geänderte Einträge sind
	// idempotent -> ein Re-Run überspringt sie via skip-if-unchanged.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	start := time.Now()
	st, interrupted := parwalk.Run(ctx, parwalk.Config{
		Roots:    roots,
		Workers:  *workers,
		Verbose:  *verbose,
		Progress: *verbose,
		Interval: *interval,
	}, chownAction(spec, *dryRun, *verbose))
	stop()

	parwalk.PrintSummary("chown", *dryRun, st, interrupted, time.Since(start))
	if st.Errors() > 0 || interrupted {
		os.Exit(1)
	}
}

// chownAction ermittelt Ziel-UID/GID für einen Eintrag und ruft lchown auf,
// sofern eine Änderung nötig ist. Symlinks werden selbst behandelt (lchown),
// nicht ihr Ziel.
func chownAction(spec ownership.Spec, dryRun, verbose bool) parwalk.Action {
	return func(path string, d os.DirEntry) parwalk.Result {
		fi, err := d.Info()
		if err != nil {
			if verbose {
				fmt.Fprintf(os.Stderr, "stat error %s: %v\n", path, err)
			}
			return parwalk.Errored
		}
		sysstat, ok := fi.Sys().(*syscall.Stat_t)
		if !ok {
			return parwalk.Errored
		}

		uid, gid, changed := spec.Resolve(int(sysstat.Uid), int(sysstat.Gid))
		if !changed {
			return parwalk.Skipped
		}
		if dryRun {
			return parwalk.Changed
		}
		if err := os.Lchown(path, uid, gid); err != nil {
			if verbose {
				fmt.Fprintf(os.Stderr, "chown error %s: %v\n", path, err)
			}
			return parwalk.Errored
		}
		return parwalk.Changed
	}
}
