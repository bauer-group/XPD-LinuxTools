//go:build unix

package main

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/bauer-group/xpd-linuxtools/internal/ownership"
	"github.com/bauer-group/xpd-linuxtools/internal/parwalk"
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

func runChown(spec ownership.Spec, dryRun bool, workers int, roots ...string) *parwalk.Stats {
	st, _ := parwalk.Run(context.Background(),
		parwalk.Config{Roots: roots, Workers: workers},
		chownAction(spec, dryRun, false))
	return st
}

func gidOf(t *testing.T, path string) int {
	t.Helper()
	fi, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("lstat %s: %v", path, err)
	}
	return int(fi.Sys().(*syscall.Stat_t).Gid)
}

// Frisch angelegter Baum gehört bereits dem aktuellen User -> chown auf die
// eigene UID/GID muss alles überspringen.
func TestChownSkipsWhenAlreadyOwned(t *testing.T) {
	root, want := buildTree(t)
	spec := ownership.Spec{UID: os.Getuid(), GID: os.Getgid(), HasUID: true, HasGID: true}

	st := runChown(spec, false, 4, root)

	if got := st.Scanned(); got != want {
		t.Fatalf("Scanned=%d, erwartet %d", got, want)
	}
	if got := st.Errors(); got != 0 {
		t.Fatalf("Errors=%d, erwartet 0", got)
	}
	if got := st.Changed(); got != 0 {
		t.Errorf("Changed=%d, erwartet 0 (bereits korrekt)", got)
	}
	if got := st.Skipped(); got != want {
		t.Errorf("Skipped=%d, erwartet %d (alle)", got, want)
	}
}

// Dry-run darf zwar zählen, aber nichts anfassen. Abweichende Ziel-GID -> alle
// "würden geändert"; da -n gesetzt ist, wird kein lchown ausgeführt.
func TestChownDryRunCountsButDoesNotMutate(t *testing.T) {
	root, want := buildTree(t)
	origGid := os.Getgid()
	spec := ownership.Spec{UID: os.Getuid(), GID: origGid + 1, HasUID: true, HasGID: true}

	st := runChown(spec, true, 4, root)

	if got := st.Changed(); got != want {
		t.Errorf("Changed=%d, erwartet %d (alle würden geändert)", got, want)
	}
	if got := gidOf(t, filepath.Join(root, "a.txt")); got != origGid {
		t.Errorf("dry-run hat GID verändert: got %d, erwartet %d", got, origGid)
	}
}

func TestChownDeterministicAcrossWorkerCounts(t *testing.T) {
	spec := ownership.Spec{UID: os.Getuid(), GID: os.Getgid(), HasUID: true, HasGID: true}

	root1, want := buildTree(t)
	st1 := runChown(spec, false, 1, root1)
	root8, _ := buildTree(t)
	st8 := runChown(spec, false, 8, root8)

	if st1.Scanned() != want || st8.Scanned() != want {
		t.Fatalf("Scanned uneinheitlich: -j1=%d -j8=%d, erwartet %d", st1.Scanned(), st8.Scanned(), want)
	}
	if st1.Skipped() != st8.Skipped() {
		t.Errorf("Skipped uneinheitlich: -j1=%d vs -j8=%d", st1.Skipped(), st8.Skipped())
	}
}
