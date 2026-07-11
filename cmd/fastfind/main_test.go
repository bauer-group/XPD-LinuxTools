//go:build unix

package main

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/bauer-group/xpd-linuxtools/internal/parwalk"
)

// crit liefert eine criteria mit den korrekten "ungesetzt"-Sentinels (uid/gid -1),
// damit ein Literal nicht versehentlich uid/gid 0 matcht.
func crit() criteria { return criteria{uid: -1, gid: -1} }

// runFind sammelt die passenden Pfade als Set ein.
func runFind(t *testing.T, c criteria, roots ...string) map[string]bool {
	t.Helper()
	preds := buildPredicates(c)
	matches := make(chan string, 256)
	got := map[string]bool{}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for p := range matches {
			got[p] = true
		}
	}()
	parwalk.Run(context.Background(), parwalk.Config{Roots: roots, Workers: 4}, findAction(preds, matches, false))
	close(matches)
	wg.Wait()
	return got
}

// mkTree: root/ + sub/ + sub/a.log (Datei) + link -> a.log (Symlink).
func mkTree(t *testing.T) (root, dir, file, link string) {
	t.Helper()
	root = t.TempDir()
	dir = filepath.Join(root, "sub")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	file = filepath.Join(dir, "a.log")
	if err := os.WriteFile(file, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	link = filepath.Join(root, "link")
	if err := os.Symlink(file, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	return root, dir, file, link
}

func TestFindByType(t *testing.T) {
	root, dir, file, link := mkTree(t)

	c := crit()
	c.typ = "f"
	if got := runFind(t, c, root); len(got) != 1 || !got[file] {
		t.Errorf("type=f: got %v, erwartet nur %s", got, file)
	}
	c.typ = "d"
	if got := runFind(t, c, root); len(got) != 2 || !got[root] || !got[dir] {
		t.Errorf("type=d: got %v, erwartet root+sub", got)
	}
	c.typ = "l"
	if got := runFind(t, c, root); len(got) != 1 || !got[link] {
		t.Errorf("type=l: got %v, erwartet nur %s", got, link)
	}
}

func TestFindByName(t *testing.T) {
	root, _, file, _ := mkTree(t)
	c := crit()
	c.name = "*.log"
	if got := runFind(t, c, root); len(got) != 1 || !got[file] {
		t.Errorf("name=*.log: got %v, erwartet nur %s", got, file)
	}
}

func TestFindWorldWritable(t *testing.T) {
	root := t.TempDir()
	open := filepath.Join(root, "open")
	closed := filepath.Join(root, "closed")
	for _, f := range []string{open, closed} {
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(open, 0o666); err != nil {
		t.Fatal(err)
	}

	c := crit()
	c.worldWrite = true
	got := runFind(t, c, root)
	if !got[open] || got[closed] {
		t.Errorf("world-writable: got %v, erwartet nur %s", got, open)
	}
}

func TestFindSetuid(t *testing.T) {
	root := t.TempDir()
	su := filepath.Join(root, "s")
	plain := filepath.Join(root, "p")
	for _, f := range []string{su, plain} {
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(su, 0o755|os.ModeSetuid); err != nil {
		t.Fatal(err)
	}

	c := crit()
	c.setuid = true
	got := runFind(t, c, root)
	if !got[su] || got[plain] {
		t.Errorf("setuid: got %v, erwartet nur %s", got, su)
	}
}

func TestFindByUID(t *testing.T) {
	root, _, _, _ := mkTree(t)
	me := os.Getuid()

	c := crit()
	c.uid = me
	if got := runFind(t, c, root); len(got) != 4 { // root, sub, a.log, link
		t.Errorf("uid=me: got %d Einträge, erwartet 4", len(got))
	}
	c.uid = me + 1_000_000 // sicher nicht der Owner
	if got := runFind(t, c, root); len(got) != 0 {
		t.Errorf("uid=fremd: got %v, erwartet leer", got)
	}
}

func TestFindBySize(t *testing.T) {
	root := t.TempDir()
	small := filepath.Join(root, "small")
	big := filepath.Join(root, "big")
	if err := os.WriteFile(small, make([]byte, 10), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(big, make([]byte, 5000), 0o644); err != nil {
		t.Fatal(err)
	}

	min := int64(1000)
	c := crit()
	c.typ = "f"
	c.minSize = &min
	got := runFind(t, c, root)
	if !got[big] || got[small] {
		t.Errorf("min-size: got %v, erwartet nur %s", got, big)
	}
}

func TestParseCriteriaErrors(t *testing.T) {
	cases := []struct {
		name                                string
		typ, nm, older, newer, minSz, maxSz string
	}{
		{"bad type", "x", "", "", "", "", ""},
		{"bad name", "", "[", "", "", "", ""},
		{"bad older", "", "", "morgen", "", "", ""},
		{"bad newer", "", "", "", "gestern", "", ""},
		{"bad min-size", "", "", "", "", "viel", ""},
		{"bad max-size", "", "", "", "", "", "10MB"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseCriteria(tc.typ, -1, -1, false, false, false, false,
				tc.nm, tc.older, tc.newer, tc.minSz, tc.maxSz)
			if err == nil {
				t.Errorf("%s: erwartete Fehler, bekam keinen", tc.name)
			}
		})
	}
}

func TestOwnerResolver(t *testing.T) {
	r := newOwnerResolver()
	if r.uidOrphan(0) {
		t.Error("uid 0 (root) sollte bekannt sein")
	}
	if !r.uidOrphan(4_000_000_000) {
		t.Error("uid 4000000000 sollte verwaist sein")
	}
	if r.gidOrphan(0) {
		t.Error("gid 0 (root) sollte bekannt sein")
	}
	if !r.gidOrphan(3_999_999_999) {
		t.Error("gid 3999999999 sollte verwaist sein")
	}
	// Cache liefert konsistentes Ergebnis.
	if !r.uidOrphan(4_000_000_000) {
		t.Error("Cache inkonsistent")
	}
}
