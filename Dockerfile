FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /bookingcom-mcp ./cmd/bookingcom-mcp
# Bake the playwright-go driver (node + playwright 1.60.0) into the image so the
# runtime never fetches it on first use. v0.6000.0's default download host serves
# the 1.60.0 driver; do not override PLAYWRIGHT_DOWNLOAD_HOST (cdn.playwright.dev
# returns 400 for this build).
RUN PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1 \
    go run github.com/playwright-community/playwright-go/cmd/playwright --version

FROM debian:bookworm-slim
# Firefox runtime deps for the Firefox that camoufox drives, plus Python to host
# the bundled camoufox browser. camoufox runs Firefox headful for stealth
# (headless is more bot-detectable), so it needs an X server: xvfb provides a
# virtual display (the server wraps camoufox in xvfb-run when DISPLAY is unset).
# tini is PID 1 to reap zombies from the camoufox/Firefox process tree that get
# reparented as the server relaunches the browser across idle cycles.
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates git python3 python3-venv xvfb xauth tini \
    libasound2 libdbus-1-3 libfontconfig1 libfreetype6 libgtk-3-0 \
    libx11-6 libx11-xcb1 libxcb-shm0 libxcb1 libxcomposite1 libxcursor1 \
    libxdamage1 libxext6 libxfixes3 libxi6 libxrandr2 libxrender1 \
    && rm -rf /var/lib/apt/lists/*
# Bake in camoufox (the default stealth Firefox) so the image works out of the
# box. The Python playwright pin must match playwright-go exactly: the
# camoufox server and the Go client must agree on the playwright protocol.
# camoufox is pinned to a commit for reproducible builds: this is the Firefox 150
# launcher that matches playwright 1.60. The published camoufox 0.4.11 ships an
# older Firefox and cannot host playwright 1.60.
RUN python3 -m venv /opt/camoufox \
    && /opt/camoufox/bin/pip install --no-cache-dir \
        "playwright==1.60.0" \
        "camoufox[geoip] @ git+https://github.com/daijro/camoufox.git@f342c20dd23736b210f4d5fa4d8b073ee877c9d6#subdirectory=pythonlib"
RUN /opt/camoufox/bin/python <<'PY'
from pathlib import Path
import subprocess

from playwright._impl._driver import compute_driver_executable

nodejs, cli = compute_driver_executable()
if isinstance(nodejs, tuple):
    nodejs = nodejs[0]
driver_package = Path(cli).parent
shim = driver_package / "lib" / "browserServerImpl.js"
shim.write_text("""'use strict';

const playwright = require('..');
const RealBrowserServerLauncherImpl = playwright.firefox._serverLauncher.constructor;

class BrowserServerLauncherImpl extends RealBrowserServerLauncherImpl {
    async launchServer(options = {}) {
        // Camoufox emits top-level nulls such as proxy: null. Playwright 1.60's
        // launch validators reject those, while older drivers tolerated them.
        // Removing null-valued top-level options preserves the old behavior.
        for (const [key, value] of Object.entries(options)) {
            if (value === null)
                delete options[key];
        }
        return await super.launchServer(options);
    }
}

module.exports = { BrowserServerLauncherImpl };
""")
subprocess.run(
    [
        nodejs,
        "-e",
        "const { BrowserServerLauncherImpl } = require('./lib/browserServerImpl.js');"
        "const launcher = new BrowserServerLauncherImpl('firefox');"
        "if (typeof launcher.launchServer !== 'function') process.exit(1);",
    ],
    cwd=driver_package,
    check=True,
)
PY
RUN /opt/camoufox/bin/camoufox fetch
ENV PATH="/opt/camoufox/bin:${PATH}"
COPY --from=build /root/.cache/ms-playwright-go /root/.cache/ms-playwright-go
COPY --from=build /bookingcom-mcp /usr/local/bin/bookingcom-mcp
# We only drive camoufox's own Firefox over a websocket, so playwright must never
# download its bundled browsers (the driver alone is fetched on first run).
ENV PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1
EXPOSE 8747
# tini as PID 1 reaps orphaned browser subprocesses and forwards signals; the
# camoufox backend wraps its headful Firefox in xvfb-run itself when no DISPLAY
# is set (see camoufoxServerCommand).
ENTRYPOINT ["/usr/bin/tini", "--", "bookingcom-mcp"]
CMD ["-transport", "http", "-addr", ":8747"]
