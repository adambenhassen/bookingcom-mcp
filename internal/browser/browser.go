// Package browser manages the shared Playwright browser behind a configurable backend.
package browser

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/playwright-community/playwright-go"

	"github.com/adam/bookingcom-mcp/internal/config"
)

const camoufoxNoEndpoint = "camoufox server exited without printing a ws:// endpoint"

// Manager owns the camoufox browser and hands out pages serially. The browser
// is torn down after cfg.IdleTimeout with no requests to free memory, and
// relaunched lazily on the next request.
type Manager struct {
	cfg       config.Config
	pw        *playwright.Playwright
	sem       chan struct{} // capacity-1 semaphore; serializes scrapes and gates shutdown
	closeCh   chan struct{} // closed by Close to reject new work and abort a cold reconnect
	closeOnce sync.Once

	mu        sync.Mutex // guards the fields below
	browser   playwright.Browser
	cmd       *exec.Cmd // camoufox server subprocess; non-nil only while a browser is connected
	idleTimer *time.Timer
	idleGen   uint64 // bumped on every timer stop/rearm; a fire whose captured gen no longer matches bails
	closed    bool
}

// New starts the Playwright driver and connects to camoufox, failing fast if
// the browser can't be launched.
func New(cfg config.Config) (*Manager, error) {
	m := &Manager{cfg: cfg, sem: make(chan struct{}, 1), closeCh: make(chan struct{})}

	pw, err := playwright.Run()
	if err != nil {
		// Driver not installed yet: install the driver only (never browsers —
		// camoufox ships its own Firefox) and retry. Stdout must stay clean: it
		// carries the MCP stdio transport.
		if ierr := playwright.Install(&playwright.RunOptions{SkipInstallBrowsers: true, Stdout: os.Stderr}); ierr != nil {
			return nil, fmt.Errorf("install playwright driver: %w", ierr)
		}
		if pw, err = playwright.Run(); err != nil {
			return nil, fmt.Errorf("start playwright driver: %w", err)
		}
	}
	m.pw = pw

	if err := m.connectCamoufox(); err != nil {
		m.Close()
		return nil, err
	}
	m.mu.Lock()
	m.armIdleLocked()
	m.mu.Unlock()
	return m, nil
}

type camoufoxOutput struct {
	mu sync.Mutex
	b  strings.Builder
}

func (o *camoufoxOutput) addLine(line string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.b.Len() > 0 {
		o.b.WriteByte('\n')
	}
	o.b.WriteString(line)
}

func (o *camoufoxOutput) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.b.String()
}

func (o *camoufoxOutput) err(message string) error {
	output := strings.TrimSpace(o.String())
	if output == "" {
		return errors.New(message)
	}
	return fmt.Errorf("%s; camoufox output:\n%s", message, output)
}

func scanCamoufoxOutput(r io.Reader, captured *camoufoxOutput, onEndpoint func(string)) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	sent := false
	for sc.Scan() {
		line := sc.Text()
		if !sent {
			captured.addLine(line)
		}
		if sent {
			continue // keep draining the pipe so camoufox never blocks on write
		}
		if i := strings.Index(line, "ws://"); i >= 0 {
			// Output may wrap the URL in ANSI color codes; cut at the first
			// character that can't be part of the endpoint URL.
			ws := line[i:]
			if j := strings.IndexFunc(ws, func(r rune) bool {
				return r <= ' ' || r == 0x1b
			}); j >= 0 {
				ws = ws[:j]
			}
			onEndpoint(ws)
			sent = true
		}
	}
	if sent {
		return nil
	}
	if serr := sc.Err(); serr != nil {
		output := strings.TrimSpace(captured.String())
		if output == "" {
			return fmt.Errorf("reading camoufox output: %w", serr)
		}
		return fmt.Errorf("reading camoufox output: %w; camoufox output:\n%s", serr, output)
	}
	return captured.err(camoufoxNoEndpoint)
}

// camoufoxServerCommand builds the argv for launching `camoufox server`.
// camoufox runs Firefox headful (headless is more bot-detectable), which needs
// an X display. On Linux with no DISPLAY set — e.g. a container or headless
// server — wrap the launch in xvfb-run so a virtual display is provided. If
// xvfb-run is unavailable we launch bare and let camoufox surface the
// "no DISPLAY" error rather than failing opaquely.
func camoufoxServerCommand(bin, display, goos string, lookPath func(string) (string, error)) (string, []string) {
	if goos == "linux" && display == "" {
		if xvfb, err := lookPath("xvfb-run"); err == nil {
			return xvfb, []string{"-a", bin, "server"}
		}
	}
	return bin, []string{"server"}
}

// connectCamoufox spawns `camoufox server` and connects to its websocket endpoint.
func (m *Manager) connectCamoufox() error {
	bin, err := exec.LookPath("camoufox")
	if err != nil {
		return errors.New("camoufox not found in PATH; install with " +
			"`pip install camoufox[geoip]` and run `camoufox fetch`")
	}
	// `camoufox server` accepts no flags (headless is a library-only option).
	name, args := camoufoxServerCommand(bin, os.Getenv("DISPLAY"), runtime.GOOS, exec.LookPath)
	cmd := exec.Command(name, args...) //nolint:gosec,noctx // name/args derive from LookPath + literal "server", no user input; server outlives any request ctx
	// Own process group so Close can kill camoufox AND the browser it spawns.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("camoufox stdout pipe: %w", err)
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start camoufox server: %w", err)
	}
	m.cmd = cmd

	wsCh := make(chan string, 1)
	errCh := make(chan error, 1)
	captured := &camoufoxOutput{}
	go func() {
		errCh <- scanCamoufoxOutput(stdout, captured, func(ws string) {
			wsCh <- ws
		})
	}()

	// Every failure arm must reap the process group started above: otherwise the
	// camoufox subprocess (and its Firefox) leaks, and the next reconnect would
	// overwrite m.cmd and lose the handle to it.
	select {
	case ws := <-wsCh:
		b, err := m.pw.Firefox.Connect(ws)
		if err != nil {
			m.reapCmdLocked()
			return fmt.Errorf("connect to camoufox at %s: %w", ws, err)
		}
		m.browser = b
		return nil
	case err := <-errCh:
		m.reapCmdLocked()
		return err
	case <-m.closeCh:
		m.reapCmdLocked()
		return errors.New("camoufox connect aborted: manager closing")
	case <-time.After(90 * time.Second):
		m.reapCmdLocked()
		return captured.err("timed out waiting for camoufox server websocket endpoint")
	}
}

// ensureBrowser returns a live browser, relaunching camoufox if an idle
// teardown closed it. It also cancels any pending idle teardown, since a
// request is now starting. Callers must hold the sem so connects are serialized.
func (m *Manager) ensureBrowser() (playwright.Browser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopIdleLocked() // a request is starting; a pending idle teardown must not fire
	if m.browser != nil && m.browser.IsConnected() {
		return m.browser, nil
	}
	// Browser is nil (fresh or idle-torn-down) or died out-of-band (crash, OOM,
	// dropped websocket). Clear any stale browser/process, then relaunch — else a
	// dead-but-non-nil browser would fail every request with no recovery.
	m.teardownBrowserLocked()
	if err := m.connectCamoufox(); err != nil {
		return nil, err
	}
	return m.browser, nil
}

// stopIdleLocked cancels the idle timer and invalidates any callback that has
// already fired but not yet run. m.mu must be held.
func (m *Manager) stopIdleLocked() {
	if m.idleTimer != nil {
		m.idleTimer.Stop()
		m.idleTimer = nil
	}
	m.idleGen++
}

// armIdleLocked (re)starts the idle timer. Called after every request and once
// at startup, so the window resets on activity. m.mu must be held.
func (m *Manager) armIdleLocked() {
	if m.closed || m.browser == nil || m.cfg.IdleTimeout <= 0 {
		return
	}
	m.stopIdleLocked()
	gen := m.idleGen
	m.idleTimer = time.AfterFunc(m.cfg.IdleTimeout, func() { m.onIdle(gen) })
}

func (m *Manager) armIdle() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.armIdleLocked()
}

// onIdle tears the browser down after an idle period to free memory. It takes
// the sem first so it never races an in-flight scrape, then verifies gen to
// ignore a fire that newer activity has already superseded.
func (m *Manager) onIdle(gen uint64) {
	select {
	case m.sem <- struct{}{}:
	case <-m.closeCh:
		return // shutting down; Close does the teardown
	}
	defer func() { <-m.sem }()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.browser == nil || gen != m.idleGen {
		return
	}
	m.teardownBrowserLocked()
	fmt.Fprintf(os.Stderr, "camoufox idle for %s; browser shut down to free memory\n", m.cfg.IdleTimeout)
}

// teardownBrowserLocked closes the browser and kills the camoufox process
// group, leaving the Playwright driver running for a cheap relaunch. m.mu must
// be held; the caller must also hold the sem so no scrape is in flight.
func (m *Manager) teardownBrowserLocked() {
	if m.browser != nil {
		if err := m.browser.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: closing browser: %v\n", err)
		}
		m.browser = nil
	}
	m.reapCmdLocked()
}

// reapCmdLocked kills the camoufox process group and waits for it, clearing
// m.cmd. Safe when m.cmd is nil or already exited. m.mu must be held (or called
// single-threaded during New).
func (m *Manager) reapCmdLocked() {
	if m.cmd == nil || m.cmd.Process == nil {
		m.cmd = nil
		return
	}
	// Negative PID targets the whole process group set up via Setpgid, killing
	// camoufox and the browser it forked. ESRCH means it already exited.
	if err := syscall.Kill(-m.cmd.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		fmt.Fprintf(os.Stderr, "warning: killing camoufox process group: %v\n", err)
	}
	if werr := m.cmd.Wait(); werr != nil {
		// A killed process returns *exec.ExitError ("signal: killed"), which is
		// expected here; anything else is worth a warning.
		var ee *exec.ExitError
		if !errors.As(werr, &ee) {
			fmt.Fprintf(os.Stderr, "warning: waiting on camoufox: %v\n", werr)
		}
	}
	m.cmd = nil
}

// Page returns a fresh page in a new context plus a cleanup func that must be
// called exactly once (a sync.Once guards against double-release). Acquisition
// is serialized and honours ctx cancellation while waiting for the turn.
func (m *Manager) Page(ctx context.Context) (playwright.Page, func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case m.sem <- struct{}{}:
	case <-m.closeCh:
		return nil, nil, errors.New("browser manager is closed")
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
	release := func() { <-m.sem }

	browser, err := m.ensureBrowser()
	if err != nil {
		release()
		return nil, nil, err
	}
	bctx, err := browser.NewContext(playwright.BrowserNewContextOptions{
		Locale: new("en-US"),
	})
	if err != nil {
		m.armIdle()
		release()
		return nil, nil, fmt.Errorf("new browser context: %w", err)
	}
	page, err := bctx.NewPage()
	if err != nil {
		cerr := bctx.Close()
		m.armIdle()
		release()
		if cerr != nil {
			return nil, nil, fmt.Errorf("new page: %w (context close: %w)", err, cerr)
		}
		return nil, nil, fmt.Errorf("new page: %w", err)
	}
	page.SetDefaultTimeout(60_000)
	var once sync.Once
	cleanup := func() {
		once.Do(func() {
			if err := bctx.Close(); err != nil {
				fmt.Fprintf(os.Stderr, "warning: closing browser context: %v\n", err)
			}
			m.armIdle() // reset the idle window now that this request is done
			release()
		})
	}
	return page, cleanup, nil
}

// Close tears down the browser, driver and any camoufox subprocess. It first
// drains any in-flight Page holder (via the semaphore) so teardown never races
// a live scrape, and is safe to call more than once.
func (m *Manager) Close() {
	m.closeOnce.Do(func() {
		close(m.closeCh)    // reject new Page calls and abort any cold reconnect
		m.sem <- struct{}{} // wait for the current holder (if any) to finish
		m.mu.Lock()
		defer m.mu.Unlock()
		m.closed = true
		m.stopIdleLocked() // no more idle teardowns after this
		m.teardownBrowserLocked()
		if m.pw != nil {
			if err := m.pw.Stop(); err != nil {
				fmt.Fprintf(os.Stderr, "warning: stopping playwright: %v\n", err)
			}
		}
	})
}
