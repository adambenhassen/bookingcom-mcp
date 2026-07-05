// Package tools registers the MCP tools and maps them to scrapers.
package tools

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/adam/bookingcom-mcp/internal/browser"
	"github.com/adam/bookingcom-mcp/internal/config"
	"github.com/adam/bookingcom-mcp/internal/scrape/cars"
	"github.com/adam/bookingcom-mcp/internal/scrape/flights"
	"github.com/adam/bookingcom-mcp/internal/scrape/stays"
)

// Deps are the shared dependencies for all tool handlers.
type Deps struct {
	Browser *browser.Manager
	Cfg     config.Config
}

// SearchHotelsIn is the input for search_hotels.
type SearchHotelsIn struct {
	Destination string `json:"destination"           jsonschema:"city, region or landmark to search, e.g. 'Amsterdam'"`
	Checkin     string `json:"checkin"               jsonschema:"check-in date YYYY-MM-DD"`
	Checkout    string `json:"checkout"              jsonschema:"check-out date YYYY-MM-DD"`
	Adults      int    `json:"adults,omitempty"      jsonschema:"number of adults, default 2"`
	Children    int    `json:"children,omitempty"    jsonschema:"number of children, default 0"`
	Rooms       int    `json:"rooms,omitempty"       jsonschema:"number of rooms, default 1"`
	MinPrice    int    `json:"min_price,omitempty"   jsonschema:"minimum price per night in the selected currency"`
	MaxPrice    int    `json:"max_price,omitempty"   jsonschema:"maximum price per night in the selected currency"`
	MinRating   int    `json:"min_rating,omitempty"  jsonschema:"minimum review score 6-9"`
	Stars       int    `json:"stars,omitempty"       jsonschema:"star class filter 1-5"`
	MaxResults  int    `json:"max_results,omitempty" jsonschema:"cap on returned results, default 25"`
}

// HotelsOut wraps hotel search results.
type HotelsOut struct {
	Properties []stays.Property `json:"properties"`
}

// HotelDetailsIn is the input for get_hotel_details.
type HotelDetailsIn struct {
	HotelURL string `json:"hotel_url"          jsonschema:"booking.com property URL from search_hotels results"`
	Checkin  string `json:"checkin,omitempty"  jsonschema:"optional check-in date YYYY-MM-DD"`
	Checkout string `json:"checkout,omitempty" jsonschema:"optional check-out date YYYY-MM-DD"`
}

// ReviewsIn is the input for get_hotel_reviews.
type ReviewsIn struct {
	HotelURL string `json:"hotel_url"           jsonschema:"booking.com property URL"`
	MaxPages int    `json:"max_pages,omitempty" jsonschema:"review pages to fetch, default 1"`
}

// ReviewsOut wraps reviews.
type ReviewsOut struct {
	Reviews []stays.Review `json:"reviews"`
}

// AvailabilityIn is the input for check_availability.
type AvailabilityIn struct {
	HotelURL string `json:"hotel_url"        jsonschema:"booking.com property URL"`
	Checkin  string `json:"checkin"          jsonschema:"check-in date YYYY-MM-DD"`
	Checkout string `json:"checkout"         jsonschema:"check-out date YYYY-MM-DD"`
	Adults   int    `json:"adults,omitempty" jsonschema:"number of adults, default 2"`
}

// AvailabilityOut wraps room offers.
type AvailabilityOut struct {
	Rooms []stays.Room `json:"rooms"`
}

// SearchFlightsIn is the input for search_flights.
type SearchFlightsIn struct {
	From       string `json:"from"                  jsonschema:"origin IATA airport code, e.g. AMS"`
	To         string `json:"to"                    jsonschema:"destination IATA airport code, e.g. JFK"`
	Depart     string `json:"depart"                jsonschema:"departure date YYYY-MM-DD"`
	Return     string `json:"return,omitempty"      jsonschema:"return date YYYY-MM-DD; omit for one-way"`
	Adults     int    `json:"adults,omitempty"      jsonschema:"number of adults, default 1"`
	CabinClass string `json:"cabin_class,omitempty" jsonschema:"ECONOMY, PREMIUM_ECONOMY, BUSINESS or FIRST; default ECONOMY"`
	MaxResults int    `json:"max_results,omitempty" jsonschema:"cap on returned results, default 15"`
}

// FlightsOut wraps flight results.
type FlightsOut struct {
	Flights []flights.Flight `json:"flights"`
}

// SearchCarsIn is the input for search_car_rentals.
type SearchCarsIn struct {
	PickupLocation  string `json:"pickup_location"            jsonschema:"city or airport for pick-up, e.g. 'Amsterdam Airport Schiphol'"`
	DropoffLocation string `json:"dropoff_location,omitempty" jsonschema:"drop-off location if different from pick-up"`
	PickupDate      string `json:"pickup_date"                jsonschema:"pick-up date YYYY-MM-DD"`
	DropoffDate     string `json:"dropoff_date"               jsonschema:"drop-off date YYYY-MM-DD"`
	MaxResults      int    `json:"max_results,omitempty"      jsonschema:"cap on returned results, default 15"`
}

// CarsOut wraps car rental results.
type CarsOut struct {
	Cars []cars.Car `json:"cars"`
}

// Register adds all Booking.com tools to the server.
func Register(server *mcp.Server, d *Deps) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_hotels",
		Description: "Search Booking.com stays by destination, dates, guests and filters.",
	}, d.searchHotels)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_hotel_details",
		Description: "Get full details (description, facilities, location, photos) for a Booking.com property URL.",
	}, d.hotelDetails)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_hotel_reviews",
		Description: "Fetch guest reviews for a Booking.com property URL.",
	}, d.hotelReviews)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "check_availability",
		Description: "Get room-level prices and availability for a Booking.com property and dates.",
	}, d.availability)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_flights",
		Description: "Search flights on flights.booking.com between two airports.",
	}, d.searchFlights)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_car_rentals",
		Description: "Search car rentals on cars.booking.com.",
	}, d.searchCars)
}

func parseDate(label, s string) (time.Time, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid %s %q (want YYYY-MM-DD)", label, s)
	}
	return t, nil
}

// dateRange validates a start/end date pair using tool-specific labels.
// allowEqual permits end == start (e.g. a same-day return flight).
func dateRange(startLabel, start, endLabel, end string, allowEqual bool) error {
	s, err := parseDate(startLabel, start)
	if err != nil {
		return err
	}
	e, err := parseDate(endLabel, end)
	if err != nil {
		return err
	}
	if e.Before(s) || (!allowEqual && e.Equal(s)) {
		rel := "after"
		if allowEqual {
			rel = "on or after"
		}
		return fmt.Errorf("%s %s must be %s %s %s", endLabel, end, rel, startLabel, start)
	}
	return nil
}

// validIATA reports whether code is a 3-letter airport code (charset-checked so
// it can't inject a path separator into the flights URL).
func validIATA(code string) bool {
	if len(code) != 3 {
		return false
	}
	for _, r := range code {
		isLetter := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		if !isLetter {
			return false
		}
	}
	return true
}

// validCabinClass reports whether s is one of booking's flight cabin classes.
func validCabinClass(s string) bool {
	switch s {
	case "ECONOMY", "PREMIUM_ECONOMY", "BUSINESS", "FIRST":
		return true
	}
	return false
}

func (d *Deps) searchHotels(ctx context.Context, _ *mcp.CallToolRequest, in SearchHotelsIn) (*mcp.CallToolResult, HotelsOut, error) {
	var out HotelsOut
	if in.Destination == "" {
		return nil, out, errors.New("destination is required")
	}
	if err := dateRange("checkin", in.Checkin, "checkout", in.Checkout, false); err != nil {
		return nil, out, err
	}
	if in.Adults < 0 || in.Children < 0 || in.Rooms < 0 {
		return nil, out, errors.New("adults, children and rooms must not be negative")
	}
	if in.Adults > 30 || in.Children > 10 || in.Rooms > 30 {
		// Bounded so a large children count can't blow up the per-child age loop
		// in SearchURL into a multi-megabyte URL.
		return nil, out, errors.New("adults (max 30), children (max 10) and rooms (max 30) exceed the allowed maximum")
	}
	if in.MinPrice < 0 || in.MaxPrice < 0 {
		return nil, out, errors.New("min_price and max_price must not be negative")
	}
	if in.MinPrice > 0 && in.MaxPrice > 0 && in.MinPrice > in.MaxPrice {
		return nil, out, errors.New("min_price must not exceed max_price")
	}
	if in.MinRating != 0 && (in.MinRating < 6 || in.MinRating > 9) {
		return nil, out, errors.New("min_rating must be between 6 and 9")
	}
	if in.Stars != 0 && (in.Stars < 1 || in.Stars > 5) {
		return nil, out, errors.New("stars must be between 1 and 5")
	}
	page, done, err := d.Browser.Page(ctx)
	if err != nil {
		return nil, out, err
	}
	defer done()
	props, err := stays.Search(ctx, page, stays.SearchParams{
		Destination: in.Destination,
		Checkin:     in.Checkin,
		Checkout:    in.Checkout,
		Adults:      defaultInt(in.Adults, 2),
		Children:    in.Children,
		Rooms:       defaultInt(in.Rooms, 1),
		MinPrice:    in.MinPrice,
		MaxPrice:    in.MaxPrice,
		MinRating:   in.MinRating,
		Stars:       in.Stars,
		Currency:    d.Cfg.Currency,
		MaxResults:  defaultInt(in.MaxResults, 25),
	})
	if err != nil {
		return nil, out, err
	}
	out.Properties = props
	return nil, out, nil
}

func (d *Deps) hotelDetails(ctx context.Context, _ *mcp.CallToolRequest, in HotelDetailsIn) (*mcp.CallToolResult, *stays.Details, error) {
	if in.HotelURL == "" {
		return nil, nil, errors.New("hotel_url is required")
	}
	if in.Checkin != "" || in.Checkout != "" {
		if in.Checkin == "" || in.Checkout == "" {
			return nil, nil, errors.New("checkin and checkout must be provided together")
		}
		if err := dateRange("checkin", in.Checkin, "checkout", in.Checkout, false); err != nil {
			return nil, nil, err
		}
	}
	page, done, err := d.Browser.Page(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer done()
	det, err := stays.GetDetails(ctx, page, in.HotelURL, in.Checkin, in.Checkout, d.Cfg.Currency)
	return nil, det, err
}

func (d *Deps) hotelReviews(ctx context.Context, _ *mcp.CallToolRequest, in ReviewsIn) (*mcp.CallToolResult, ReviewsOut, error) {
	var out ReviewsOut
	if in.HotelURL == "" {
		return nil, out, errors.New("hotel_url is required")
	}
	page, done, err := d.Browser.Page(ctx)
	if err != nil {
		return nil, out, err
	}
	defer done()
	revs, err := stays.GetReviews(ctx, page, in.HotelURL, defaultInt(in.MaxPages, 1))
	if err != nil {
		return nil, out, err
	}
	out.Reviews = revs
	return nil, out, nil
}

func (d *Deps) availability(ctx context.Context, _ *mcp.CallToolRequest, in AvailabilityIn) (*mcp.CallToolResult, AvailabilityOut, error) {
	var out AvailabilityOut
	if in.HotelURL == "" {
		return nil, out, errors.New("hotel_url is required")
	}
	if err := dateRange("checkin", in.Checkin, "checkout", in.Checkout, false); err != nil {
		return nil, out, err
	}
	page, done, err := d.Browser.Page(ctx)
	if err != nil {
		return nil, out, err
	}
	defer done()
	rooms, err := stays.CheckAvailability(ctx, page, in.HotelURL, in.Checkin, in.Checkout, d.Cfg.Currency, defaultInt(in.Adults, 2))
	if err != nil {
		return nil, out, err
	}
	out.Rooms = rooms
	return nil, out, nil
}

func (d *Deps) searchFlights(ctx context.Context, _ *mcp.CallToolRequest, in SearchFlightsIn) (*mcp.CallToolResult, FlightsOut, error) {
	var out FlightsOut
	if !validIATA(in.From) || !validIATA(in.To) {
		return nil, out, errors.New("from and to must be 3-letter IATA airport codes")
	}
	if in.CabinClass != "" && !validCabinClass(in.CabinClass) {
		return nil, out, errors.New("cabin_class must be ECONOMY, PREMIUM_ECONOMY, BUSINESS or FIRST")
	}
	if _, err := parseDate("depart", in.Depart); err != nil {
		return nil, out, err
	}
	if in.Return != "" {
		// A same-day return is legitimate for flights.
		if err := dateRange("depart", in.Depart, "return", in.Return, true); err != nil {
			return nil, out, err
		}
	}
	page, done, err := d.Browser.Page(ctx)
	if err != nil {
		return nil, out, err
	}
	defer done()
	fl, err := flights.Search(ctx, page, flights.SearchParams{
		From:       in.From,
		To:         in.To,
		Depart:     in.Depart,
		Return:     in.Return,
		Adults:     defaultInt(in.Adults, 1),
		CabinClass: cmp.Or(in.CabinClass, "ECONOMY"),
		Currency:   d.Cfg.Currency,
		MaxResults: defaultInt(in.MaxResults, 15),
	})
	if err != nil {
		return nil, out, err
	}
	out.Flights = fl
	return nil, out, nil
}

func (d *Deps) searchCars(ctx context.Context, _ *mcp.CallToolRequest, in SearchCarsIn) (*mcp.CallToolResult, CarsOut, error) {
	var out CarsOut
	if in.PickupLocation == "" {
		return nil, out, errors.New("pickup_location is required")
	}
	// Same-day drop-off is allowed for rentals.
	if err := dateRange("pickup_date", in.PickupDate, "dropoff_date", in.DropoffDate, true); err != nil {
		return nil, out, err
	}
	page, done, err := d.Browser.Page(ctx)
	if err != nil {
		return nil, out, err
	}
	defer done()
	cs, err := cars.Search(ctx, page, cars.SearchParams{
		PickupLocation:  in.PickupLocation,
		DropoffLocation: in.DropoffLocation,
		PickupDate:      in.PickupDate,
		DropoffDate:     in.DropoffDate,
		MaxResults:      defaultInt(in.MaxResults, 15),
	})
	if err != nil {
		return nil, out, err
	}
	out.Cars = cs
	return nil, out, nil
}

func defaultInt(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}
