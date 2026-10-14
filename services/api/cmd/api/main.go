package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"ifri-admissions/services/api/internal/config"
	"ifri-admissions/services/api/internal/modules/admin"
	"ifri-admissions/services/api/internal/modules/analysis"
	"ifri-admissions/services/api/internal/modules/documents"
	"ifri-admissions/services/api/internal/modules/drafts"
	"ifri-admissions/services/api/internal/modules/references"
	"ifri-admissions/services/api/internal/modules/review"
	"ifri-admissions/services/api/internal/modules/submissions"
	"ifri-admissions/services/api/internal/platform/database"
	"ifri-admissions/services/api/internal/platform/httpserver"
	"ifri-admissions/services/api/internal/platform/pdfcheck"
	"ifri-admissions/services/api/internal/platform/ratelimit"
	"ifri-admissions/services/api/internal/platform/storage"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}
	if len(os.Args) >= 2 && os.Args[1] == "admin" {
		os.Exit(adminCommand(os.Args[2:]))
	}
	if len(os.Args) >= 2 && os.Args[1] == "dev" {
		os.Exit(devCommand(os.Args[2:]))
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
	objects, err := storage.NewS3(c.S3Endpoint, c.S3Bucket, c.S3Region, c.S3AccessKey, c.S3SecretKey)
	if err != nil {
		return err
	}
	if _, err = exec.LookPath("qpdf"); err != nil {
		return fmt.Errorf("qpdf is required for PDF validation")
	}

	secret := os.Getenv("OCR_INTERNAL_SECRET")
	if len(secret) < 32 || (c.Environment == "production" && strings.HasPrefix(secret, "replace-dev-")) {
		return fmt.Errorf("OCR_INTERNAL_SECRET must contain at least 32 characters")
	}
	threshold, err := strconv.ParseFloat(os.Getenv("OCR_MIN_CONFIDENCE"), 64)
	if err != nil || math.IsNaN(threshold) || math.IsInf(threshold, 0) || threshold < 0.5 || threshold > 1 {
		return fmt.Errorf("OCR_MIN_CONFIDENCE must be between 0.5 and 1")
	}
	analysisRepo := analysis.Repository{Pool: pool, Threshold: threshold}
	internal := http.NewServeMux()
	analysisHandler := analysis.Handler{Logger: logger, Repo: analysisRepo, Origin: c.FrontendOrigin, Secret: secret, Objects: objects}
	analysisHandler.Ready = func(ctx context.Context) error {
		if err := pool.Ping(ctx); err != nil {
			return err
		}
		if err := cache.Ping(ctx).Err(); err != nil {
			return err
		}
		return storage.Readiness(c.MinioEndpoint)(ctx)
	}
	analysisHandler.RegisterInternal(internal)
	// OCR proof images: separate stream and worker loop, same private listener.
	reviewService := &review.Service{Pool: pool, Objects: objects, Cache: cache, Logger: logger}
	reviewService.RegisterInternal(internal, secret)
	internalServer := &http.Server{Addr: net.JoinHostPort(c.Host, "8081"), Handler: internal, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 35 * time.Second}
	internalListener, err := net.Listen("tcp", internalServer.Addr)
	if err != nil {
		return fmt.Errorf("internal OCR listener unavailable")
	}
	defer internalListener.Close()
	go func() {
		if err := internalServer.Serve(internalListener); err != nil && err != http.ErrServerClosed {
			logger.Error("internal OCR server stopped")
			stop()
		}
	}()
	defer internalServer.Close()
	go analysisRepo.Dispatch(ctx, cache, logger)
	adminService, err := newAdminService(c, logger, ratelimit.Redis{Client: cache, Prefix: "ifri:admin:rl:"})
	if err != nil {
		return fmt.Errorf("administration setup failed")
	}
	adminService.Pool = pool
	go reviewService.DispatchProofs(ctx)
	handler := httpserver.New(c.FrontendOrigin, map[string]httpserver.Check{
		"postgres": pool.Ping,
		"redis":    func(ctx context.Context) error { return cache.Ping(ctx).Err() },
		"minio":    storage.Readiness(c.MinioEndpoint),
	}, logger, func(mux *http.ServeMux) {
		analysisHandler.RegisterPublic(mux)
		references.Register(mux, references.NewService(references.NewRepository(pool)), logger)
		drafts.Register(mux, drafts.NewRepository(pool), c.DraftSessionTTL, c.FrontendOrigin, c.Environment == "production", logger)
		documents.Register(mux, documents.NewService(documents.NewRepository(pool), objects, pdfcheck.QPDF{}, c.DocumentMaxBytes, logger), c.FrontendOrigin, c.Environment == "production", logger)
		submissions.Register(mux, &submissions.Repository{Pool: pool, Logger: logger}, c.FrontendOrigin, c.Environment == "production", logger)
		review.Register(admin.Register(mux, adminService, c.FrontendOrigin, c.Environment == "production", logger), reviewService)
	})
	server := &http.Server{Addr: net.JoinHostPort(c.Host, c.Port), Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 70 * time.Second, WriteTimeout: 75 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 16}
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
