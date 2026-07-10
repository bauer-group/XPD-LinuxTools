//go:build unix

package main

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/bauer-group/xpd-linuxtools/internal/ownership"
)

// buildTree legt einen kleinen Baum an (Verzeichnis + Dateien + Symlink) und
// liefert Root plus die Anzahl der Einträge, die ein Walk sehen wird.
func buildTree(t *testing.T) (root string, entries int64) {
	t.Helper()
	root = t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, f := range []string{
		filepath.Join(root, "a.txt"),
		filepath.Join(sub, "b.txt"),
	} {
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", f, err)
		}
	}
	if err := os.Symlink("a.txt", filepath.Join(root, "link")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	// root + sub + a.txt + b.txt + link = 5 Einträge
	return root, 5
}

func gidOf(t *testing.T, path string) int {
	t.Helper()
	fi, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("lstat %s: %v", path, err)
	}
	return int(fi.Sys().(*syscall.Stat_t).Gid)
}

// Erster Lauf auf einem frisch angelegten Baum: alle Einträge gehören bereits
// dem aktuellen User -> chown auf die eigene UID/GID muss alles überspringen.
func TestRunSkipsWhenAlreadyOwned(t *testing.T) {
	root, want := buildTree(t)
	spec := ownership.Spec{UID: os.Getuid(), GID: os.Getgid(), HasUID: true, HasGID: true}

	st, interrupted := run(context.Background(), config{spec: spec, roots: []string{root}, workers: 4})

	if interrupted {
		t.Fatal("unerwartet abgebrochen")
	}
	if got := st.scanned.Load(); got != want {
		t.Fatalf("scanned=%d, erwartet %d", got, want)
	}
	if got := st.errors.Load(); got != 0 {
		t.Fatalf("errors=%d, erwartet 0", got)
	}
	if got := st.changed.Load(); got != 0 {
		t.Errorf("changed=%d, erwartet 0 (bereits korrekt)", got)
	}
	if got := st.skipped.Load(); got != want {
		t.Errorf("skipped=%d, erwartet %d (alle)", got, want)
	}
}

// Dry-run darf zwar zählen, aber nichts anfassen. Wir setzen eine abweichende
// Ziel-GID, sodass alle Einträge "würden geändert" zählen; da -n gesetzt ist,
// wird kein lchown ausgeführt (kein root nötig) und die reale GID bleibt.
func TestRunDryRunCountsButDoesNotMutate(t *testing.T) {
	root, want := buildTree(t)
	origGid := os.Getgid()
	spec := ownership.Spec{UID: os.Getuid(), GID: origGid + 1, HasUID: true, HasGID: true}

	st, _ := run(context.Background(), config{spec: spec, roots: []string{root}, workers: 4, dryRun: true})

	if got := st.changed.Load(); got != want {
		t.Errorf("changed=%d, erwartet %d (alle würden geändert)", got, want)
	}
	if got := st.skipped.Load(); got != 0 {
		t.Errorf("skipped=%d, erwartet 0", got)
	}
	// Beweis, dass dry-run nichts verändert hat:
	if got := gidOf(t, filepath.Join(root, "a.txt")); got != origGid {
		t.Errorf("dry-run hat GID verändert: got %d, erwartet %d", got, origGid)
	}
}

// Die Zählung muss unabhängig von der Worker-Anzahl deterministisch sein
// (fängt Doppelzählung / Races; mit `go test -race` zusätzlich abgesichert).
func TestRunDeterministicAcrossWorkerCounts(t *testing.T) {
	spec := ownership.Spec{UID: os.Getuid(), GID: os.Getgid(), HasUID: true, HasGID: true}

	root1, want := buildTree(t)
	st1, _ := run(context.Background(), config{spec: spec, roots: []string{root1}, workers: 1})

	root8, _ := buildTree(t)
	st8, _ := run(context.Background(), config{spec: spec, roots: []string{root8}, workers: 8})

	if st1.scanned.Load() != want || st8.scanned.Load() != want {
		t.Fatalf("scanned uneinheitlich: -j1=%d -j8=%d, erwartet %d",
			st1.scanned.Load(), st8.scanned.Load(), want)
	}
	if st1.skipped.Load() != st8.skipped.Load() {
		t.Errorf("skipped uneinheitlich: -j1=%d vs -j8=%d", st1.skipped.Load(), st8.skipped.Load())
	}
}
