// cmd/cc2ws/main.go
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"cc2ws/internal/core"
)

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
	connectTimeout := fs.String("connect-timeout", envOr("CONNECT_TIMEOUT", "10s"), "WS dial/handshake timeout (e.g. 10s)")
	idleTimeout := fs.String("idle-timeout", envOr("IDLE_TIMEOUT", "600s"), "per-read idle timeout (e.g. 600s)")
	skipTLSDefault, _ := strconv.ParseBool(envOr("UPSTREAM_INSECURE_SKIP_TLS_VERIFY", "false"))
	insecureSkipTLSVerify := fs.Bool("insecure-skip-tls-verify", skipTLSDefault, "skip upstream TLS verify (debug only)")
	logLevel := fs.String("log-level", envOr("LOG_LEVEL", "info"), "debug/info/warn/error")
	headless := fs.Bool("headless", envOr("CC2WS_HEADLESS", "true") == "true", "run without UI (servers/SSH/CI)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *showVersion {
		fmt.Println("cc2ws", core.Version)
		return nil
	}

	os.Setenv("LISTEN", *listen)
	os.Setenv("UPSTREAM_BASE", *upstream)
	os.Setenv("CONNECT_TIMEOUT", *connectTimeout)
	os.Setenv("IDLE_TIMEOUT", *idleTimeout)
	os.Setenv("UPSTREAM_INSECURE_SKIP_TLS_VERIFY", strconv.FormatBool(*insecureSkipTLSVerify))
	os.Setenv("LOG_LEVEL", *logLevel)

	cfg, err := core.LoadConfig()
	if err != nil {
		return err
	}

	if *headless {
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		return core.Run(ctx, cfg)
	}
	// Non-headless path: no GUI/TUI frontend is compiled into this build yet.
	// Tasks 6-7 wire in the frontend via cc2ws/app.runFrontend.
	return errors.New("no GUI/TUI frontend compiled for this build; use -headless")
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
