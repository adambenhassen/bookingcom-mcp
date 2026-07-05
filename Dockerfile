FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /bookingcom-mcp ./cmd/bookingcom-mcp

FROM debian:bookworm-slim
# Firefox runtime deps for the Firefox that camoufox drives, plus Python to host
# the bundled camoufox browser. camoufox runs Firefox headful for stealth
# (headless is more bot-detectable), so it needs an X server: xvfb provides a
# virtual display (the server wraps camoufox in xvfb-run when DISPLAY is unset).
# tini is PID 1 to reap zombies from the camoufox/Firefox process tree that get
# reparented as the server relaunches the browser across idle cycles.
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates python3 python3-venv xvfb xauth tini \
    libasound2 libdbus-1-3 libfontconfig1 libfreetype6 libgtk-3-0 \
    libx11-6 libx11-xcb1 libxcb-shm0 libxcb1 libxcomposite1 libxcursor1 \
    libxdamage1 libxext6 libxfixes3 libxi6 libxrandr2 libxrender1 \
    && rm -rf /var/lib/apt/lists/*
# Bake in camoufox (the default stealth Firefox) so the image works out of the
# box. The playwright client is pinned to match playwright-go v0.5200.x — the
# camoufox server and the Go client must agree on the playwright protocol.
RUN python3 -m venv /opt/camoufox \
    && /opt/camoufox/bin/pip install --no-cache-dir "camoufox[geoip]" "playwright==1.52.0" \
    && /opt/camoufox/bin/camoufox fetch
ENV PATH="/opt/camoufox/bin:${PATH}"
COPY --from=build /bookingcom-mcp /usr/local/bin/bookingcom-mcp
# ponytail: the playwright-go driver is still fetched on first run; bake it in if cold starts matter
# We only drive camoufox's own Firefox over a websocket, so playwright must never
# download its bundled browsers (the driver alone is fetched on first run).
ENV PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1
EXPOSE 8747
# tini as PID 1 reaps orphaned browser subprocesses and forwards signals; the
# camoufox backend wraps its headful Firefox in xvfb-run itself when no DISPLAY
# is set (see camoufoxServerCommand).
ENTRYPOINT ["/usr/bin/tini", "--", "bookingcom-mcp"]
CMD ["-transport", "http", "-addr", ":8747"]
