package ownership

import "testing"

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		spec    string
		want    Spec
		wantErr bool
	}{
		{"uid und gid", "1000:1000", Spec{UID: 1000, GID: 1000, HasUID: true, HasGID: true}, false},
		{"unterschiedliche uid gid", "0:2000", Spec{UID: 0, GID: 2000, HasUID: true, HasGID: true}, false},
		{"nur uid", "1000", Spec{UID: 1000, HasUID: true}, false},
		{"nur gid", ":1000", Spec{GID: 1000, HasGID: true}, false},
		{"uid mit leerer gid", "1000:", Spec{UID: 1000, HasUID: true}, false},
		{"leer", "", Spec{}, true},
		{"nur doppelpunkt", ":", Spec{}, true},
		{"nicht-numerische uid", "root:1000", Spec{}, true},
		{"nicht-numerische gid", "1000:staff", Spec{}, true},
		{"negativ ist ungueltig", "-5", Spec{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.spec)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Parse(%q) error = %v, wantErr %v", tt.spec, err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if got != tt.want {
				t.Errorf("Parse(%q) = %+v, want %+v", tt.spec, got, tt.want)
			}
		})
	}
}

func TestResolve(t *testing.T) {
	tests := []struct {
		name           string
		spec           Spec
		curUID, curGID int
		wantUID        int
		wantGID        int
		wantChanged    bool
	}{
		{"beide gleich -> skip", Spec{UID: 1000, GID: 1000, HasUID: true, HasGID: true}, 1000, 1000, 1000, 1000, false},
		{"uid abweichend", Spec{UID: 1000, GID: 1000, HasUID: true, HasGID: true}, 0, 1000, 1000, 1000, true},
		{"gid abweichend", Spec{UID: 1000, GID: 1000, HasUID: true, HasGID: true}, 1000, 0, 1000, 1000, true},
		{"beide abweichend", Spec{UID: 1000, GID: 1000, HasUID: true, HasGID: true}, 0, 0, 1000, 1000, true},
		{"nur gid gesetzt, gleich", Spec{GID: 1000, HasGID: true}, 500, 1000, 500, 1000, false},
		{"nur gid gesetzt, abweichend -> uid bleibt", Spec{GID: 1000, HasGID: true}, 500, 0, 500, 1000, true},
		{"nur uid gesetzt -> gid bleibt", Spec{UID: 1000, HasUID: true}, 0, 42, 1000, 42, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uid, gid, changed := tt.spec.Resolve(tt.curUID, tt.curGID)
			if uid != tt.wantUID || gid != tt.wantGID || changed != tt.wantChanged {
				t.Errorf("Resolve(%d,%d) = (%d,%d,%v), want (%d,%d,%v)",
					tt.curUID, tt.curGID, uid, gid, changed, tt.wantUID, tt.wantGID, tt.wantChanged)
			}
		})
	}
}
