package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/X-Ray-X/mailflow/app/internal/api"
	"github.com/X-Ray-X/mailflow/app/internal/config"
	"github.com/X-Ray-X/mailflow/app/internal/jmap"
	"github.com/X-Ray-X/mailflow/app/internal/mail"
	"github.com/X-Ray-X/mailflow/app/internal/store"
)

func main() {
	if err := run(); err != nil {
		slog.Error("startup failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)

	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}

	client, err := jmap.New(jmap.Config{
		URL:      cfg.JMAPURL,
		Hostname: cfg.JMAPHostname,
		CAFile:   cfg.CAFile,
		Timeout:  cfg.RequestTimeout,
	})
	if err != nil {
		return err
	}

	// The application database holds idempotency records, never mail. A
	// failure here stops startup: the service will not send without a record.
	db, err := store.Open(context.Background(), store.Config{
		Host:     cfg.DBHost,
		Port:     cfg.DBPort,
		Name:     cfg.DBName,
		User:     cfg.DBUser,
		Password: cfg.DBPassword,
		Logger:   log,
	})
	if err != nil {
		return err
	}
	defer db.Close()

	service := mail.NewService(client, cfg.PageSizeDefault, cfg.PageSizeMax, cfg.BodyMaxBytes, log)
	server := api.New(service, api.Options{
		Timeout:           cfg.RequestTimeout,
		PageSizeMax:       cfg.PageSizeMax,
		Logger:            log,
		Records:           db,
		IdempotencyTTL:    cfg.IdempotencyTTL,
		IdempotencyLease:  cfg.IdempotencyLease,
		SendMaxRecipients: cfg.SendMaxRecipients,
		SendMaxBytes:      cfg.SendMaxBytes,
	})

	httpServer := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       cfg.RequestTimeout + 5*time.Second,
		WriteTimeout:      cfg.RequestTimeout + 5*time.Second,
		IdleTimeout:       60 * time.Second,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Expired idempotency records are deleted in the background; the sweep
	// stops with the process.
	go db.RunJanitor(ctx, 10*time.Minute)

	serveErr := make(chan error, 1)
	go func() {
		log.Info("listening",
			"addr", cfg.ListenAddr,
			"jmap_url", cfg.JMAPURL.String(),
			"jmap_hostname", cfg.JMAPHostname,
			"page_size_default", cfg.PageSizeDefault,
			"page_size_max", cfg.PageSizeMax,
			"body_max_bytes", cfg.BodyMaxBytes,
			"db_host", cfg.DBHost,
			"db_name", cfg.DBName,
			"idempotency_ttl", cfg.IdempotencyTTL.String(),
			"idempotency_lease", cfg.IdempotencyLease.String(),
			"send_max_recipients", cfg.SendMaxRecipients,
			"send_max_bytes", cfg.SendMaxBytes,
		)
		serveErr <- httpServer.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		log.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	}
}
