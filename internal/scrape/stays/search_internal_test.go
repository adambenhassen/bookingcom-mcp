package stays

import (
	"strings"
	"testing"
)

func TestSearchURL(t *testing.T) {
	u := SearchURL(SearchParams{
		Destination: "Amsterdam",
		Checkin:     "2026-08-10",
		Checkout:    "2026-08-12",
		Adults:      2,
		Currency:    "EUR",
		MinRating:   8,
		Stars:       4,
		MinPrice:    50,
		MaxPrice:    200,
	})
	for _, want := range []string{
		"ss=Amsterdam", "checkin=2026-08-10", "checkout=2026-08-12",
		"group_adults=2", "selected_currency=EUR",
		// Assert the whole nflt filter, including the %3B separators, so a broken
		// separator (which merges the filters into one booking discards) is caught.
		"nflt=review_score%3D80%3Bclass%3D4%3Bprice%3DEUR-50-200-1",
	} {
		if !strings.Contains(u, want) {
			t.Errorf("SearchURL missing %q in %s", want, u)
		}
	}
}

func TestSearchURLOccupancyAndPriceDefaults(t *testing.T) {
	// Children emit one age param each; a min-only price fills the upper bound.
	u := SearchURL(SearchParams{
		Destination: "Rome", Checkin: "2026-08-10", Checkout: "2026-08-12",
		Adults: 2, Children: 2, Rooms: 3, Currency: "EUR", MinPrice: 50,
	})
	if got := strings.Count(u, "age=10"); got != 2 {
		t.Errorf("want 2 age params for 2 children, got %d in %s", got, u)
	}
	for _, want := range []string{"group_children=2", "no_rooms=3", "price%3DEUR-50-999999-1"} {
		if !strings.Contains(u, want) {
			t.Errorf("SearchURL missing %q in %s", want, u)
		}
	}
	// No rooms given => defaults to 1, not 0.
	if noRooms := SearchURL(SearchParams{Destination: "x", Adults: 1}); !strings.Contains(noRooms, "no_rooms=1") {
		t.Errorf("no_rooms should default to 1: %s", noRooms)
	}
}

func TestNormalizeHotelURL(t *testing.T) {
	u, err := normalizeHotelURL("https://www.booking.com/hotel/es/lleo.html?foo=1#tab-reviews", "2026-09-01", "2026-09-04", "EUR", 3)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"checkin=2026-09-01", "checkout=2026-09-04", "selected_currency=EUR", "lang=en-us", "group_adults=3"} {
		if !strings.Contains(u, want) {
			t.Errorf("missing %q in %s", want, u)
		}
	}
	// group_adults must be a real query param, not buried in the #fragment.
	if i := strings.Index(u, "#"); i >= 0 && strings.Contains(u[i:], "group_adults") {
		t.Errorf("group_adults landed inside the fragment: %s", u)
	}
	rejected := []string{
		"https://evil.com/hotel/x.html",
		"https://booking.com.evil.example/hotel/x.html", // lookalike host
		"https://www.booking.com/searchresults.html",    // not a /hotel/ URL
	}
	for _, raw := range rejected {
		if _, err := normalizeHotelURL(raw, "", "", "", 0); err == nil {
			t.Errorf("expected error for %q", raw)
		}
	}
}
