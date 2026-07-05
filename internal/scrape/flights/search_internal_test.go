package flights

import (
	"strings"
	"testing"
)

func TestSearchURL(t *testing.T) {
	u := SearchURL(SearchParams{
		From: "ams", To: "JFK", Depart: "2026-09-01", Return: "2026-09-08",
		Adults: 2, CabinClass: "ECONOMY", Currency: "EUR",
	})
	for _, want := range []string{
		"flights/AMS.AIRPORT-JFK.AIRPORT/", "type=ROUNDTRIP", "depart=2026-09-01",
		"return=2026-09-08", "adults=2", "cabinClass=ECONOMY", "currency=EUR",
	} {
		if !strings.Contains(u, want) {
			t.Errorf("SearchURL missing %q in %s", want, u)
		}
	}
	if !strings.Contains(SearchURL(SearchParams{From: "AMS", To: "BCN", Depart: "2026-09-01", Adults: 1}), "type=ONEWAY") {
		t.Error("one-way search should set type=ONEWAY")
	}
}
