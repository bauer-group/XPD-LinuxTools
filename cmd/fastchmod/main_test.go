//go:build unix

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/bauer-group/xpd-linuxtools/internal/filemode"
	"github.com/bauer-group/xpd-linuxtools/internal/parwalk"
)

func octal(t *testing.T, s string) os.FileMode {
	t.Helper()
	m, err := filemode.ParseOctal(s)
	if err != nil {
		t.Fatalf("ParseOctal(%q): %v", s, err)
	}
	return m
}

// modeOf liefert die von chmod veränderten Bits eines Eintrags.
func modeOf(t *testing.T, path string) os.FileMode {
	t.Helper()
	fi, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("lstat %s: %v", path, err)
	}
	return fi.Mode() & filemode.ChangeMask
}

// buildModeTree legt Root + Unterverzeichnis (0700) + Datei (0600) an; Modi via
// Chmod exakt gesetzt (umask-unabhängig).
func buildModeTree(t *testing.T) (root, dir, file string) {
	t.Helper()
	root = t.TempDir()
	dir = filepath.Join(root, "sub")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	file = filepath.Join(dir, "f.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatalf("chmod dir: %v", err)
	}
	if err := os.Chmod(file, 0o600); err != nil {
		t.Fatalf("chmod file: %v", err)
	}
	return root, dir, file
}

func TestResolveModes(t *testing.T) {
	// uniform: MODE PATH...
	d, f, roots, err := resolveModes("", "", []string{"644", "/p1", "/p2"})
	if err != nil {
		t.Fatalf("uniform: %v", err)
	}
	if d == nil || f == nil || *d != octal(t, "644") || *f != octal(t, "644") || len(roots) != 2 {
		t.Errorf("uniform falsch: d=%v f=%v roots=%v", d, f, roots)
	}

	// split: -d -f PATH...
	d, f, roots, err = resolveModes("755", "644", []string{"/p"})
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	if d == nil || *d != octal(t, "755") || f == nil || *f != octal(t, "644") || len(roots) != 1 {
		t.Errorf("split falsch: d=%v f=%v roots=%v", d, f, roots)
	}

	// dir-only: -d PATH...  (Files unangetastet -> fileMode nil)
	d, f, _, err = resolveModes("2775", "", []string{"/p"})
	if err != nil || d == nil || f != nil {
		t.Errorf("dir-only falsch: d=%v f=%v err=%v", d, f, err)
	}

	// file-only: -f PATH...  (Dirs unangetastet -> dirMode nil)
	d, f, _, err = resolveModes("", "600", []string{"/p"})
	if err != nil || f == nil || d != nil {
		t.Errorf("file-only falsch: d=%v f=%v err=%v", d, f, err)
	}

	// Fehlerfälle
	for _, tc := range []struct {
		name        string
		dirF, fileF string
		args        []string
	}{
		{"uniform ohne pfad", "", "", []string{"644"}},
		{"keine args", "", "", nil},
		{"split ohne pfad", "755", "644", nil},
		{"ungültiger uniform-modus", "", "", []string{"888", "/p"}},
		{"ungültiger dir-modus", "999", "", []string{"/p"}},
	} {
		if _, _, _, err := resolveModes(tc.dirF, tc.fileF, tc.args); err == nil {
			t.Errorf("%s: erwartete Fehler, bekam keinen", tc.name)
		}
	}
}

func TestChmodSplitModesAndSkip(t *testing.T) {
	root, dir, file := buildModeTree(t)
	dm, fm := octal(t, "755"), octal(t, "644")

	st, _ := parwalk.Run(context.Background(), parwalk.Config{Roots: []string{root}, Workers: 4},
		chmodAction(&dm, &fm, false, false))

	if st.Errors() != 0 {
		t.Fatalf("Errors=%d", st.Errors())
	}
	if got := modeOf(t, dir); got != 0o755 {
		t.Errorf("dir mode=%o, erwartet 755", got)
	}
	if got := modeOf(t, file); got != 0o644 {
		t.Errorf("file mode=%o, erwartet 644", got)
	}
	if got := modeOf(t, root); got != 0o755 {
		t.Errorf("root mode=%o, erwartet 755", got)
	}

	// Re-run: bereits korrekt -> nichts geändert, alles übersprungen.
	st2, _ := parwalk.Run(context.Background(), parwalk.Config{Roots: []string{root}, Workers: 4},
		chmodAction(&dm, &fm, false, false))
	if st2.Changed() != 0 {
		t.Errorf("Re-run Changed=%d, erwartet 0", st2.Changed())
	}
	if st2.Skipped() != st2.Scanned() {
		t.Errorf("Re-run: skip=%d scan=%d (nicht alle übersprungen)", st2.Skipped(), st2.Scanned())
	}
}

func TestChmodDryRunDoesNotMutate(t *testing.T) {
	root, dir, file := buildModeTree(t)
	dm, fm := octal(t, "755"), octal(t, "644")

	st, _ := parwalk.Run(context.Background(), parwalk.Config{Roots: []string{root}, Workers: 4},
		chmodAction(&dm, &fm, true, false))

	if st.Changed() == 0 {
		t.Error("dry-run: erwartet Changed>0")
	}
	if got := modeOf(t, file); got != 0o600 {
		t.Errorf("dry-run hat file verändert: %o", got)
	}
	if got := modeOf(t, dir); got != 0o700 {
		t.Errorf("dry-run hat dir verändert: %o", got)
	}
}

func TestChmodSkipsSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if err := os.Chmod(target, 0o600); err != nil {
		t.Fatalf("chmod target: %v", err)
	}
	if err := os.Symlink(target, filepath.Join(root, "link")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	dm, fm := octal(t, "777"), octal(t, "777")
	parwalk.Run(context.Background(), parwalk.Config{Roots: []string{root}, Workers: 2},
		chmodAction(&dm, &fm, false, false))

	// Symlink übersprungen -> Ziel bleibt 0600 (kein Follow).
	if got := modeOf(t, target); got != 0o600 {
		t.Errorf("symlink-Ziel verändert: %o, erwartet 600", got)
	}
}
