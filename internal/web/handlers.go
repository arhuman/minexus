package web

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/arhuman/minexus/internal/config"
	"github.com/arhuman/minexus/internal/nexus"
	"github.com/arhuman/minexus/internal/version"
	pb "github.com/arhuman/minexus/protogen"
	"go.uber.org/zap"
)

// WebServer represents the HTTP web server
type WebServer struct {
	config    *config.NexusConfig
	nexus     *nexus.Server
	templates *template.Template
	logger    *zap.Logger
	startTime time.Time
}

// NewWebServer creates a new web server instance
func NewWebServer(cfg *config.NexusConfig, nexusServer *nexus.Server, logger *zap.Logger) (*WebServer, error) {
	// Load templates from file system
	templatesPath := fmt.Sprintf("%s/templates/*.html", cfg.WebRoot)
	templates, err := template.ParseGlob(templatesPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load web templates from %s: %w", templatesPath, err)
	}

	return &WebServer{
		config:    cfg,
		nexus:     nexusServer,
		templates: templates,
		logger:    logger,
		startTime: time.Now(),
	}, nil
}

// handleDashboard serves the main dashboard page
func (ws *WebServer) handleDashboard(w http.ResponseWriter, r *http.Request) {
	ws.setSecurityHeaders(w)

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	data := ws.buildDashboardData()

	if err := ws.templates.ExecuteTemplate(w, "base.html", data); err != nil {
		ws.logger.Error("Failed to execute dashboard template", zap.Error(err))
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
}

// handleDownload serves binary downloads
func (ws *WebServer) handleDownload(w http.ResponseWriter, r *http.Request) {
	ws.setSecurityHeaders(w)

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract path after /download/
	path := strings.TrimPrefix(r.URL.Path, "/download/")
	if path == "" || path == "/" {
		ws.serveDownloadIndex(w, r)
		return
	}

	// Serve binary file
	ws.serveBinaryFile(w, r, path)
}

// serveDownloadIndex serves the download index page
func (ws *WebServer) serveDownloadIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	html := `
<!DOCTYPE html>
<html>
<head>
    <title>Downloads - Minexus</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 2rem; }
        .download-section { margin-bottom: 2rem; }
        .download-link { display: block; margin: 0.5rem 0; }
    </style>
</head>
<body>
    <h1>Binary Downloads</h1>
    <div class="download-section">
        <h2>Minion Binaries</h2>
        <a href="/download/minion/linux-amd64" class="download-link">Linux x64</a>
        <a href="/download/minion/linux-arm64" class="download-link">Linux ARM64</a>
        <a href="/download/minion/windows-amd64.exe" class="download-link">Windows x64</a>
        <a href="/download/minion/windows-arm64.exe" class="download-link">Windows ARM64</a>
        <a href="/download/minion/darwin-amd64" class="download-link">macOS x64</a>
        <a href="/download/minion/darwin-arm64" class="download-link">macOS ARM64</a>
    </div>
    <div class="download-section">
        <h2>Console Binaries</h2>
        <a href="/download/console/linux-amd64" class="download-link">Linux x64</a>
        <a href="/download/console/linux-arm64" class="download-link">Linux ARM64</a>
        <a href="/download/console/windows-amd64.exe" class="download-link">Windows x64</a>
        <a href="/download/console/windows-arm64.exe" class="download-link">Windows ARM64</a>
        <a href="/download/console/darwin-amd64" class="download-link">macOS x64</a>
        <a href="/download/console/darwin-arm64" class="download-link">macOS ARM64</a>
    </div>
    <p><a href="/">← Back to Dashboard</a></p>
</body>
</html>`
	w.Write([]byte(html))
}

// handleAPIStatus serves the /api/status endpoint
func (ws *WebServer) handleAPIStatus(w http.ResponseWriter, r *http.Request) {
	ws.setJSONHeaders(w)

	if r.Method != http.MethodGet {
		ws.writeJSONError(w, http.StatusMethodNotAllowed, "Method not allowed", "Only GET requests are supported")
		return
	}

	response := StatusResponse{
		Version:   version.Component("Nexus"),
		Uptime:    ws.getUptime(),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Servers: ServerStatusInfo{
			Minion: ServerInfo{
				Port:        ws.config.MinionPort,
				Status:      "running",
				Connections: ws.getMinionConnectionCount(),
			},
			Console: ServerInfo{
				Port:        ws.config.ConsolePort,
				Status:      "running",
				Connections: ws.getConsoleConnectionCount(),
			},
			Web: ServerInfo{
				Port:   ws.config.WebPort,
				Status: "running",
			},
		},
		Database: DatabaseStatus{
			Status: ws.getDatabaseStatus(),
			Host:   fmt.Sprintf("%s:%d", ws.config.DBHost, ws.config.DBPort),
		},
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		ws.logger.Error("Failed to encode status response", zap.Error(err))
		ws.writeJSONError(w, http.StatusInternalServerError, "Internal Server Error", "Failed to encode response")
	}
}

// handleAPIMinions serves the /api/minions endpoint
func (ws *WebServer) handleAPIMinions(w http.ResponseWriter, r *http.Request) {
	ws.setJSONHeaders(w)

	if r.Method != http.MethodGet {
		ws.writeJSONError(w, http.StatusMethodNotAllowed, "Method not allowed", "Only GET requests are supported")
		return
	}

	minions := ws.getConnectedMinions()
	response := MinionsResponse{
		Count:   len(minions),
		Minions: minions,
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		ws.logger.Error("Failed to encode minions response", zap.Error(err))
		ws.writeJSONError(w, http.StatusInternalServerError, "Internal Server Error", "Failed to encode response")
	}
}

// handleAPIHealth serves the /api/health endpoint
func (ws *WebServer) handleAPIHealth(w http.ResponseWriter, r *http.Request) {
	ws.setJSONHeaders(w)

	if r.Method != http.MethodGet {
		ws.writeJSONError(w, http.StatusMethodNotAllowed, "Method not allowed", "Only GET requests are supported")
		return
	}

	response := HealthResponse{
		Status:    "healthy",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		ws.logger.Error("Failed to encode health response", zap.Error(err))
		ws.writeJSONError(w, http.StatusInternalServerError, "Internal Server Error", "Failed to encode response")
	}
}

// buildDashboardData constructs data for the dashboard template
func (ws *WebServer) buildDashboardData() DashboardData {
	minions := ws.getConnectedMinions()
	systemStatus := "healthy"

	// Determine system status based on various factors
	if len(minions) == 0 {
		systemStatus = "warning"
	}

	return DashboardData{
		Title:        "Dashboard",
		Version:      version.Component("Nexus"),
		Uptime:       ws.getUptime(),
		SystemStatus: systemStatus,
		MinionCount:  len(minions),
		MinionPort:   ws.config.MinionPort,
		ConsolePort:  ws.config.ConsolePort,
		WebPort:      ws.config.WebPort,
		Minions:      minions,
	}
}

// getUptime returns formatted uptime string
func (ws *WebServer) getUptime() string {
	duration := time.Since(ws.startTime)
	days := int(duration.Hours()) / 24
	hours := int(duration.Hours()) % 24
	minutes := int(duration.Minutes()) % 60
	seconds := int(duration.Seconds()) % 60

	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm %ds", days, hours, minutes, seconds)
	} else if hours > 0 {
		return fmt.Sprintf("%dh %dm %ds", hours, minutes, seconds)
	} else if minutes > 0 {
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	} else {
		return fmt.Sprintf("%ds", seconds)
	}
}

// getConnectedMinions returns information about connected minions
func (ws *WebServer) getConnectedMinions() []MinionInfo {
	if ws.nexus == nil {
		return []MinionInfo{}
	}

	ctx := context.Background()
	minionList, err := ws.nexus.ListMinions(ctx, &pb.Empty{})
	if err != nil {
		ws.logger.Error("Failed to get minion list", zap.Error(err))
		return []MinionInfo{}
	}

	var minions []MinionInfo
	for _, hostInfo := range minionList.Minions {
		lastSeen := time.Unix(hostInfo.LastSeen, 0)
		minion := MinionInfo{
			ID:          hostInfo.Id,
			Status:      "active", // All listed minions are considered active
			ConnectedAt: lastSeen, // Use last seen as a proxy for connected time
			LastSeen:    lastSeen,
		}
		minions = append(minions, minion)
	}

	return minions
}

// getMinionConnectionCount returns the number of connected minions
func (ws *WebServer) getMinionConnectionCount() int {
	return len(ws.getConnectedMinions())
}

// getConsoleConnectionCount returns the number of console connections
func (ws *WebServer) getConsoleConnectionCount() int {
	// For now, assume 0-1 console connections since we don't track them separately
	return 0 // Could be enhanced to track actual console connections
}

// getDatabaseStatus returns database connection status
func (ws *WebServer) getDatabaseStatus() string {
	if ws.nexus == nil {
		return "disconnected"
	}

	// Try a simple health check by calling ListMinions
	ctx := context.Background()
	_, err := ws.nexus.ListMinions(ctx, &pb.Empty{})
	if err != nil {
		ws.logger.Error("Database health check failed", zap.Error(err))
		return "disconnected"
	}
	return "connected"
}

// setSecurityHeaders sets security headers for HTTP responses
func (ws *WebServer) setSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("X-XSS-Protection", "1; mode=block")
	w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
}

// setJSONHeaders sets headers for JSON API responses
func (ws *WebServer) setJSONHeaders(w http.ResponseWriter) {
	ws.setSecurityHeaders(w)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
}

// writeJSONError writes a JSON error response
func (ws *WebServer) writeJSONError(w http.ResponseWriter, statusCode int, error, message string) {
	w.WriteHeader(statusCode)
	response := ErrorResponse{
		Error:   error,
		Message: message,
	}
	json.NewEncoder(w).Encode(response)
}

// logRequest logs HTTP requests
func (ws *WebServer) logRequest(r *http.Request, statusCode int, duration time.Duration) {
	ws.logger.Info("HTTP request processed",
		zap.String("method", r.Method),
		zap.String("path", r.URL.Path),
		zap.String("remote_addr", r.RemoteAddr),
		zap.String("user_agent", r.UserAgent()),
		zap.Int("status", statusCode),
		zap.Duration("duration", duration),
	)
}

// loggingMiddleware provides request logging middleware
func (ws *WebServer) loggingMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Create a response writer that captures the status code
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next(wrapped, r)

		ws.logRequest(r, wrapped.statusCode, time.Since(start))
	}
}

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// serveBinaryFile serves binary files for download
func (ws *WebServer) serveBinaryFile(w http.ResponseWriter, r *http.Request, path string) {
	// Parse the path to extract component and platform
	parts := strings.Split(path, "/")
	if len(parts) != 2 {
		http.Error(w, "Invalid download path. Expected format: /download/component/platform", http.StatusBadRequest)
		return
	}

	component := parts[0] // minion or console
	platform := parts[1]  // linux-amd64, windows-amd64.exe, etc.

	// Validate component
	if component != "minion" && component != "console" {
		http.Error(w, "Invalid component. Must be 'minion' or 'console'", http.StatusBadRequest)
		return
	}

	// Validate platform format
	validPlatforms := map[string]bool{
		"linux-amd64":       true,
		"linux-arm64":       true,
		"windows-amd64.exe": true,
		"windows-arm64.exe": true,
		"darwin-amd64":      true,
		"darwin-arm64":      true,
	}

	if !validPlatforms[platform] {
		http.Error(w, "Invalid platform", http.StatusBadRequest)
		return
	}

	// Construct the binary file path
	binaryPath := fmt.Sprintf("binaries/%s/%s", component, platform)

	ws.logger.Info("Binary download requested",
		zap.String("component", component),
		zap.String("platform", platform),
		zap.String("path", binaryPath),
		zap.String("remote_addr", r.RemoteAddr))

	// Set appropriate headers for binary download
	filename := component
	if strings.HasSuffix(platform, ".exe") {
		filename += ".exe"
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
	w.Header().Set("X-Content-Type-Options", "nosniff")

	// Serve the file
	http.ServeFile(w, r, binaryPath)
}
