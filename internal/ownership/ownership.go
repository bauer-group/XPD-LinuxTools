// Package ownership parst chown-artige Ownership-Angaben der Form "UID[:GID]".
//
// Der Parser ist bewusst plattformneutral (keine Build-Tags, keine syscalls),
// damit die Kernlogik – welches UID/GID gesetzt werden soll und ob überhaupt
// eine Änderung nötig ist – auf jeder Plattform unit-testbar bleibt. Die
// eigentlichen lchown()-Aufrufe liegen im jeweiligen Tool (cmd/fastchown).
package ownership

import (
	"fmt"
	"strconv"
	"strings"
)

// Spec beschreibt eine geparste Ownership-Angabe. HasUID/HasGID zeigen an,
// welche Komponenten tatsächlich gesetzt werden sollen – eine fehlende
// Komponente (z. B. bei ":1000" oder "1000:") bleibt unverändert.
type Spec struct {
	UID    int
	GID    int
	HasUID bool
	HasGID bool
}

// Parse zerlegt eine chown-artige Angabe:
//
//	"1000:1000" -> UID und GID setzen
//	"1000"      -> nur UID setzen
//	":1000"     -> nur GID setzen (wie `chown :1000`)
//	"1000:"     -> nur UID setzen (leere GID-Komponente = unverändert)
//
// Es werden ausschließlich numerische IDs unterstützt (keine Namensauflösung),
// da das Tool typischerweise ohne /etc/passwd-Kontext auf fremden Trees läuft.
func Parse(spec string) (Spec, error) {
	var s Spec
	uidPart, gidPart, hasColon := strings.Cut(spec, ":")

	if uidPart != "" {
		uid, err := strconv.Atoi(uidPart)
		if err != nil || uid < 0 {
			return Spec{}, fmt.Errorf("ungültige UID %q (erwartet nicht-negative Ganzzahl)", uidPart)
		}
		s.UID, s.HasUID = uid, true
	}

	if hasColon && gidPart != "" {
		gid, err := strconv.Atoi(gidPart)
		if err != nil || gid < 0 {
			return Spec{}, fmt.Errorf("ungültige GID %q (erwartet nicht-negative Ganzzahl)", gidPart)
		}
		s.GID, s.HasGID = gid, true
	}

	if !s.HasUID && !s.HasGID {
		return Spec{}, fmt.Errorf("weder UID noch GID angegeben (erwartet UID:GID, UID oder :GID)")
	}
	return s, nil
}

// Resolve berechnet für einen Eintrag mit den aktuellen Werten curUID/curGID
// das Ziel-UID/GID und ob überhaupt eine Änderung nötig ist. Nicht gesetzte
// Komponenten (HasUID/HasGID == false) bleiben unverändert – dadurch ist ein
// skip-if-unchanged trivial: changed == false bedeutet "nichts zu tun".
func (s Spec) Resolve(curUID, curGID int) (uid, gid int, changed bool) {
	uid, gid = curUID, curGID
	if s.HasUID && s.UID != curUID {
		uid, changed = s.UID, true
	}
	if s.HasGID && s.GID != curGID {
		gid, changed = s.GID, true
	}
	return uid, gid, changed
}
