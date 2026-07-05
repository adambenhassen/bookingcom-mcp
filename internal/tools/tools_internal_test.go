package tools

import "testing"

func TestDateRange(t *testing.T) {
	cases := []struct {
		start, end string
		allowEqual bool
		wantErr    bool
	}{
		{"2026-09-01", "2026-09-04", false, false},
		{"2026-09-04", "2026-09-01", false, true}, // end before start
		{"2026-09-01", "2026-09-01", false, true}, // equal, not allowed
		{"2026-09-01", "2026-09-01", true, false}, // equal, allowed (same-day return)
		{"not-a-date", "2026-09-04", false, true},
		{"2026-09-01", "bad", false, true},
	}
	for _, c := range cases {
		err := dateRange("start", c.start, "end", c.end, c.allowEqual)
		if (err != nil) != c.wantErr {
			t.Errorf("dateRange(%q,%q,allowEqual=%v) err=%v, wantErr=%v", c.start, c.end, c.allowEqual, err, c.wantErr)
		}
	}
}

func TestValidIATA(t *testing.T) {
	cases := map[string]bool{
		"AMS": true, "jfk": true,
		"AM": false, "AMST": false, "a/b": false, "A1B": false, "": false,
	}
	for code, want := range cases {
		if got := validIATA(code); got != want {
			t.Errorf("validIATA(%q) = %v, want %v", code, got, want)
		}
	}
}

func TestValidCabinClass(t *testing.T) {
	cases := map[string]bool{
		"ECONOMY": true, "PREMIUM_ECONOMY": true, "BUSINESS": true, "FIRST": true,
		"economy": false, "Business": false, "COACH": false, "": false,
	}
	for cc, want := range cases {
		if got := validCabinClass(cc); got != want {
			t.Errorf("validCabinClass(%q) = %v, want %v", cc, got, want)
		}
	}
}
