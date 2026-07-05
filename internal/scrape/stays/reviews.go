package stays

import (
	"context"

	"github.com/playwright-community/playwright-go"

	"github.com/adam/bookingcom-mcp/internal/scrape"
)

// Review is one guest review.
type Review struct {
	Title       string `json:"title,omitempty"`
	Score       string `json:"score,omitempty"`
	Positive    string `json:"positive,omitempty"`
	Negative    string `json:"negative,omitempty"`
	Date        string `json:"date,omitempty"`
	Reviewer    string `json:"reviewer,omitempty"`
	Nationality string `json:"nationality,omitempty"`
	StayedRoom  string `json:"stayedRoom,omitempty"`
}

const extractReviewsJS = `
() => Array.from(document.querySelectorAll('[data-testid="review-card"], .review_list_new_item_block')).map(c => {
  const txt = (sel) => c.querySelector(sel)?.textContent?.trim() ?? '';
  return {
    title: txt('[data-testid="review-title"]') || txt('.c-review-block__title'),
    score: txt('[data-testid="review-score"] .a3b8729ab1') || txt('[data-testid="review-score"]') || txt('.bui-review-score__badge'),
    positive: txt('[data-testid="review-positive-text"]') || txt('.c-review__row .c-review__body'),
    negative: txt('[data-testid="review-negative-text"]'),
    date: txt('[data-testid="review-date"]') || txt('.c-review-block__date'),
    reviewer: txt('[data-testid="review-avatar"] .a3332d346a') || txt('.bui-avatar-block__title'),
    nationality: txt('[data-testid="review-avatar"] .afac1f68d9') || txt('.bui-avatar-block__subtitle'),
    stayedRoom: txt('[data-testid="review-room-name"]') || txt('.c-review-block__room-link .bui-list__body'),
  };
}).filter(r => r.title || r.positive || r.negative)`

const reviewCardSel = `[data-testid="review-card"], .review_list_new_item_block`

// GetReviews opens the property's review list and scrapes up to maxPages pages.
func GetReviews(ctx context.Context, page playwright.Page, hotelURL string, maxPages int) ([]Review, error) {
	u, err := normalizeHotelURL(hotelURL, "", "", "", 0)
	if err != nil {
		return nil, err
	}
	u += "#tab-reviews"
	if err := scrape.Goto(ctx, page, u); err != nil {
		return nil, err
	}
	scrape.DismissCookieBanner(page)

	// Open the reviews panel; the read-all-reviews entry point has moved around,
	// so try a few known triggers, then fall back to whatever rendered inline.
	for _, sel := range []string{
		`[data-testid="fr-read-all-reviews"]`,
		`[data-testid="Property-Header-Nav-Tab-Trigger-reviews"]`,
		`#show_reviews_tab`,
	} {
		loc := page.Locator(sel).First()
		if n, cerr := loc.Count(); cerr == nil && n > 0 {
			if cerr := loc.Click(playwright.LocatorClickOptions{Timeout: playwright.Float(5000)}); cerr == nil {
				break
			}
		}
	}
	if err := scrape.WaitForOrBlocked(page, reviewCardSel, 20000,
		"no reviews rendered (property may have none, or layout changed)"); err != nil {
		return nil, err
	}

	if maxPages < 1 {
		maxPages = 1
	}

	seen := map[string]bool{}
	var all []Review
	for i := range maxPages {
		var batch []Review
		if err := scrape.EvalJSON(page, extractReviewsJS, &batch); err != nil {
			return nil, err
		}
		for _, r := range batch {
			// Dedup in case the widget appends rather than replaces cards. Include
			// the body: booking review titles are often empty and the reviewer is
			// just a first name, so reviewer|date|title alone collides across
			// distinct reviews.
			key := r.Reviewer + "|" + r.Date + "|" + r.Title + "|" + r.Positive + "|" + r.Negative
			if seen[key] {
				continue
			}
			seen[key] = true
			all = append(all, r)
		}
		if i == maxPages-1 {
			break
		}
		next := page.Locator(`[data-testid="reviews-pagination"] [aria-label="Next page"], .bui-pagination__next-arrow a`).First()
		n, cerr := next.Count()
		if cerr != nil || n == 0 {
			break // genuinely no further pages
		}
		if err := next.Click(playwright.LocatorClickOptions{Timeout: playwright.Float(5000)}); err != nil {
			break
		}
		// Wait for review cards on the next page rather than networkidle, which
		// booking.com's background beacons keep from ever settling. Dedup above
		// guards against the case where stale cards are still attached.
		if err := page.Locator(reviewCardSel).First().WaitFor(playwright.LocatorWaitForOptions{
			Timeout: playwright.Float(15000),
		}); err != nil {
			break
		}
	}
	//nolint:nilerr // a pagination step failing just ends collection; the reviews gathered so far are valid
	return all, nil
}
