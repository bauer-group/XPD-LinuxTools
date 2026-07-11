//go:build !unix

// fastfind wertet unix-Datei-Metadaten (uid/gid, Mode-Bits) aus und ist damit
// auf Linux/Unix beschränkt. Dieser Stub hält `go build ./...` auf Nicht-Unix-
// Plattformen (z. B. dem Windows-Dev-Rechner) grün.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "fastfind wird nur unter Linux/Unix unterstützt")
	os.Exit(1)
}
