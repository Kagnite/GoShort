package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"goshort/internal/config"
	httpserver "goshort/internal/http"
	"goshort/internal/logging"
	"goshort/internal/security"
	"goshort/internal/service"
	"goshort/internal/storage/postgres"
	"goshort/internal/storage/redis"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	// Initialize logger
	logger := logging.NewLogger(cfg)
	defer func() {
		_ = logger.Sync()
		}()


	logger.Infow("starting goshort service",
		"version", "1.0.0",
		"environment", cfg.Server.Environment,
	)

	// Connect to PostgreSQL
	db, err := postgres.Connect(
		cfg.Database.Host,
		cfg.Database.Port,
		cfg.Database.User,
		cfg.Database.Password,
		cfg.Database.DBName,
		cfg.Database.SSLMode,
	)
	if err != nil {
		logger.Fatalw("failed to connect to database", "error", err)
	}
	defer db.Close()
	logger.Info("connected to PostgreSQL")

	// Connect to Redis
	redisClient, err := redis.Connect(
		cfg.Redis.Host,
		cfg.Redis.Port,
		cfg.Redis.Password,
		cfg.Redis.DB,
	)
	if err != nil {
		logger.Fatalw("failed to connect to Redis", "error", err)
	}
	defer redisClient.Close()
	logger.Info("connected to Redis")

	// Initialize repositories
	urlRepo := postgres.NewPostgresRepository(db)
	cacheRepo := redis.NewRedisCache(redisClient)
	rateLimiter := redis.NewRedisRateLimiter(redisClient, cfg.Security.RateLimitRequestsPerMin)

	// Initialize SSRF validator
	ssrfValidator := initializeSSRFValidator(cfg)
	logger.Infow("SSRF protection initialized",
		"allowlist_enabled", cfg.Security.UseAllowlist,
		"allowed_domains_count", len(cfg.Security.AllowedDomains),
		"allowed_ports", cfg.Security.AllowedPorts,
	)

	// Initialize service
	urlService := service.NewURLShortenerService(
		urlRepo,
		cacheRepo,
		rateLimiter,
		ssrfValidator,
		logger,
		cfg.Security.ShortCodeLength,
		cfg.Security.ShortCodeAlphabet,
	)

	// Create HTTP router
	router := httpserver.NewRouter(cfg, logger, urlService)

	// Create HTTP server
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	server := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
		MaxHeaderBytes: 1 << 20, // 1 MB
	}

	// Channel to listen for errors
	serverErrors := make(chan error, 1)

	// Start server
	go func() {
		logger.Infow("starting HTTP server",
			"address", addr,
			"environment", cfg.Server.Environment,
		)
		serverErrors <- server.ListenAndServe()
	}()

	// Channel to listen for interrupt/terminate signals
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	// Block and wait for shutdown
	select {
	case err := <-serverErrors:
		logger.Fatalw("server error", "error", err)

	case sig := <-shutdown:
		logger.Infow("shutdown signal received", "signal", sig)

		// Graceful shutdown with timeout
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		if err := server.Shutdown(ctx); err != nil {
			server.Close()
			logger.Fatalw("could not gracefully shutdown server", "error", err)
		}

		logger.Info("server stopped gracefully")
	}
}

// initializeSSRFValidator creates SSRF validator with configuration
func initializeSSRFValidator(cfg *config.Config) security.SSRFValidator {
	ssrfConfig := security.SSRFConfig{
		AllowedDomains:       cfg.Security.AllowedDomains,
		UseAllowlist:         cfg.Security.UseAllowlist,
		AllowedPorts:         cfg.Security.AllowedPorts,
		MaxRedirects:         cfg.Security.MaxRedirects,
		Timeout:              time.Duration(cfg.Security.TimeoutSeconds) * time.Second,
		DisableIPLiterals:    cfg.Security.DisableIPLiterals,
		DNSRevalidationCount: cfg.Security.DNSRevalidationCount,
		DNSRevalidationDelay: time.Duration(cfg.Security.DNSRevalidationDelayMs) * time.Millisecond,
	}

	return security.NewSSRFValidator(ssrfConfig)
}

