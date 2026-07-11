package main

import (
	"testing"
	"time"
)

func TestParseSize(t *testing.T) {
	tests := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{"0", 0, false},
		{"500", 500, false},
		{"10K", 10 * 1024, false},
		{"20M", 20 * 1024 * 1024, false},
		{"2G", 2 * 1024 * 1024 * 1024, false},
		{"1T", 1 << 40, false},
		{"5m", 5 * 1024 * 1024, false}, // Kleinbuchstabe
		{"", 0, true},
		{"M", 0, true},
		{"10MB", 0, true}, // nur einbuchstabige Suffixe
		{"-5", 0, true},
		{"abc", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseSize(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseSize(%q) err=%v, wantErr=%v", tt.in, err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Errorf("parseSize(%q)=%d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseAge(t *testing.T) {
	tests := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{"30d", 30 * 24 * time.Hour, false},
		{"12h", 12 * time.Hour, false},
		{"45m", 45 * time.Minute, false},
		{"90s", 90 * time.Second, false},
		{"0d", 0, false},
		{"", 0, true},
		{"d", 0, true},
		{"-5h", 0, true},
		{"30days", 0, true},
		{"abc", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseAge(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseAge(%q) err=%v, wantErr=%v", tt.in, err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Errorf("parseAge(%q)=%v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
