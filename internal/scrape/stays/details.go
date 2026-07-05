package stays

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/playwright-community/playwright-go"

	"github.com/adam/bookingcom-mcp/internal/scrape"
)

// Details holds the scraped property page content.
type Details struct {
	Name        string   `json:"name"`
	Address     string   `json:"address,omitempty"`
	Description string   `json:"description,omitempty"`
	Rating      string   `json:"rating,omitempty"`
	Facilities  []string `json:"facilities,omitempty"`
	Latitude    string   `json:"latitude,omitempty"`
	Longitude   string   `json:"longitude,omitempty"`
	Photos      []string `json:"photos,omitempty"`
	CheckinFrom string   `json:"checkinFrom,omitempty"`
	Checkout    string   `json:"checkout,omitempty"`
}

const extractDetailsJS = `
() => {
  const txt = (sel) => document.querySelector(sel)?.textContent?.trim() ?? '';
  const latlng = document.querySelector('[data-atlas-latlng]')?.getAttribute('data-atlas-latlng') ?? '';
  const [lat, lng] = latlng.split(',');
  return {
    name: txt('#hp_hotel_name_reviews') || txt('h2.pp-header__title') || txt('[data-capla-component-boundary*="PropertyHeaderName"] h2') || txt('h2'),
    address: txt('[data-testid="PropertyHeaderAddressDesktop-wrapper"]') || txt('.hp_address_subtitle') || txt('[data-node_tt_id="location_score_tooltip"]'),
    description: txt('[data-testid="property-description"]') || txt('#property_description_content'),
    rating: txt('[data-testid="review-score-right-component"]'),
    facilities: Array.from(document.querySelectorAll('[data-testid="property-most-popular-facilities-wrapper"] li, .hotel-facilities-group li'))
      .map(li => li.textContent.trim()).filter((v, i, a) => v && a.indexOf(v) === i),
    latitude: (lat ?? '').trim(),
    longitude: (lng ?? '').trim(),
    photos: Array.from(document.querySelectorAll('a[data-fancybox="gallery"] img, [data-testid="GalleryDesktop-wrapper"] img, .bh-photo-grid img'))
      .map(img => img.src).filter(Boolean).slice(0, 10),
    checkinFrom: txt('#checkin_policy') , checkout: txt('#checkout_policy'),
  };
}`

// GetDetails scrapes a property page. hotelURL must be a booking.com /hotel/ URL.
func GetDetails(ctx context.Context, page playwright.Page, hotelURL, checkin, checkout, currency string) (*Details, error) {
	u, err := normalizeHotelURL(hotelURL, checkin, checkout, currency, 0)
	if err != nil {
		return nil, err
	}
	if err := scrape.Goto(ctx, page, u); err != nil {
		return nil, err
	}
	scrape.DismissCookieBanner(page)
	if err := scrape.WaitForOrBlocked(page, `h2.pp-header__title, #hp_hotel_name_reviews`, 30000,
		"property page did not render a title (layout may have changed)"); err != nil {
		return nil, err
	}
	var d Details
	if err := scrape.EvalJSON(page, extractDetailsJS, &d); err != nil {
		return nil, err
	}
	if d.Name == "" {
		return nil, fmt.Errorf("could not extract property name from %s (layout may have changed)", u)
	}
	return &d, nil
}

// normalizeHotelURL validates a booking.com /hotel/ URL and applies query params.
// A non-zero adults sets group_adults via the query (not string concat) so a
// pre-existing #fragment on the URL can't swallow the parameter.
func normalizeHotelURL(raw, checkin, checkout, currency string, adults int) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || !isBookingHost(u.Host) || !strings.Contains(u.Path, "/hotel/") {
		return "", fmt.Errorf("hotel_url must be an http(s) booking.com /hotel/ URL, got %q", raw)
	}
	q := u.Query()
	q.Set("lang", "en-us")
	if checkin != "" {
		q.Set("checkin", checkin)
	}
	if checkout != "" {
		q.Set("checkout", checkout)
	}
	if currency != "" {
		q.Set("selected_currency", currency)
	}
	if adults > 0 {
		q.Set("group_adults", strconv.Itoa(adults))
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// isBookingHost reports whether host is booking.com or a subdomain of it,
// rejecting lookalikes such as booking.com.evil.example.
func isBookingHost(host string) bool {
	host = strings.ToLower(host)
	return host == "booking.com" || strings.HasSuffix(host, ".booking.com")
}
