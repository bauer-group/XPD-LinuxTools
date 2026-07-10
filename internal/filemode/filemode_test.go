package filemode

import (
	"os"
	"testing"
)

func TestParseOctal(t *testing.T) {
	tests := []struct {
		in      string
		want    os.FileMode
		wantErr bool
	}{
		{"644", 0o644, false},
		{"0755", 0o755, false},
		{"600", 0o600, false},
		{"2755", 0o755 | os.ModeSetgid, false},
		{"4755", 0o755 | os.ModeSetuid, false},
		{"1777", 0o777 | os.ModeSticky, false},
		{"7000", os.ModeSetuid | os.ModeSetgid | os.ModeSticky, false},
		{"0", 0, false},
		{"", 0, true},
		{"8", 0, true},      // 8 ist keine Oktalziffer
		{"64a", 0, true},    // Buchstabe
		{"-644", 0, true},   // negativ
		{"17777", 0, true},  // > 7777
		{"777777", 0, true}, // weit außerhalb
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseOctal(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseOctal(%q) err=%v, wantErr=%v", tt.in, err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Errorf("ParseOctal(%q)=%v (%o), want %v (%o)", tt.in, got, got, tt.want, tt.want)
			}
		})
	}
}

func TestNeeds(t *testing.T) {
	dir := os.ModeDir // Typbit, muss ignoriert werden
	tests := []struct {
		name            string
		current, target os.FileMode
		want            bool
	}{
		{"gleich", 0o644, 0o644, false},
		{"perm abweichend", 0o600, 0o644, true},
		{"typbit ignoriert", dir | 0o755, 0o755, false},
		{"setgid abweichend", 0o755, 0o755 | os.ModeSetgid, true},
		{"setgid gleich", 0o755 | os.ModeSetgid, 0o755 | os.ModeSetgid, false},
		{"sticky abweichend", 0o777, 0o777 | os.ModeSticky, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Needs(tt.current, tt.target); got != tt.want {
				t.Errorf("Needs(%o,%o)=%v, want %v", tt.current, tt.target, got, tt.want)
			}
		})
	}
}
