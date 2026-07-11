package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// parseSize parst Größenangaben wie "500", "10K", "20M", "2G", "1T" (Basis 1024)
// in Bytes. Ohne Suffix werden Bytes angenommen.
func parseSize(s string) (int64, error) {
	if s == "" {
		return 0, fmt.Errorf("leere Größe")
	}
	mult := int64(1)
	switch s[len(s)-1] {
	case 'K', 'k':
		mult, s = 1<<10, s[:len(s)-1]
	case 'M', 'm':
		mult, s = 1<<20, s[:len(s)-1]
	case 'G', 'g':
		mult, s = 1<<30, s[:len(s)-1]
	case 'T', 't':
		mult, s = 1<<40, s[:len(s)-1]
	}
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("ungültige Größe (erwartet z. B. 100, 20M, 2G)")
	}
	return n * mult, nil
}

// parseAge parst Zeitspannen wie "30d", "12h", "45m", "90s" in time.Duration.
// Tage ("d") ergänzen die von time.ParseDuration unterstützten Einheiten.
func parseAge(s string) (time.Duration, error) {
	if s == "" {
		return 0, fmt.Errorf("leere Zeitspanne")
	}
	if strings.HasSuffix(s, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil || n < 0 {
			return 0, fmt.Errorf("ungültige Zeitspanne (erwartet z. B. 30d, 12h)")
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("ungültige Zeitspanne (erwartet z. B. 30d, 12h, 45m)")
	}
	return d, nil
}
