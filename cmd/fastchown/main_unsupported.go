//go:build !unix

// fastchown nutzt lchown()/syscall.Stat_t und ist damit auf Linux/Unix
// beschränkt. Dieser Stub sorgt dafür, dass `go build ./...` auch auf
// Nicht-Unix-Plattformen (z. B. dem Windows-Dev-Rechner) durchläuft, statt
// mit "build constraints exclude all Go files" abzubrechen.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "fastchown wird nur unter Linux/Unix unterstützt")
	os.Exit(1)
}
