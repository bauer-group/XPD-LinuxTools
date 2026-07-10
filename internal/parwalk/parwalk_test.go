package parwalk

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// buildTree legt Root + Unterordner + Dateien an (plattformneutral, keine
// Symlinks – die sind auf Windows privilegiert). Liefert Gesamtzahl der
// Einträge sowie die Anzahl Verzeichnisse.
func buildTree(t *testing.T) (root string, entries, dirs int) {
	t.Helper()
	root = t.TempDir()
	for _, d := range []string{"a", "a/x", "b"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	for _, f := range []string{"a/f1", "a/x/f2", "b/f3", "f4"} {
		if err := os.WriteFile(filepath.Join(root, f), []byte("x"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	// Einträge: root + a + a/x + b (4 dirs) + 4 files = 8
	return root, 8, 4
}

func TestRunCountsByResult(t *testing.T) {
	root, entries, dirs := buildTree(t)

	// Action: Verzeichnisse überspringen, Dateien "ändern".
	st, interrupted := Run(context.Background(), Config{Roots: []string{root}, Workers: 4},
		func(_ string, d os.DirEntry) Result {
			if d.IsDir() {
				return Skipped
			}
			return Changed
		})

	if interrupted {
		t.Fatal("unerwartet abgebrochen")
	}
	if got := st.Scanned(); got != int64(entries) {
		t.Errorf("Scanned=%d, erwartet %d", got, entries)
	}
	if got := st.Skipped(); got != int64(dirs) {
		t.Errorf("Skipped=%d, erwartet %d (dirs)", got, dirs)
	}
	if got := st.Changed(); got != int64(entries-dirs) {
		t.Errorf("Changed=%d, erwartet %d (files)", got, entries-dirs)
	}
	if got := st.Errors(); got != 0 {
		t.Errorf("Errors=%d, erwartet 0", got)
	}
}

func TestRunDeterministicAcrossWorkers(t *testing.T) {
	action := func(_ string, d os.DirEntry) Result {
		if d.IsDir() {
			return Skipped
		}
		return Changed
	}
	r1, _, _ := buildTree(t)
	st1, _ := Run(context.Background(), Config{Roots: []string{r1}, Workers: 1}, action)
	r8, _, _ := buildTree(t)
	st8, _ := Run(context.Background(), Config{Roots: []string{r8}, Workers: 8}, action)

	if st1.Scanned() != st8.Scanned() || st1.Changed() != st8.Changed() || st1.Skipped() != st8.Skipped() {
		t.Errorf("uneinheitlich: -j1(scan=%d chg=%d skip=%d) vs -j8(scan=%d chg=%d skip=%d)",
			st1.Scanned(), st1.Changed(), st1.Skipped(), st8.Scanned(), st8.Changed(), st8.Skipped())
	}
}

func TestRunCountsErrors(t *testing.T) {
	root, entries, _ := buildTree(t)
	st, _ := Run(context.Background(), Config{Roots: []string{root}, Workers: 2},
		func(_ string, _ os.DirEntry) Result { return Errored })
	if got := st.Errors(); got != int64(entries) {
		t.Errorf("Errors=%d, erwartet %d", got, entries)
	}
}
