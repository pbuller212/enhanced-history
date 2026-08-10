package tui

import "time"

// parseDate parses a YYYY-MM-DD date string into a UTC unix timestamp. ok is
// false when s is empty or malformed, in which case the caller should leave
// the corresponding filter field unset rather than reject the input — the
// TUI filters live as the user types, so a partially-typed date must not
// surface as an error.
func parseDate(s string) (int64, bool) {
	if s == "" {
		return 0, false
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return 0, false
	}
	return t.UTC().Unix(), true
}

// formatTs renders a unix timestamp as a UTC "YYYY-MM-DD HH:MM" string.
func formatTs(ts int64) string {
	return time.Unix(ts, 0).UTC().Format("2006-01-02 15:04")
}
