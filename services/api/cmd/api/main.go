package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"ifri-admissions/services/api/internal/config"
	"ifri-admissions/services/api/internal/modules/drafts"
	"ifri-admissions/services/api/internal/modules/references"
	"ifri-admissions/services/api/internal/platform/database"
	"ifri-admissions/services/api/internal/platform/httpserver"
	"ifri-admissions/services/api/internal/platform/storage"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", "api", "environment", os.Getenv("APP_ENV"))
	if err := run(logger); err != nil {
		logger.Error("bootstrap failed", "reason", err.Error())
		os.Exit(1)
	}
}
func healthcheck() int {
	client := http.Client{Timeout: 3 * time.Second}
	res, err := client.Get("http://127.0.0.1:" + os.Getenv("API_PORT") + "/ready")
	if err != nil {
		return 1
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return 1
	}
	return 0
}
func run(logger *slog.Logger) error {
	c, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	pool, err := database.Open(ctx, c.DatabaseURL())
	if err != nil {
		return fmt.Errorf("PostgreSQL client configuration failed")
	}
	defer pool.Close()
	options := &redis.Options{Addr: c.RedisAddress(), Password: c.RedisPassword, DialTimeout: 2 * time.Second, ReadTimeout: 2 * time.Second, WriteTimeout: 2 * time.Second, MaxRetries: -1}
	if c.RedisTLS {
		options.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	cache := redis.NewClient(options)
	defer cache.Close()
	handler := httpserver.New(c.FrontendOrigin, map[string]httpserver.Check{
		"postgres": pool.Ping,
		"redis":    func(ctx context.Context) error { return cache.Ping(ctx).Err() },
		"minio":    storage.Readiness(c.MinioEndpoint),
	}, logger, func(mux *http.ServeMux) {
		references.Register(mux, references.NewService(references.NewRepository(pool)), logger)
		drafts.Register(mux, drafts.NewRepository(pool), c.DraftSessionTTL, c.FrontendOrigin, c.Environment == "production", logger)
	})
	server := &http.Server{Addr: net.JoinHostPort(c.Host, c.Port), Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 16}
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return fmt.Errorf("HTTP listen failed: check API_HOST, API_PORT and socket permissions")
	}
	defer listener.Close()
	errs := make(chan error, 1)
	go func() { errs <- server.Serve(listener) }()
	logger.Info("http server started", "port", c.Port)
	select {
	case err := <-errs:
		if err != http.ErrServerClosed {
			return fmt.Errorf("HTTP server failed to listen")
		}
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			return fmt.Errorf("HTTP shutdown timed out")
		}
	}
	logger.Info("http server stopped")
	return nil
}
