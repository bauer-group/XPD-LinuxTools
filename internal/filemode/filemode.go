// Package filemode parst oktale chmod-Modi und vergleicht Dateimodi.
//
// Besonderheit: Go repräsentiert die Sonderbits setuid/setgid/sticky NICHT in
// den unteren Bits von os.FileMode, sondern als eigene Flags (os.ModeSetuid …).
// Dieser Parser mappt die oktalen Bits 4000/2000/1000 explizit auf diese Flags,
// damit os.Chmod sie korrekt setzt. Der Vergleich (Needs) maskiert genau die
// Bits, die chmod verändert.
//
// Plattformneutral (keine Build-Tags) – auf jeder Plattform unit-testbar.
package filemode

import (
	"fmt"
	"os"
	"strconv"
)

// ChangeMask sind die Bits, die chmod verändert: die 9 Permission-Bits plus
// setuid, setgid und sticky.
const ChangeMask = os.ModePerm | os.ModeSetuid | os.ModeSetgid | os.ModeSticky

// ParseOctal parst einen oktalen Modus ("644", "0755", "2775") und liefert die
// zugehörige os.FileMode inkl. setuid/setgid/sticky.
func ParseOctal(s string) (os.FileMode, error) {
	if s == "" {
		return 0, fmt.Errorf("leerer Modus")
	}
	v, err := strconv.ParseUint(s, 8, 32)
	if err != nil {
		return 0, fmt.Errorf("ungültiger oktaler Modus %q (erwartet z. B. 644, 0755, 2775)", s)
	}
	if v > 0o7777 {
		return 0, fmt.Errorf("oktaler Modus %q außerhalb des Bereichs (max 7777)", s)
	}

	m := os.FileMode(v & 0o777)
	if v&0o4000 != 0 {
		m |= os.ModeSetuid
	}
	if v&0o2000 != 0 {
		m |= os.ModeSetgid
	}
	if v&0o1000 != 0 {
		m |= os.ModeSticky
	}
	return m, nil
}

// Needs meldet, ob current auf target geändert werden muss – verglichen werden
// nur die von chmod veränderten Bits (ChangeMask), Typbits werden ignoriert.
func Needs(current, target os.FileMode) bool {
	return current&ChangeMask != target&ChangeMask
}
