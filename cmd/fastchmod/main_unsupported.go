//go:build !unix

// fastchmod nutzt unix-Modus-Semantik (setuid/setgid/sticky, Symlink-Handling)
// und ist auf Linux/Unix beschränkt. Dieser Stub hält `go build ./...` auf
// Nicht-Unix-Plattformen (z. B. dem Windows-Dev-Rechner) grün.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "fastchmod wird nur unter Linux/Unix unterstützt")
	os.Exit(1)
}
