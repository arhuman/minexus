package web

import (
	"fmt"
	"net/http"
	"time"

	"github.com/arhuman/minexus/internal/config"
	"github.com/arhuman/minexus/internal/nexus"
	"go.uber.org/zap"
)

// StartWebServer starts the HTTP web server
func StartWebServer(cfg *config.NexusConfig, nexusServer *nexus.Server, logger *zap.Logger) error {
	if !cfg.WebEnabled {
		logger.Info("Web server disabled")
		return nil
	}

	// Create web server instance
	webServer, err := NewWebServer(cfg, nexusServer, logger)
	if err != nil {
		return fmt.Errorf("failed to create web server: %w", err)
	}

	// Create HTTP multiplexer
	mux := http.NewServeMux()

	// Static assets from file system
	staticDir := fmt.Sprintf("%s/static", cfg.WebRoot)
	staticHandler := http.StripPrefix("/static/", http.FileServer(http.Dir(staticDir)))
	mux.Handle("/static/", staticHandler)

	// Dashboard with file system templates
	mux.HandleFunc("/", webServer.loggingMiddleware(webServer.handleDashboard))

	// Binary downloads
	mux.HandleFunc("/download/", webServer.loggingMiddleware(webServer.handleDownload))

	// API endpoints
	mux.HandleFunc("/api/status", webServer.loggingMiddleware(webServer.handleAPIStatus))
	mux.HandleFunc("/api/minions", webServer.loggingMiddleware(webServer.handleAPIMinions))
	mux.HandleFunc("/api/health", webServer.loggingMiddleware(webServer.handleAPIHealth))

	// Create HTTP server with appropriate timeouts
	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.WebPort),
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	logger.Info("Web server starting with file system assets",
		zap.Int("port", cfg.WebPort),
		zap.String("webroot", cfg.WebRoot),
		zap.String("address", server.Addr))

	return server.ListenAndServe()
}
