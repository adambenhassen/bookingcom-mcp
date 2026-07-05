// Command bookingcom-mcp serves Booking.com search tools over MCP (stdio or HTTP).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/adam/bookingcom-mcp/internal/browser"
	"github.com/adam/bookingcom-mcp/internal/config"
	"github.com/adam/bookingcom-mcp/internal/tools"
)

const version = "0.1.6"

func main() {
	transport := flag.String("transport", "stdio", "transport: stdio or http")
	addr := flag.String("addr", ":8747", "listen address for -transport http")
	flag.Parse()

	if err := run(*transport, *addr); err != nil {
		fmt.Fprintf(os.Stderr, "bookingcom-mcp: %v\n", err)
		os.Exit(1)
	}
}

func run(transport, addr string) error {
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}

	mgr, err := browser.New(cfg)
	if err != nil {
		return err
	}
	defer mgr.Close()

	server := mcp.NewServer(&mcp.Implementation{Name: "bookingcom-mcp", Version: version}, nil)
	tools.Register(server, &tools.Deps{Browser: mgr, Cfg: cfg})

	// Stop on SIGINT/SIGTERM so the deferred mgr.Close() runs and the camoufox
	// process group is reaped instead of orphaned.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch transport {
	case "stdio":
		return server.Run(ctx, &mcp.StdioTransport{})
	case "http":
		handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
		log.Printf("bookingcom-mcp listening on %s", addr)
		srv := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second}
		go func() {
			<-ctx.Done()
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := srv.Shutdown(shutdownCtx); err != nil {
				log.Printf("http shutdown: %v", err)
			}
		}()
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	default:
		return fmt.Errorf("unknown transport %q (want stdio or http)", transport)
	}
}
