package cars

import (
	"strings"
	"testing"
)

func TestSearchURL(t *testing.T) {
	pick := &location{Name: "Barcelona El Prat Airport", IATA: "BCN", Key: "41", Lat: 41.2969, Lng: 2.07833}
	u, err := searchURL(pick, nil, SearchParams{PickupDate: "2026-09-01", DropoffDate: "2026-09-04"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"location=41", "locationIata=BCN", "puDay=1", "puMonth=9", "puYear=2026",
		"doDay=4", "doMonth=9", "doYear=2026", "puSameAsDo=on", "driversAge=30",
	} {
		if !strings.Contains(u, want) {
			t.Errorf("searchURL missing %q in %s", want, u)
		}
	}
	if _, err := searchURL(pick, nil, SearchParams{PickupDate: "bad", DropoffDate: "2026-09-04"}); err == nil {
		t.Error("expected error for bad pickup date")
	}
	if _, err := searchURL(pick, nil, SearchParams{PickupDate: "2026-09-01", DropoffDate: "bad"}); err == nil {
		t.Error("expected error for bad dropoff date")
	}
}

func TestSearchURLDistinctDropoff(t *testing.T) {
	pick := &location{Name: "Barcelona El Prat Airport", IATA: "BCN", Key: "41", Lat: 41.2969, Lng: 2.07833}
	drop := &location{Name: "Madrid Barajas Airport", IATA: "MAD", Key: "77", Lat: 40.4936, Lng: -3.56676}
	u, err := searchURL(pick, drop, SearchParams{PickupDate: "2026-09-01", DropoffDate: "2026-09-04"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"dropLocation=77", "dropLocationIata=MAD"} {
		if !strings.Contains(u, want) {
			t.Errorf("searchURL missing %q in %s", want, u)
		}
	}
	if strings.Contains(u, "puSameAsDo=on") {
		t.Errorf("distinct dropoff must not set puSameAsDo=on: %s", u)
	}
}
