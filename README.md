<div align="center">

<img src="assets/banner.svg" alt="bookingcom-mcp banner" width="100%">

# bookingcom-mcp

**MCP server exposing Booking.com search as tools — hotels, flights, and car rentals.**

[![Go Version](https://img.shields.io/badge/go-1.26-00ADD8?logo=go&logoColor=white)](go.mod)
[![MCP](https://img.shields.io/badge/protocol-MCP-8A2BE2)](https://modelcontextprotocol.io)
[![Platform](https://img.shields.io/badge/transport-stdio%20%7C%20http-blue)](#usage)

</div>

Booking.com has no public API, so this server scrapes with a real browser (the [camoufox](https://camoufox.com) stealth browser by default). It is **read-only**: search and lookup only — no booking or checkout.

## Table of Contents

- [Features](#features)
- [Tools](#tools)
- [Installation](#installation)
- [Configuration](#configuration)
- [Usage](#usage)
- [How It Works](#how-it-works)
- [Contributing](#contributing)

## Features

- 🏨 Hotel search with price, rating, and star filters
- ✈️ Flight search between IATA airports
- 🚗 Car rental offers by pick-up location and dates
- 🕵️ Scrapes with camoufox, a stealth Firefox that defeats anti-bot checks
- 🧱 Anti-bot interstitials surface as explicit errors, never as silently empty results

## Tools

| Tool | Description |
|------|-------------|
| `search_hotels` | Search stays by destination, dates, guests, price/rating/star filters |
| `get_hotel_details` | Description, facilities, coordinates, photos for a property URL |
| `get_hotel_reviews` | Guest reviews (paginated) for a property URL |
| `check_availability` | Room-level prices and availability for dates |
| `search_flights` | Flights between two IATA airports (geo-gated by Booking; needs a supported region) |
| `search_car_rentals` | Car rental offers by pick-up location and dates |

## Installation

### Prerequisites

- Go 1.26+
- camoufox (see below)

### Build

```sh
go build -o bookingcom-mcp ./cmd/bookingcom-mcp
```

### Browser (camoufox)

Scraping runs on [camoufox](https://camoufox.com), a stealth Firefox. Install it
once (the Docker image bakes it in):

```sh
uv tool install --force "camoufox[geoip]" --with "playwright==1.52.0" && camoufox fetch
```

The `playwright` pin must match this module's playwright-go v0.5200.x — client and
server versions must agree. camoufox runs Firefox headful, so a headless Linux host
also needs an X server; install `xvfb` and the server auto-wraps camoufox in
`xvfb-run` when no `DISPLAY` is set.

## Configuration

All configuration is via environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `BOOKING_CURRENCY` | `EUR` | Currency for prices |
| `BOOKING_IDLE_TIMEOUT` | `2m` | Shut the browser down after this long idle to free memory; relaunched on the next request. `0` disables. Any Go duration (e.g. `90s`, `5m`). |

## Usage

```sh
bookingcom-mcp                                # stdio (default)
bookingcom-mcp -transport http -addr :8747    # streamable HTTP
```

### Claude Code

```sh
claude mcp add bookingcom -- /path/to/bookingcom-mcp
```

### Claude Desktop

```json
{
  "mcpServers": {
    "bookingcom": {
      "command": "/path/to/bookingcom-mcp"
    }
  }
}
```

## How It Works

- Anti-bot interstitials are detected and returned as errors (never as empty results); the error suggests retrying or running from a different network/region.
- Booking Flights is unavailable in some regions (`/not-available` redirect) — the tool reports this explicitly.
- Scrapers depend on Booking.com page structure; selectors are centralized per scraper package (`internal/scrape/*`) to limit breakage when the site changes.

## Contributing

Issues and pull requests are welcome. Please run `go vet ./...` and `golangci-lint run` before submitting.

## Disclaimer

This project is not affiliated with or endorsed by Booking.com. It scrapes public pages; use responsibly and in accordance with Booking.com's terms of service.
