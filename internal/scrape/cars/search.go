// Package cars scrapes cars.booking.com (rentalcars) search results.
package cars

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/playwright-community/playwright-go"

	"github.com/adam/bookingcom-mcp/internal/scrape"
)

// httpClient is shared across autocomplete lookups so pickup+dropoff resolution
// reuses connections instead of building a fresh Transport per call.
var httpClient = &http.Client{Timeout: 15 * time.Second}

// SearchParams are the inputs for a car rental search.
type SearchParams struct {
	PickupLocation  string
	DropoffLocation string // empty = same as pickup
	PickupDate      string // YYYY-MM-DD
	DropoffDate     string // YYYY-MM-DD
	DriverAge       int    // 0 defaults to 30
	MaxResults      int
}

// Car is one rental offer.
type Car struct {
	Name         string `json:"name"`
	Class        string `json:"class,omitempty"`
	Supplier     string `json:"supplier,omitempty"`
	Price        string `json:"price,omitempty"`
	Seats        string `json:"seats,omitempty"`
	Transmission string `json:"transmission,omitempty"`
}

// location is one FTSAutocomplete.do result.
type location struct {
	Name string  `json:"name"`
	IATA string  `json:"iata"`
	Key  string  `json:"placeKey"`
	Lat  float64 `json:"lat"`
	Lng  float64 `json:"lng"`
}

// resolveLocation resolves free text to a rentalcars location via the public
// autocomplete endpoint (plain HTTP; no browser needed).
func resolveLocation(ctx context.Context, query string) (loc *location, err error) {
	u := "https://cars.booking.com/FTSAutocomplete.do?solrIndex=fts_en&solrRows=1&solrTerm=" + url.QueryEscape(query)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build autocomplete request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15) Gecko/20100101 Firefox/135.0")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("location autocomplete: %w", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close autocomplete body: %w", cerr)
		}
	}()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("location autocomplete: HTTP %d", resp.StatusCode)
	}
	var payload struct {
		Results struct {
			Docs []location `json:"docs"`
		} `json:"results"`
	}
	if derr := json.NewDecoder(resp.Body).Decode(&payload); derr != nil {
		return nil, fmt.Errorf("decode autocomplete response: %w", derr)
	}
	if len(payload.Results.Docs) == 0 || payload.Results.Docs[0].Key == "" {
		return nil, fmt.Errorf("no usable car rental location found for %q", query)
	}
	return &payload.Results.Docs[0], nil
}

// searchURL builds the SearchResults.do URL for a resolved location pair.
func searchURL(pick, drop *location, p SearchParams) (string, error) {
	pu, err := time.Parse("2006-01-02", p.PickupDate)
	if err != nil {
		return "", fmt.Errorf("invalid pickup_date %q (want YYYY-MM-DD)", p.PickupDate)
	}
	do, err := time.Parse("2006-01-02", p.DropoffDate)
	if err != nil {
		return "", fmt.Errorf("invalid dropoff_date %q (want YYYY-MM-DD)", p.DropoffDate)
	}
	q := url.Values{}
	q.Set("location", pick.Key)
	q.Set("locationName", pick.Name)
	if pick.IATA != "" {
		q.Set("locationIata", pick.IATA)
	}
	q.Set("coordinates", fmt.Sprintf("%f,%f", pick.Lat, pick.Lng))
	q.Set("puDay", strconv.Itoa(pu.Day()))
	q.Set("puMonth", strconv.Itoa(int(pu.Month())))
	q.Set("puYear", strconv.Itoa(pu.Year()))
	q.Set("puHour", "10")
	q.Set("puMinute", "0")
	q.Set("doDay", strconv.Itoa(do.Day()))
	q.Set("doMonth", strconv.Itoa(int(do.Month())))
	q.Set("doYear", strconv.Itoa(do.Year()))
	q.Set("doHour", "10")
	q.Set("doMinute", "0")
	age := p.DriverAge
	if age <= 0 {
		age = 30
	}
	q.Set("driversAge", strconv.Itoa(age))
	if drop == nil {
		q.Set("puSameAsDo", "on")
	} else {
		q.Set("dropLocation", drop.Key)
		q.Set("dropLocationName", drop.Name)
		if drop.IATA != "" {
			q.Set("dropLocationIata", drop.IATA)
		}
	}
	return "https://cars.booking.com/SearchResults.do?" + q.Encode(), nil
}

// Result cards carry no stable test ids, so extraction anchors on the
// "<model> / or similar <class>" text every card contains and walks up to the
// enclosing card element.
const extractCarsJS = `
() => {
  const layout = document.querySelector('[data-testid="search-results-layout"]');
  if (!layout) return [];
  const anchors = Array.from(layout.querySelectorAll('*'))
    .filter(e => e.children.length === 0 && /^or similar/i.test(e.textContent.trim()));
  const seen = new Set();
  const cars = [];
  // Detect a price+seats block without depending on a specific currency symbol,
  // so results still parse in kr/CHF/zł/¥ regions, not just €/$/£.
  const priceRe = /(?:[€$£¥₹]|US\$|CHF|kr\.?|zł|R\$|₺|฿)\s?[\d.,]+/gi;
  for (const a of anchors) {
    let card = a;
    for (let i = 0; i < 10 && card; i++) {
      const t = card.innerText || '';
      if (/\bseats?\b/i.test(t) && /\d/.test(t)) break;
      card = card.parentElement;
    }
    if (!card || seen.has(card)) continue;
    seen.add(card);
    const text = card.innerText;
    const lines = text.split('\n').map(s => s.trim()).filter(Boolean);
    const simIdx = lines.findIndex(l => /^or similar/i.test(l));
    const prices = text.match(priceRe) || [];
    cars.push({
      name: simIdx > 0 ? lines[simIdx - 1] : '',
      class: simIdx >= 0 ? lines[simIdx].replace(/^or similar\s*/i, '') : '',
      supplier: (card.querySelector('img[alt]:not([alt=""])') || {alt: ''}).alt,
      price: prices.length ? prices[prices.length - 1] : '',
      seats: (text.match(/(\d+)\s+seats?/i) || ['',''])[1],
      transmission: (text.match(/Automatic|Manual/i) || [''])[0],
    });
  }
  return cars.filter(c => c.name && c.price);
}`

// Search resolves locations, opens the results page and scrapes offers.
func Search(ctx context.Context, page playwright.Page, p SearchParams) ([]Car, error) {
	pick, err := resolveLocation(ctx, p.PickupLocation)
	if err != nil {
		return nil, err
	}
	var drop *location
	if p.DropoffLocation != "" && p.DropoffLocation != p.PickupLocation {
		if drop, err = resolveLocation(ctx, p.DropoffLocation); err != nil {
			return nil, err
		}
	}
	u, err := searchURL(pick, drop, p)
	if err != nil {
		return nil, err
	}
	if err := scrape.Goto(ctx, page, u); err != nil {
		return nil, err
	}
	scrape.DismissCookieBanner(page)
	if err := scrape.WaitForOrBlocked(page, `[data-testid="search-results-layout"]`, 60000,
		fmt.Sprintf("no car results appeared at %s (location/dates may be invalid, or layout changed)", u)); err != nil {
		return nil, err
	}
	// Cards hydrate after the layout mounts; poll briefly instead of one fixed sleep.
	const attempts = 15
	var cars []Car
	for i := range attempts {
		if err := scrape.EvalJSON(page, extractCarsJS, &cars); err != nil {
			return nil, err
		}
		if len(cars) > 0 {
			break
		}
		if i < attempts-1 {
			// Cards hydrate with no DOM signal to await; a short fixed poll is
			// deliberate. The browser renders in its own process, so this Go-side
			// pause lets more cards mount before the next extraction.
			time.Sleep(2 * time.Second)
		}
	}
	if len(cars) == 0 {
		return nil, fmt.Errorf("car results page loaded but no offers parsed (layout may have changed): %s", u)
	}
	return scrape.Cap(cars, p.MaxResults), nil
}
