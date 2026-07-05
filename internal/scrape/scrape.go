// Package scrape holds helpers shared by all Booking.com scrapers.
package scrape

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/playwright-community/playwright-go"
)

// ErrBlocked signals that Booking.com served an anti-bot / captcha page.
var ErrBlocked = errors.New(
	"booking.com blocked the request (captcha/anti-bot page); " +
		"retry, or run from a different network/region")

// Goto navigates and fails fast on block pages. It honours ctx cancellation
// while the (context-less) playwright navigation is in flight.
func Goto(ctx context.Context, page playwright.Page, url string) error {
	// playwright-go's Goto takes no context, so run it off-goroutine and honour
	// ctx cancellation: on cancel the caller's deferred page/context cleanup
	// aborts the in-flight navigation, freeing the serialized page slot instead
	// of holding it for the full navigation timeout.
	errc := make(chan error, 1)
	go func() {
		_, err := page.Goto(url, playwright.PageGotoOptions{
			WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		})
		errc <- err
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errc:
		if err != nil {
			return fmt.Errorf("navigate to %s: %w", url, err)
		}
	}
	return CheckBlocked(page)
}

// CheckBlocked returns ErrBlocked if the current page is an anti-bot interstitial.
func CheckBlocked(page playwright.Page) error {
	title, err := page.Title()
	if err != nil {
		return fmt.Errorf("read page title: %w", err)
	}
	t := strings.ToLower(title)
	for _, marker := range []string{"just a moment", "access denied", "are you a robot", "attention required"} {
		if strings.Contains(t, marker) {
			return ErrBlocked
		}
	}
	for _, sel := range []string{"#px-captcha", "iframe[src*='captcha']", "#challenge-running"} {
		n, err := page.Locator(sel).Count()
		if err != nil {
			return fmt.Errorf("probe block-page selector %q: %w", sel, err)
		}
		if n > 0 {
			return ErrBlocked
		}
	}
	return nil
}

// WaitForOrBlocked waits for selector to appear; on timeout it reports ErrBlocked
// if the page is an anti-bot interstitial, otherwise wraps notFoundMsg. This is
// the shared "the results should be here by now" gate used by every scraper.
func WaitForOrBlocked(page playwright.Page, selector string, timeoutMs float64, notFoundMsg string) error {
	if err := page.Locator(selector).First().WaitFor(playwright.LocatorWaitForOptions{
		Timeout: new(timeoutMs),
	}); err != nil {
		if berr := CheckBlocked(page); berr != nil {
			return berr
		}
		return fmt.Errorf("%s: %w", notFoundMsg, err)
	}
	return nil
}

// Cap truncates s to at most n elements (n <= 0 means no limit).
func Cap[T any](s []T, n int) []T {
	if n > 0 && len(s) > n {
		return s[:n]
	}
	return s
}

// EvalJSON runs a JS expression in the page and unmarshals its result into out.
func EvalJSON(page playwright.Page, js string, out any) error {
	v, err := page.Evaluate(js)
	if err != nil {
		return fmt.Errorf("evaluate extraction script: %w", err)
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal extraction result: %w", err)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode extraction result: %w", err)
	}
	return nil
}

// DismissCookieBanner clicks the OneTrust accept button if present. Best effort,
// but a click failure is logged so a banner left overlaying the page (which can
// swallow later interactions) is at least diagnosable.
func DismissCookieBanner(page playwright.Page) {
	loc := page.Locator("#onetrust-accept-btn-handler")
	n, err := loc.Count()
	if err != nil || n == 0 {
		return
	}
	if err := loc.Click(playwright.LocatorClickOptions{Timeout: playwright.Float(3000)}); err != nil {
		fmt.Fprintf(os.Stderr, "warning: dismissing cookie banner: %v\n", err)
	}
}
