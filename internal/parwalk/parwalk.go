// Package parwalk führt einen rekursiven Verzeichnis-Walk aus, dessen Einträge
// von N parallelen Workern verarbeitet werden: ein einziger Walk speist eine
// Queue, die Worker rufen pro Eintrag eine Action auf und melden das Ergebnis.
//
// Es ist die gemeinsame Basis der Dateibaum-Tools (fastchown, fastchmod, …).
// Die Mechanik (Parallelität, Zählung, Fortschritt, Abbruch via Context) lebt
// hier; die Tools liefern nur die Action ("was pro Eintrag zu tun ist").
//
// parwalk ist plattformneutral (keine Syscalls) und damit auf jeder Plattform
// testbar; die unix-spezifische Logik steckt in den Actions der Tools.
package parwalk

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// Result meldet, was eine Action mit einem Eintrag gemacht hat.
type Result int

const (
	Skipped Result = iota // unverändert (bereits korrekt, oder bewusst übersprungen)
	Changed               // geändert (bzw. würde geändert bei Dry-run)
	Errored               // Fehler bei der Verarbeitung
)

// Stats sammelt die Zählwerte threadsicher.
type Stats struct {
	scanned atomic.Int64
	changed atomic.Int64
	skipped atomic.Int64
	errors  atomic.Int64
}

func (s *Stats) Scanned() int64 { return s.scanned.Load() }
func (s *Stats) Changed() int64 { return s.changed.Load() }
func (s *Stats) Skipped() int64 { return s.skipped.Load() }
func (s *Stats) Errors() int64  { return s.errors.Load() }

// Config steuert einen Lauf.
type Config struct {
	Roots    []string
	Workers  int           // < 1 wird als 1 behandelt
	Verbose  bool          // Walk-/Eintragsfehler auf stderr loggen
	Progress bool          // periodischer Fortschritts-Ticker auf stderr
	Interval time.Duration // Fortschrittsintervall (0 => 5s)
}

// Action verarbeitet einen einzelnen Eintrag und meldet das Ergebnis zurück.
// Sie wird nebenläufig aus mehreren Workern aufgerufen und muss threadsicher
// sein. Fehlerausgaben (bei Verbose) verantwortet die Action selbst.
type Action func(path string, d os.DirEntry) Result

type item struct {
	path string
	d    os.DirEntry
}

// Run walkt Config.Roots ab und ruft action parallel pro Eintrag auf. Liefert
// die Statistik und ob via ctx (Signal) abgebrochen wurde.
func Run(ctx context.Context, cfg Config, action Action) (*Stats, bool) {
	st := &Stats{}
	workers := cfg.Workers
	if workers < 1 {
		workers = 1
	}

	items := make(chan item, 4096)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for it := range items {
				st.scanned.Add(1)
				switch action(it.path, it.d) {
				case Changed:
					st.changed.Add(1)
				case Errored:
					st.errors.Add(1)
				default:
					st.skipped.Add(1)
				}
			}
		}()
	}

	start := time.Now()
	done := make(chan struct{})
	if cfg.Progress {
		go progress(st, start, cfg.Interval, done)
	}

	interrupted := false
	for _, root := range cfg.Roots {
		werr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if ctx.Err() != nil {
				interrupted = true
				return filepath.SkipAll
			}
			if err != nil {
				st.errors.Add(1)
				if cfg.Verbose {
					fmt.Fprintf(os.Stderr, "walk error %s: %v\n", path, err)
				}
				return nil
			}
			items <- item{path, d}
			return nil
		})
		if werr != nil {
			fmt.Fprintf(os.Stderr, "walk fehlgeschlagen für %s: %v\n", root, werr)
		}
		if interrupted {
			break
		}
	}
	close(items)
	wg.Wait()
	close(done)
	return st, interrupted
}

// PrintSummary gibt die einheitliche Abschluss-Zusammenfassung aus (stdout;
// Abbruch-Hinweis auf stderr). label ist das Tool-Verb (z. B. "chown"/"chmod").
func PrintSummary(label string, dryRun bool, st *Stats, interrupted bool, elapsed time.Duration) {
	mode := label
	if dryRun {
		mode = "dry-run"
	}
	fmt.Printf("\n%s abgeschlossen in %s\n", mode, elapsed.Truncate(time.Second))
	fmt.Printf("gescannt: %d | geändert: %d | übersprungen: %d | fehler: %d\n",
		st.Scanned(), st.Changed(), st.Skipped(), st.Errors())
	if interrupted {
		fmt.Fprintln(os.Stderr, "abgebrochen (SIGINT/SIGTERM) – Re-Run überspringt bereits verarbeitete Einträge")
	}
}

func progress(st *Stats, start time.Time, interval time.Duration, done <-chan struct{}) {
	if interval <= 0 {
		interval = 5 * time.Second
	}
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
