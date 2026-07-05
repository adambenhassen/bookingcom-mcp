package stays

import (
	"context"
	"errors"
	"fmt"

	"github.com/playwright-community/playwright-go"

	"github.com/adam/bookingcom-mcp/internal/scrape"
)

// Room is one bookable room option for the requested dates.
type Room struct {
	Name         string `json:"name"`
	Sleeps       string `json:"sleeps,omitempty"`
	Price        string `json:"price,omitempty"`
	Conditions   string `json:"conditions,omitempty"`
	BedTypes     string `json:"bedTypes,omitempty"`
	Availability string `json:"availability,omitempty"`
}

const extractRoomsJS = `
() => {
  const rows = Array.from(document.querySelectorAll('#hprt-table tbody tr'));
  let lastName = '', lastBeds = '';
  return rows.map(r => {
    const txt = (sel) => r.querySelector(sel)?.textContent?.replace(/\s+/g, ' ').trim() ?? '';
    const name = txt('.hprt-roomtype-icon-link') || txt('[data-testid="room-name"]');
    const beds = txt('.hprt-roomtype-bed');
    if (name) { lastName = name; lastBeds = beds; }
    return {
      name: name || lastName,
      bedTypes: beds || lastBeds,
      sleeps: (r.querySelector('.hprt-occupancy-occupancy-info')?.getAttribute('aria-label') ?? '') ||
              String(r.querySelectorAll('.hprt-occupancy-occupancy-info .bicon-occupancy').length || ''),
      price: txt('.hprt-price-price, .prco-valign-middle-helper, [data-testid="price-and-discounted-price"]'),
      conditions: Array.from(r.querySelectorAll('.hprt-conditions li')).map(li => li.textContent.replace(/\s+/g,' ').trim()).join('; '),
      availability: txt('.hprt-nos-select option:checked') ? 'available' : (txt('.hprt-table-cell-conditions .urgency_message_red') || 'available'),
    };
  }).filter(r => r.name && r.price);
}`

// CheckAvailability scrapes room-level rates for the given dates.
func CheckAvailability(ctx context.Context, page playwright.Page, hotelURL, checkin, checkout, currency string, adults int) ([]Room, error) {
	if checkin == "" || checkout == "" {
		return nil, errors.New("checkin and checkout are required")
	}
	u, err := normalizeHotelURL(hotelURL, checkin, checkout, currency, adults)
	if err != nil {
		return nil, err
	}
	if err := scrape.Goto(ctx, page, u); err != nil {
		return nil, err
	}
	scrape.DismissCookieBanner(page)
	if err := scrape.WaitForOrBlocked(page, "#hprt-table", 30000,
		"no availability table for these dates (sold out, or layout changed)"); err != nil {
		return nil, err
	}
	var rooms []Room
	if err := scrape.EvalJSON(page, extractRoomsJS, &rooms); err != nil {
		return nil, err
	}
	if len(rooms) == 0 {
		// The table rendered but no priced rows parsed: either genuinely sold out
		// or the room/price selectors drifted. Surface it instead of "0 rooms".
		return nil, fmt.Errorf("availability table rendered but no rooms parsed (sold out, or price selector changed): %s", u)
	}
	return rooms, nil
}
