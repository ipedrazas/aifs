// Command aifs is the AI Fashion Studio server.
//
// Usage:
//
//	aifs              start the server (same as "aifs serve")
//	aifs healthcheck  probe /healthz on the local server; exit 0 when healthy
//	aifs version      print the build version
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ipedrazas/aifs/api/internal/auth"
	"github.com/ipedrazas/aifs/api/internal/config"
	"github.com/ipedrazas/aifs/api/internal/db"
	"github.com/ipedrazas/aifs/api/internal/httpapi"
	"github.com/ipedrazas/aifs/api/internal/version"
	"github.com/ipedrazas/aifs/api/internal/webui"
)

func main() {
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}

	var err error
	switch cmd {
	case "serve":
		err = serve()
	case "healthcheck":
		err = healthcheck()
	case "version":
		fmt.Printf("%s (%s)\n", version.Version, version.Commit)
	default:
		err = fmt.Errorf("unknown command %q (want serve, healthcheck or version)", cmd)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "aifs:", err)
		os.Exit(1)
	}
}

func serve() error {
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	var handler slog.Handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel})
	if cfg.Dev() {
		handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel})
	}
	log := slog.New(handler)
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	database, err := db.Open(ctx, cfg.DataDir)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer func() { _ = database.Close() }()

	schema, _ := database.SchemaVersion(ctx)
	log.Info("starting",
		"version", version.Version,
		"commit", version.Commit,
		"env", cfg.Env,
		"addr", cfg.Addr,
		"data_dir", cfg.DataDir,
		"schema_version", schema,
	)

	if cfg.Dev() && cfg.Password == config.DevPassword {
		log.Warn("development mode: using the default password", "password", config.DevPassword)
	}
	if !cfg.CookieSecure {
		log.Warn("session cookie is not marked Secure (AIFS_COOKIE_SECURE=false); serve over HTTPS where possible")
	}

	authn, err := auth.New(ctx, database, auth.Options{
		Password: cfg.Password,
		TTL:      cfg.SessionTTL,
		Logger:   log,
	})
	if err != nil {
		return fmt.Errorf("auth: %w", err)
	}

	srv := &http.Server{
		Addr: cfg.Addr,
		Handler: httpapi.New(httpapi.Deps{
			Logger:       log,
			DB:           database,
			Web:          webui.FS(),
			Auth:         authn,
			CookieSecure: cfg.CookieSecure,
		}),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       2 * time.Minute, // uploads arrive in later phases
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       2 * time.Minute,
		BaseContext:       func(net.Listener) context.Context { return ctx },
		ErrorLog:          slog.NewLogLogger(handler, slog.LevelWarn),
	}

	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}

	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	if err := <-errc; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// healthcheck lets the distroless image (which has no shell or curl) run a
// Docker HEALTHCHECK against itself.
func healthcheck() error {
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	_, port, err := net.SplitHostPort(cfg.Addr)
	if err != nil {
		return fmt.Errorf("AIFS_ADDR: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+port+"/healthz", nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unhealthy: %s", resp.Status)
	}
	return nil
}
