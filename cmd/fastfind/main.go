//go:build unix

// fastfind - paralleler, rekursiver find mit den Prädikaten, die man beim
// Aufräumen großer Filetrees braucht. Read-only: gibt passende Pfade auf
// stdout aus (ein Pfad pro Zeile), ideal als Vorstufe zu fastchown/fastchmod:
//
//	fastfind --uid 0 --world-writable /srv | xargs -r fastchmod 640
//	fastfind --orphan /data | fastchown 1000:1000 /dev/stdin   # o. ä.
//
// Prädikate werden UND-verknüpft. Ohne Prädikat wird der ganze Baum gelistet.
// Nutzt die gemeinsame Engine internal/parwalk.
//
// Usage:
//
//	fastfind [flags] PATH...
//
// Beispiele:
//
//	fastfind --world-writable /var/www
//	fastfind --orphan /home                     # Owner ohne Eintrag in /etc/passwd|group
//	fastfind --type f --setuid /usr             # setuid-Binaries
//	fastfind --older 90d --min-size 100M /data  # alte, große Dateien
//	fastfind -0 --name '*.log' /var/log | xargs -0r rm
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/bauer-group/xpd-linuxtools/internal/parwalk"
)

// version wird beim Release per -ldflags "-X main.version=..." gesetzt.
var version = "dev"

// predicate testet einen Eintrag; alle Prädikate müssen zutreffen (UND).
type predicate func(path string, fi os.FileInfo, sys *syscall.Stat_t) bool

// criteria hält die geparsten Suchkriterien (optionale via Pointer/Sentinel).
type criteria struct {
	typ        string // "", "f", "d", "l"
	uid, gid   int    // -1 = ungesetzt
	orphan     bool
	worldWrite bool
	setuid     bool
	setgid     bool
	name       string
	older      *time.Duration
	newer      *time.Duration
	minSize    *int64
	maxSize    *int64
}

func main() {
	workers := flag.Int("j", 8, "Anzahl paralleler Worker")
	verbose := flag.Bool("v", false, "Zusammenfassung + Fehler auf stderr")
	null := flag.Bool("0", false, "Pfade mit NUL statt Newline trennen (für xargs -0)")
	showVersion := flag.Bool("version", false, "Version ausgeben und beenden")

	typ := flag.String("type", "", "Typ: f (Datei), d (Verzeichnis), l (Symlink)")
	uid := flag.Int("uid", -1, "nur Einträge mit dieser Owner-UID")
	gid := flag.Int("gid", -1, "nur Einträge mit dieser Owner-GID")
	orphan := flag.Bool("orphan", false, "Owner-UID oder -GID ohne Eintrag in /etc/passwd|group")
	worldWrite := flag.Bool("world-writable", false, "für andere schreibbar (o+w)")
	setuid := flag.Bool("setuid", false, "setuid-Bit gesetzt")
	setgid := flag.Bool("setgid", false, "setgid-Bit gesetzt")
	name := flag.String("name", "", "Glob auf den Dateinamen (z. B. '*.log')")
	older := flag.String("older", "", "Mtime älter als (z. B. 30d, 12h)")
	newer := flag.String("newer", "", "Mtime jünger als (z. B. 7d)")
	minSize := flag.String("min-size", "", "Mindestgröße (z. B. 100M, 2G)")
	maxSize := flag.String("max-size", "", "Maximalgröße (z. B. 500K)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "fastfind %s\n\n", version)
		fmt.Fprintf(os.Stderr, "Usage: %s [flags] PATH...\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Paralleler, rekursiver find. Prädikate werden UND-verknüpft; passende Pfade auf stdout.\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *showVersion {
		fmt.Printf("fastfind %s\n", version)
		return
	}

	roots := flag.Args()
	if len(roots) < 1 {
		flag.Usage()
		os.Exit(1)
	}

	crit, err := parseCriteria(*typ, *uid, *gid, *orphan, *worldWrite, *setuid, *setgid,
		*name, *older, *newer, *minSize, *maxSize)
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

	preds := buildPredicates(crit)

	// Ein einzelner Printer-Goroutine schreibt gepuffert auf stdout -> keine
	// verwürfelten Zeilen trotz paralleler Worker, pipe-tauglich.
	sep := byte('\n')
	if *null {
		sep = 0
	}
	matches := make(chan string, 1024)
	var printerWg sync.WaitGroup
	var writeErr error
	printerWg.Add(1)
	go func() {
		defer printerWg.Done()
		w := bufio.NewWriter(os.Stdout)
		for p := range matches {
			if writeErr != nil {
				continue // nach Fehler weiter draining (kein Deadlock), aber nicht mehr schreiben
			}
			if _, err := w.WriteString(p); err != nil {
				writeErr = err
				continue
			}
			if err := w.WriteByte(sep); err != nil {
				writeErr = err
			}
		}
		if err := w.Flush(); err != nil && writeErr == nil {
			writeErr = err
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	start := time.Now()
	st, interrupted := parwalk.Run(ctx, parwalk.Config{
		Roots:   roots,
		Workers: *workers,
		Verbose: *verbose, // Walk-Fehler loggen; kein Progress-Ticker (Ausgabe = Fortschritt)
	}, findAction(preds, matches, *verbose))
	stop()

	close(matches)
	printerWg.Wait()

	if *verbose {
		fmt.Fprintf(os.Stderr, "\ngeprüft: %d | gefunden: %d | fehler: %d | dauer: %s\n",
			st.Scanned(), st.Changed(), st.Errors(), time.Since(start).Truncate(time.Millisecond))
	}
	if writeErr != nil {
		fmt.Fprintln(os.Stderr, "Ausgabefehler:", writeErr)
		os.Exit(1)
	}
	if st.Errors() > 0 || interrupted {
		os.Exit(1)
	}
}

// parseCriteria validiert und wandelt die Roh-Flags in criteria.
func parseCriteria(typ string, uid, gid int, orphan, worldWrite, setuid, setgid bool,
	name, older, newer, minSize, maxSize string) (criteria, error) {
	c := criteria{typ: typ, uid: uid, gid: gid, orphan: orphan,
		worldWrite: worldWrite, setuid: setuid, setgid: setgid, name: name}

	switch typ {
	case "", "f", "d", "l":
	default:
		return criteria{}, fmt.Errorf("ungültiger --type %q (erwartet f, d oder l)", typ)
	}
	if name != "" {
		if _, err := filepath.Match(name, ""); err != nil {
			return criteria{}, fmt.Errorf("ungültiges --name Muster %q: %w", name, err)
		}
	}
	if older != "" {
		d, err := parseAge(older)
		if err != nil {
			return criteria{}, fmt.Errorf("--older: %w", err)
		}
		c.older = &d
	}
	if newer != "" {
		d, err := parseAge(newer)
		if err != nil {
			return criteria{}, fmt.Errorf("--newer: %w", err)
		}
		c.newer = &d
	}
	if minSize != "" {
		n, err := parseSize(minSize)
		if err != nil {
			return criteria{}, fmt.Errorf("--min-size: %w", err)
		}
		c.minSize = &n
	}
	if maxSize != "" {
		n, err := parseSize(maxSize)
		if err != nil {
			return criteria{}, fmt.Errorf("--max-size: %w", err)
		}
		c.maxSize = &n
	}
	return c, nil
}

// buildPredicates baut aus den Kriterien die UND-verknüpften Prädikate.
func buildPredicates(c criteria) []predicate {
	var preds []predicate

	switch c.typ {
	case "f":
		preds = append(preds, func(_ string, fi os.FileInfo, _ *syscall.Stat_t) bool { return fi.Mode().IsRegular() })
	case "d":
		preds = append(preds, func(_ string, fi os.FileInfo, _ *syscall.Stat_t) bool { return fi.IsDir() })
	case "l":
		preds = append(preds, func(_ string, fi os.FileInfo, _ *syscall.Stat_t) bool { return fi.Mode()&os.ModeSymlink != 0 })
	}

	if c.uid >= 0 {
		u := c.uid
		preds = append(preds, func(_ string, _ os.FileInfo, s *syscall.Stat_t) bool { return s != nil && int(s.Uid) == u })
	}
	if c.gid >= 0 {
		g := c.gid
		preds = append(preds, func(_ string, _ os.FileInfo, s *syscall.Stat_t) bool { return s != nil && int(s.Gid) == g })
	}
	if c.orphan {
		res := newOwnerResolver()
		preds = append(preds, func(_ string, _ os.FileInfo, s *syscall.Stat_t) bool {
			return s != nil && (res.uidOrphan(int(s.Uid)) || res.gidOrphan(int(s.Gid)))
		})
	}
	if c.worldWrite {
		preds = append(preds, func(_ string, fi os.FileInfo, _ *syscall.Stat_t) bool { return fi.Mode().Perm()&0o002 != 0 })
	}
	if c.setuid {
		preds = append(preds, func(_ string, fi os.FileInfo, _ *syscall.Stat_t) bool { return fi.Mode()&os.ModeSetuid != 0 })
	}
	if c.setgid {
		preds = append(preds, func(_ string, fi os.FileInfo, _ *syscall.Stat_t) bool { return fi.Mode()&os.ModeSetgid != 0 })
	}
	if c.name != "" {
		pat := c.name
		preds = append(preds, func(path string, _ os.FileInfo, _ *syscall.Stat_t) bool {
			ok, _ := filepath.Match(pat, filepath.Base(path))
			return ok
		})
	}
	if c.older != nil {
		cutoff := time.Now().Add(-*c.older)
		preds = append(preds, func(_ string, fi os.FileInfo, _ *syscall.Stat_t) bool { return fi.ModTime().Before(cutoff) })
	}
	if c.newer != nil {
		cutoff := time.Now().Add(-*c.newer)
		preds = append(preds, func(_ string, fi os.FileInfo, _ *syscall.Stat_t) bool { return fi.ModTime().After(cutoff) })
	}
	if c.minSize != nil {
		min := *c.minSize
		preds = append(preds, func(_ string, fi os.FileInfo, _ *syscall.Stat_t) bool { return fi.Size() >= min })
	}
	if c.maxSize != nil {
		max := *c.maxSize
		preds = append(preds, func(_ string, fi os.FileInfo, _ *syscall.Stat_t) bool { return fi.Size() <= max })
	}
	return preds
}

// findAction wertet alle Prädikate aus und meldet passende Pfade an out.
func findAction(preds []predicate, out chan<- string, verbose bool) parwalk.Action {
	return func(path string, d os.DirEntry) parwalk.Result {
		fi, err := d.Info()
		if err != nil {
			if verbose {
				fmt.Fprintf(os.Stderr, "stat error %s: %v\n", path, err)
			}
			return parwalk.Errored
		}
		sys, _ := fi.Sys().(*syscall.Stat_t)
		for _, p := range preds {
			if !p(path, fi, sys) {
				return parwalk.Skipped
			}
		}
		out <- path
		return parwalk.Changed
	}
}

// ownerResolver cached uid/gid-Lookups gegen /etc/passwd bzw. /etc/group, damit
// nicht pro Datei erneut aufgelöst wird.
type ownerResolver struct {
	mu       sync.Mutex
	uidKnown map[int]bool
	gidKnown map[int]bool
}

func newOwnerResolver() *ownerResolver {
	return &ownerResolver{uidKnown: map[int]bool{}, gidKnown: map[int]bool{}}
}

func (r *ownerResolver) uidOrphan(uid int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if known, ok := r.uidKnown[uid]; ok {
		return !known
	}
	_, err := user.LookupId(strconv.Itoa(uid))
	known := err == nil
	r.uidKnown[uid] = known
	return !known
}

func (r *ownerResolver) gidOrphan(gid int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if known, ok := r.gidKnown[gid]; ok {
		return !known
	}
	_, err := user.LookupGroupId(strconv.Itoa(gid))
	known := err == nil
	r.gidKnown[gid] = known
	return !known
}
