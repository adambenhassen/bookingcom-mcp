// Package flights scrapes flights.booking.com search results.
package flights

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/playwright-community/playwright-go"

	"github.com/adam/bookingcom-mcp/internal/scrape"
)

// SearchParams are the inputs for a flight search.
type SearchParams struct {
	From       string // IATA airport code, e.g. AMS (SearchURL appends .AIRPORT)
	To         string // IATA airport code, e.g. JFK (SearchURL appends .AIRPORT)
	Depart     string // YYYY-MM-DD
	Return     string // YYYY-MM-DD, empty = one way
	Adults     int
	CabinClass string // ECONOMY | PREMIUM_ECONOMY | BUSINESS | FIRST
	Currency   string
	MaxResults int
}

// Flight is one result card.
type Flight struct {
	Carrier   string `json:"carrier,omitempty"`
	Departure string `json:"departure,omitempty"`
	Arrival   string `json:"arrival,omitempty"`
	Duration  string `json:"duration,omitempty"`
	Stops     string `json:"stops,omitempty"`
	Price     string `json:"price"`
	Summary   string `json:"summary,omitempty"`
}

// SearchURL builds the flights.booking.com results URL.
func SearchURL(p SearchParams) string {
	from := strings.ToUpper(p.From) + ".AIRPORT"
	to := strings.ToUpper(p.To) + ".AIRPORT"
	typ := "ONEWAY"
	q := url.Values{}
	q.Set("adults", strconv.Itoa(p.Adults))
	q.Set("cabinClass", p.CabinClass)
	q.Set("depart", p.Depart)
	if p.Return != "" {
		typ = "ROUNDTRIP"
		q.Set("return", p.Return)
	}
	q.Set("type", typ)
	q.Set("locale", "en-us")
	if p.Currency != "" {
		q.Set("currency", p.Currency)
	}
	return fmt.Sprintf("https://flights.booking.com/flights/%s-%s/?%s", from, to, q.Encode())
}

const extractFlightsJS = `
() => Array.from(document.querySelectorAll('[data-testid="flight_card"]')).map(c => {
  const txt = (sel) => c.querySelector(sel)?.textContent?.replace(/\s+/g, ' ').trim() ?? '';
  const all = (sel) => Array.from(c.querySelectorAll(sel)).map(e => e.textContent.replace(/\s+/g, ' ').trim());
  return {
    carrier: all('[data-testid="flight_card_carrier_0"] div, img[alt]').find(Boolean) ??
             (c.querySelector('img')?.alt ?? ''),
    departure: txt('[data-testid="flight_card_segment_departure_time_0"]') + ' ' + txt('[data-testid="flight_card_segment_departure_airport_0"]'),
    arrival: txt('[data-testid="flight_card_segment_destination_time_0"]') + ' ' + txt('[data-testid="flight_card_segment_destination_airport_0"]'),
    duration: txt('[data-testid="flight_card_segment_duration_0"]'),
    stops: txt('[data-testid="flight_card_segment_stops_0"]'),
    price: txt('[data-testid="flight_card_price_main_price"]'),
    summary: txt('[data-testid="flight_card_segment_0"]'),
  };
}).filter(f => f.price)`

// Search runs a flight search and returns result cards.
func Search(ctx context.Context, page playwright.Page, p SearchParams) ([]Flight, error) {
	built := SearchURL(p)
	if err := scrape.Goto(ctx, page, built); err != nil {
		return nil, err
	}
	scrape.DismissCookieBanner(page)
	// Booking redirects to /not-available where Flights isn't offered.
	if strings.Contains(page.URL(), "/not-available") {
		return nil, errors.New("booking.com Flights is not available from this network's region; " +
			"run from a network located in a supported country")
	}
	if err := scrape.WaitForOrBlocked(page, `[data-testid="flight_card"]`, 60000,
		"no flight results appeared (route/date may be invalid, or layout changed)"); err != nil {
		return nil, err
	}
	var flights []Flight
	if err := scrape.EvalJSON(page, extractFlightsJS, &flights); err != nil {
		return nil, err
	}
	if len(flights) == 0 {
		return nil, errors.New("flight cards rendered but none parsed (price selector may have changed)")
	}
	return scrape.Cap(flights, p.MaxResults), nil
}
