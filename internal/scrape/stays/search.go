// Package stays scrapes Booking.com accommodation pages.
package stays

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"github.com/playwright-community/playwright-go"

	"github.com/adam/bookingcom-mcp/internal/scrape"
)

// SearchParams are the inputs for a stay search.
type SearchParams struct {
	Destination string
	Checkin     string // YYYY-MM-DD
	Checkout    string // YYYY-MM-DD
	Adults      int
	Children    int
	Rooms       int
	MinPrice    int // per night, 0 = no filter
	MaxPrice    int // per night, 0 = no filter
	MinRating   int // review score 0-10 (booking filters: 6,7,8,9)
	Stars       int // 1-5, 0 = no filter
	Currency    string
	MaxResults  int
}

// Property is one search result card.
type Property struct {
	Name        string  `json:"name"`
	URL         string  `json:"url"`
	Price       string  `json:"price"`
	Rating      float64 `json:"rating,omitempty"`
	RatingWord  string  `json:"ratingWord,omitempty"`
	ReviewCount string  `json:"reviewCount,omitempty"`
	Address     string  `json:"address,omitempty"`
	Distance    string  `json:"distance,omitempty"`
	Thumbnail   string  `json:"thumbnail,omitempty"`
}

// SearchURL builds the searchresults URL for the given params.
func SearchURL(p SearchParams) string {
	q := url.Values{}
	q.Set("ss", p.Destination)
	q.Set("checkin", p.Checkin)
	q.Set("checkout", p.Checkout)
	q.Set("group_adults", strconv.Itoa(p.Adults))
	q.Set("group_children", strconv.Itoa(p.Children))
	// Booking requires an age per child or it discards the child occupancy;
	// we have no age input, so default each to 10 (a mid-range child fare).
	for range p.Children {
		q.Add("age", "10")
	}
	q.Set("no_rooms", strconv.Itoa(max(p.Rooms, 1)))
	q.Set("lang", "en-us")
	if p.Currency != "" {
		q.Set("selected_currency", p.Currency)
	}
	nflt := ""
	if p.MinRating > 0 {
		nflt += fmt.Sprintf("review_score%%3D%d0", p.MinRating) // e.g. review_score=80
	}
	if p.Stars > 0 {
		if nflt != "" {
			nflt += "%3B"
		}
		nflt += fmt.Sprintf("class%%3D%d", p.Stars)
	}
	if p.MinPrice > 0 || p.MaxPrice > 0 {
		lo, hi := p.MinPrice, p.MaxPrice
		if hi == 0 {
			hi = 999999
		}
		if nflt != "" {
			nflt += "%3B"
		}
		nflt += fmt.Sprintf("price%%3D%s-%d-%d-1", p.Currency, lo, hi)
	}
	u := "https://www.booking.com/searchresults.html?" + q.Encode()
	if nflt != "" {
		u += "&nflt=" + nflt
	}
	return u
}

const extractCardsJS = `
() => Array.from(document.querySelectorAll('[data-testid="property-card"]')).map(c => {
  const q = (sel) => c.querySelector(sel);
  const txt = (sel) => q(sel)?.textContent?.trim() ?? '';
  const score = txt('[data-testid="review-score"]');
  // score text looks like "Scored 8.4 8.4 Very Good 1,234 reviews" — or with a
  // whole-number score, "Scored 10 10 Exceptional 1,234 reviews", so the decimal
  // part is optional and "review(s)" may be singular.
  const m = score.match(/(\d+(?:[.,]\d+)?)/);
  const rc = score.match(/([\d,.]+)\s+reviews?/i);
  // Word starts with a letter so the regex skips both leading numeric scores
  // ("Scored 8.4 8.4 Very Good ...") instead of capturing the second one.
  const word = score.match(/\d+(?:[.,]\d+)?\s+([A-Za-z][^0-9]*?)\s+[\d,.]+\s+reviews?/i);
  return {
    name: txt('[data-testid="title"]'),
    url: (q('a[data-testid="title-link"]')?.href ?? '').split('?')[0],
    price: txt('[data-testid="price-and-discounted-price"]'),
    rating: m ? parseFloat(m[1].replace(',', '.')) : 0,
    ratingWord: word ? word[1] : '',
    reviewCount: rc ? rc[1] : '',
    address: txt('[data-testid="address"]'),
    distance: txt('[data-testid="distance"]'),
    thumbnail: q('img')?.src ?? '',
  };
}).filter(p => p.name)`

// Search runs a stay search and returns property cards.
func Search(ctx context.Context, page playwright.Page, p SearchParams) ([]Property, error) {
	if err := scrape.Goto(ctx, page, SearchURL(p)); err != nil {
		return nil, err
	}
	scrape.DismissCookieBanner(page)
	if err := scrape.WaitForOrBlocked(page, `[data-testid="property-card"]`, 30000,
		"no property cards appeared (site layout may have changed)"); err != nil {
		return nil, err
	}
	var props []Property
	if err := scrape.EvalJSON(page, extractCardsJS, &props); err != nil {
		return nil, err
	}
	if len(props) == 0 {
		// Cards matched the wait selector but none parsed: the title/card selectors
		// drifted. Surface it instead of reporting "no hotels" for a live search.
		return nil, fmt.Errorf("property cards rendered but none parsed (layout may have changed): %s", SearchURL(p))
	}
	return scrape.Cap(props, p.MaxResults), nil
}
