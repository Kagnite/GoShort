package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/gin-gonic/gin"
)

type ShortenRequest struct {
	URL string `json:"url" binding:"required,url"`
}

func main() {
	// -------------------------------------------------------------------
	// Application settings
	// -------------------------------------------------------------------

	gin.SetMode(gin.ReleaseMode)

	port := getEnv("PORT", "8080") // configurable port
	r := gin.New()

	// -------------------------------------------------------------------
	// Middlewares
	// -------------------------------------------------------------------

	r.Use(gin.Recovery())        // prevents server crash on panic
	r.Use(gin.Logger())          // structured logging
	r.Use(securityHeaders())     // basic security hardening headers
	r.Use(requestTimeout(5 * time.Second)) // timeout middleware

	// -------------------------------------------------------------------
	// Routes
	// -------------------------------------------------------------------

	r.GET("/", healthHandler)
	r.GET("/ping", func(c *gin.Context) {
		c.String(http.StatusOK, "pong")
	})

	r.POST("/shorten", shortenHandler)

	// -------------------------------------------------------------------
	// Graceful Server Setup
	// -------------------------------------------------------------------

	server := &http.Server{
		Addr:    ":" + port,
		Handler: r,
	}

	// Run server in goroutine
	go func() {
		log.Printf("GoShort running on port %s\n", port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Startup failed: %v", err)
		}
	}()

	// Handle graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)
	<-quit

	log.Println("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Forced shutdown: %v", err)
	}

	log.Println("Server exited cleanly")
}

//
// ----------------------- HANDLERS -------------------------------------
//

func healthHandler(c *gin.Context) {
	hostname, _ := os.Hostname()
	c.JSON(http.StatusOK, gin.H{
		"service": "GoShort",
		"version": "v1.0.0",
		"status":  "healthy",
		"server":  hostname,
		"uptime":  time.Now().Format(time.RFC3339),
	})
}

func shortenHandler(c *gin.Context) {
	var req ShortenRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid URL format"})
		return
	}

	// TODO: Replace with real shortener logic + DB integration
	shortURL := "https://goshort.ly/" + "xyz123"

	c.JSON(http.StatusOK, gin.H{
		"original":  req.URL,
		"shortened": shortURL,
		"source":    "Mock DB",
	})
}

//
// ----------------------- MIDDLEWARES ----------------------------------
//

func securityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-XSS-Protection", "1; mode=block")
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("Content-Security-Policy", "default-src 'self'")
		c.Next()
	}
}

func requestTimeout(timeout time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
		defer cancel()

		c.Request = c.Request.WithContext(ctx)

		c.Next()
	}
}

//
// ----------------------- HELPERS --------------------------------------
//

func getEnv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
