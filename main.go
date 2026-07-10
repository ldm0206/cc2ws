package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// version is injected at build time via ldflags: -ldflags "-X main.version=v0.1.0".
var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "cc2ws:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("cc2ws", flag.ContinueOnError)
	showVersion := fs.Bool("version", false, "print version and exit")
	listen := fs.String("listen", envOr("LISTEN", "127.0.0.1:18080"), "HTTP listen address")
	upstream := fs.String("upstream-base", envOr("UPSTREAM_BASE", ""), "upstream origin, e.g. https://host")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *showVersion {
		fmt.Println("cc2ws", version)
		return nil
	}

	// Flags override env; LoadConfig reads env, so mirror flags into env.
	os.Setenv("LISTEN", *listen)
	os.Setenv("UPSTREAM_BASE", *upstream)

	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	log.Printf("cc2ws %s listening on %s, upstream %s (ws=%s)",
		version, cfg.Listen, cfg.UpstreamBase, cfg.UpstreamWS)

	srv := &http.Server{
		Addr:         cfg.Listen,
		Handler:      withRequestLog(newRouter(cfg)),
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 0, // streaming responses must not time out on write
		IdleTimeout:  120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	select {
	case err := <-errCh:
		return err
	case <-sigCh:
		log.Printf("cc2ws shutting down")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(ctx)
	}
}
