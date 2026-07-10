//go:build unix

// fastchown - paralleler, rekursiver chown mit skip-if-unchanged.
//
// Gedacht für sehr große Filetrees (>10M Dateien), wo klassisches
// `chown -R` durch fork/exec-Overhead und serielle Traversal ausbremst.
// Ein einziger Prozess, ein Walk, direkter Lchown()-Syscall über N Worker.
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
// parallele Worker erzeugen nur Seek-Thrashing statt Speedup. Faustregel:
// -j im Bereich der Anzahl Vdevs bzw. niedrig zweistellig testen.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/bauer-group/xpd-linuxtools/internal/ownership"
)

// version wird beim Release per -ldflags "-X main.version=..." gesetzt.
var version = "dev"

type stats struct {
	scanned atomic.Int64
	changed atomic.Int64
	skipped atomic.Int64
	errors  atomic.Int64
}

// config bündelt die Laufparameter, damit run() ohne Flag-/os.Args-Kopplung
// testbar bleibt.
type config struct {
	spec     ownership.Spec
	roots    []string
	workers  int
	dryRun   bool
	verbose  bool
	interval time.Duration
}

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

	cfg := config{
		spec:     spec,
		roots:    roots,
		workers:  *workers,
		dryRun:   *dryRun,
		verbose:  *verbose,
		interval: *interval,
	}

	start := time.Now()
	st, interrupted := run(ctx, cfg)
	stop()

	elapsed := time.Since(start)
	mode := "chown"
	if cfg.dryRun {
		mode = "dry-run"
	}
	fmt.Printf("\n%s abgeschlossen in %s\n", mode, elapsed.Truncate(time.Second))
	fmt.Printf("gescannt: %d | geändert: %d | übersprungen (bereits korrekt): %d | fehler: %d\n",
		st.scanned.Load(), st.changed.Load(), st.skipped.Load(), st.errors.Load())
	if interrupted {
		fmt.Fprintln(os.Stderr, "abgebrochen (SIGINT/SIGTERM) – Re-Run überspringt bereits geänderte Einträge")
	}
	if st.errors.Load() > 0 || interrupted {
		os.Exit(1)
	}
}

// run führt den parallelen Walk+chown aus und liefert die Statistik sowie ob
// via ctx (SIGINT/SIGTERM) abgebrochen wurde. Ein einziger Walk-Goroutine
// speist die Pfade an cfg.workers parallele Worker.
func run(ctx context.Context, cfg config) (*stats, bool) {
	st := &stats{}
	if cfg.workers < 1 {
		cfg.workers = 1
	}

	paths := make(chan string, 4096)
	var wg sync.WaitGroup
	for i := 0; i < cfg.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range paths {
				processPath(p, cfg.spec, st, cfg.dryRun, cfg.verbose)
			}
		}()
	}

	start := time.Now()
	done := make(chan struct{})
	if cfg.verbose {
		go progressLoop(st, start, cfg.interval, done)
	}

	interrupted := false
	for _, root := range cfg.roots {
		werr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if ctx.Err() != nil {
				interrupted = true
				return filepath.SkipAll
			}
			if err != nil {
				st.errors.Add(1)
				if cfg.verbose {
					fmt.Fprintf(os.Stderr, "walk error %s: %v\n", path, err)
				}
				return nil
			}
			paths <- path
			return nil
		})
		if werr != nil {
			fmt.Fprintf(os.Stderr, "walk fehlgeschlagen für %s: %v\n", root, werr)
		}
		if interrupted {
			break
		}
	}
	close(paths)
	wg.Wait()
	close(done)
	return st, interrupted
}

// processPath ermittelt Ziel-UID/GID für einen Eintrag und ruft lchown auf,
// sofern eine Änderung nötig ist. Symlinks werden selbst behandelt (lchown),
// nicht ihr Ziel.
func processPath(p string, spec ownership.Spec, st *stats, dryRun, verbose bool) {
	st.scanned.Add(1)

	lst, err := os.Lstat(p)
	if err != nil {
		st.errors.Add(1)
		if verbose {
			fmt.Fprintf(os.Stderr, "stat error %s: %v\n", p, err)
		}
		return
	}
	sysstat, ok := lst.Sys().(*syscall.Stat_t)
	if !ok {
		st.errors.Add(1)
		return
	}

	targetUID, targetGID, changed := spec.Resolve(int(sysstat.Uid), int(sysstat.Gid))
	if !changed {
		st.skipped.Add(1)
		return
	}
	if dryRun {
		st.changed.Add(1)
		return
	}
	if err := os.Lchown(p, targetUID, targetGID); err != nil {
		st.errors.Add(1)
		if verbose {
			fmt.Fprintf(os.Stderr, "chown error %s: %v\n", p, err)
		}
		return
	}
	st.changed.Add(1)
}

// progressLoop gibt bis zum Schließen von done periodisch den Fortschritt aus.
func progressLoop(st *stats, start time.Time, interval time.Duration, done <-chan struct{}) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			el := time.Since(start).Seconds()
			sc := st.scanned.Load()
			rate := 0.0
			if el > 0 {
				rate = float64(sc) / el
			}
			fmt.Fprintf(os.Stderr,
				"[%s] gescannt=%d geändert=%d übersprungen=%d fehler=%d rate=%.0f/s\n",
				time.Since(start).Truncate(time.Second), sc,
				st.changed.Load(), st.skipped.Load(), st.errors.Load(), rate)
		case <-done:
			return
		}
	}
}
