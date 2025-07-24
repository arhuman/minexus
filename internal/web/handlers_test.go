package web

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/arhuman/minexus/internal/config"
	"go.uber.org/zap"
)

func createTestWebServer() *WebServer {
	cfg := &config.NexusConfig{
		MinionPort:  11972,
		ConsolePort: 11973,
		WebPort:     8086,
		WebEnabled:  true,
		WebRoot:     "./webroot",
	}

	logger := zap.NewNop()

	// Create templates using real file system approach
	templatesPath := filepath.Join(cfg.WebRoot, "templates", "*.html")
	templates, err := template.ParseGlob(templatesPath)
	if err != nil {
		// Fallback to embedded templates for testing if webroot doesn't exist
		templates, _ = GetTemplates()
	}

	return &WebServer{
		config:    cfg,
		nexus:     nil, // We'll test without a real nexus server
		templates: templates,
		logger:    logger,
		startTime: time.Now(),
	}
}

func TestHandleDashboardMethodNotAllowed(t *testing.T) {
	webServer := createTestWebServer()

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	w := httptest.NewRecorder()

	webServer.handleDashboard(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", resp.StatusCode)
	}
}

func TestHandleDashboardContentServing(t *testing.T) {
	webServer := createTestWebServer()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	webServer.handleDashboard(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/html") {
		t.Errorf("Expected HTML content type, got %s", contentType)
	}

	body := w.Body.String()

	// Verify key dashboard content is present
	expectedContent := []string{
		"Dashboard",         // Page title from data
		"System Status",     // Section header from template
		"Connected Minions", // Section header from template
		"Server Ports",      // Section header from template
		"8086",              // Web server port number
		"11972",             // Minion server port number
		"11973",             // Console server port number
		"Minion (gRPC)",     // Port label
		"Console (mTLS)",    // Port label
		"Web (HTTP)",        // Port label
	}

	for _, expected := range expectedContent {
		if !strings.Contains(body, expected) {
			t.Errorf("Expected dashboard to contain '%s', but it was missing", expected)
		}
	}

	// Verify template was actually executed (not just empty response)
	if len(body) < 100 {
		t.Errorf("Dashboard response too short (%d chars), likely template not rendering", len(body))
	}
}

func TestHandleAPIHealth(t *testing.T) {
	webServer := createTestWebServer()

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	w := httptest.NewRecorder()

	webServer.handleAPIHealth(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected JSON content type, got %s", contentType)
	}

	var healthResp HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&healthResp); err != nil {
		t.Fatalf("Failed to decode JSON response: %v", err)
	}

	if healthResp.Status != "healthy" {
		t.Errorf("Expected status 'healthy', got %s", healthResp.Status)
	}

	if healthResp.Timestamp == "" {
		t.Error("Expected timestamp to be set")
	}
}

func TestHandleAPIStatus(t *testing.T) {
	webServer := createTestWebServer()

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	w := httptest.NewRecorder()

	webServer.handleAPIStatus(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected JSON content type, got %s", contentType)
	}

	var statusResp StatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&statusResp); err != nil {
		t.Fatalf("Failed to decode JSON response: %v", err)
	}

	// Verify required fields are present
	if statusResp.Version == "" {
		t.Error("Expected version to be set")
	}
	if statusResp.Uptime == "" {
		t.Error("Expected uptime to be set")
	}
	if statusResp.Timestamp == "" {
		t.Error("Expected timestamp to be set")
	}

	// Verify server information
	if statusResp.Servers.Minion.Port != 11972 {
		t.Errorf("Expected minion port 11972, got %d", statusResp.Servers.Minion.Port)
	}
	if statusResp.Servers.Console.Port != 11973 {
		t.Errorf("Expected console port 11973, got %d", statusResp.Servers.Console.Port)
	}
	if statusResp.Servers.Web.Port != 8086 {
		t.Errorf("Expected web port 8086, got %d", statusResp.Servers.Web.Port)
	}

	// Verify all servers show as running
	if statusResp.Servers.Minion.Status != "running" {
		t.Errorf("Expected minion status 'running', got %s", statusResp.Servers.Minion.Status)
	}
	if statusResp.Servers.Console.Status != "running" {
		t.Errorf("Expected console status 'running', got %s", statusResp.Servers.Console.Status)
	}
	if statusResp.Servers.Web.Status != "running" {
		t.Errorf("Expected web status 'running', got %s", statusResp.Servers.Web.Status)
	}
}

func TestHandleAPIMinions(t *testing.T) {
	webServer := createTestWebServer()

	req := httptest.NewRequest(http.MethodGet, "/api/minions", nil)
	w := httptest.NewRecorder()

	webServer.handleAPIMinions(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected JSON content type, got %s", contentType)
	}

	var minionsResp MinionsResponse
	if err := json.NewDecoder(resp.Body).Decode(&minionsResp); err != nil {
		t.Fatalf("Failed to decode JSON response: %v", err)
	}

	// With no nexus server, should return 0 minions
	if minionsResp.Count != 0 {
		t.Errorf("Expected 0 minions (no nexus server), got %d", minionsResp.Count)
	}

	if len(minionsResp.Minions) != 0 {
		t.Errorf("Expected empty minions array, got %d minions", len(minionsResp.Minions))
	}
}

func TestHandleDownloadIndex(t *testing.T) {
	webServer := createTestWebServer()

	req := httptest.NewRequest(http.MethodGet, "/download/", nil)
	w := httptest.NewRecorder()

	webServer.handleDownload(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body := w.Body.String()
	if !strings.Contains(body, "Binary Downloads") {
		t.Error("Expected download index page to contain 'Binary Downloads'")
	}

	if !strings.Contains(body, "Minion Binaries") {
		t.Error("Expected download index to contain 'Minion Binaries'")
	}
}

func TestHandleDownloadBinary(t *testing.T) {
	webServer := createTestWebServer()

	testCases := []struct {
		path           string
		expectedStatus int
		expectedHeader string
	}{
		{"/download/minion/linux-amd64", http.StatusNotFound, "application/octet-stream"},
		{"/download/console/windows-amd64.exe", http.StatusNotFound, "application/octet-stream"},
		{"/download/invalid/linux-amd64", http.StatusBadRequest, ""},
		{"/download/minion/invalid-platform", http.StatusBadRequest, ""},
		{"/download/minion", http.StatusBadRequest, ""},
	}

	for _, tc := range testCases {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		w := httptest.NewRecorder()

		webServer.handleDownload(w, req)

		resp := w.Result()
		if resp.StatusCode != tc.expectedStatus {
			t.Errorf("Path %s: expected status %d, got %d", tc.path, tc.expectedStatus, resp.StatusCode)
		}

		if tc.expectedHeader != "" {
			contentType := resp.Header.Get("Content-Type")
			if contentType != tc.expectedHeader {
				t.Errorf("Path %s: expected Content-Type %s, got %s", tc.path, tc.expectedHeader, contentType)
			}
		}
	}
}

func TestServeBinaryFileValidation(t *testing.T) {
	webServer := createTestWebServer()

	testCases := []struct {
		name           string
		path           string
		expectedStatus int
		expectedBody   string
	}{
		{
			name:           "Valid minion binary",
			path:           "minion/linux-amd64",
			expectedStatus: http.StatusNotFound,
			expectedBody:   "Binary for minion/linux-amd64 not available",
		},
		{
			name:           "Valid console binary",
			path:           "console/windows-amd64.exe",
			expectedStatus: http.StatusNotFound,
			expectedBody:   "Binary for console/windows-amd64.exe not available",
		},
		{
			name:           "Invalid component",
			path:           "invalid/linux-amd64",
			expectedStatus: http.StatusBadRequest,
			expectedBody:   "Invalid component",
		},
		{
			name:           "Invalid platform",
			path:           "minion/invalid-platform",
			expectedStatus: http.StatusBadRequest,
			expectedBody:   "Invalid platform",
		},
		{
			name:           "Invalid path format",
			path:           "minion",
			expectedStatus: http.StatusBadRequest,
			expectedBody:   "Invalid download path",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/download/"+tc.path, nil)
			w := httptest.NewRecorder()

			webServer.serveBinaryFile(w, req, tc.path)

			resp := w.Result()
			if resp.StatusCode != tc.expectedStatus {
				t.Errorf("Expected status %d, got %d", tc.expectedStatus, resp.StatusCode)
			}

			body := w.Body.String()
			if !strings.Contains(body, tc.expectedBody) {
				t.Errorf("Expected body to contain %q, got %q", tc.expectedBody, body)
			}
		})
	}
}

func TestSetSecurityHeaders(t *testing.T) {
	webServer := createTestWebServer()
	w := httptest.NewRecorder()

	webServer.setSecurityHeaders(w)

	expectedHeaders := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"X-XSS-Protection":       "1; mode=block",
		"Referrer-Policy":        "strict-origin-when-cross-origin",
	}

	for header, expectedValue := range expectedHeaders {
		if got := w.Header().Get(header); got != expectedValue {
			t.Errorf("Expected header %s: %s, got %s", header, expectedValue, got)
		}
	}
}

func TestSetJSONHeaders(t *testing.T) {
	webServer := createTestWebServer()
	w := httptest.NewRecorder()

	webServer.setJSONHeaders(w)

	expectedHeaders := map[string]string{
		"Content-Type":                 "application/json",
		"Access-Control-Allow-Origin":  "*",
		"Access-Control-Allow-Methods": "GET, OPTIONS",
		"Access-Control-Allow-Headers": "Content-Type",
		"X-Content-Type-Options":       "nosniff",
		"X-Frame-Options":              "DENY",
		"X-XSS-Protection":             "1; mode=block",
		"Referrer-Policy":              "strict-origin-when-cross-origin",
	}

	for header, expectedValue := range expectedHeaders {
		if got := w.Header().Get(header); got != expectedValue {
			t.Errorf("Expected header %s: %s, got %s", header, expectedValue, got)
		}
	}
}

func TestWriteJSONError(t *testing.T) {
	webServer := createTestWebServer()
	w := httptest.NewRecorder()

	webServer.writeJSONError(w, http.StatusBadRequest, "test error", "test message")

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", resp.StatusCode)
	}

	var errorResp ErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&errorResp); err != nil {
		t.Fatalf("Failed to decode JSON response: %v", err)
	}

	if errorResp.Error != "test error" {
		t.Errorf("Expected error 'test error', got %s", errorResp.Error)
	}

	if errorResp.Message != "test message" {
		t.Errorf("Expected message 'test message', got %s", errorResp.Message)
	}
}

func TestGetUptime(t *testing.T) {
	startTime := time.Now().Add(-time.Hour * 2)
	webServer := &WebServer{startTime: startTime}

	uptime := webServer.getUptime()

	// Just check that uptime contains expected time components
	if !strings.Contains(uptime, "h") {
		t.Error("Expected uptime to contain hours")
	}
}

func TestLogRequest(t *testing.T) {
	webServer := createTestWebServer()

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	duration := time.Millisecond * 100

	// This test mainly ensures the method doesn't panic
	webServer.logRequest(req, 200, duration)
}

func TestLoggingMiddleware(t *testing.T) {
	webServer := createTestWebServer()

	testHandler := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test"))
	}

	wrappedHandler := webServer.loggingMiddleware(testHandler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	wrappedHandler(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body := w.Body.String()
	if body != "test" {
		t.Errorf("Expected body 'test', got %s", body)
	}
}

func TestResponseWriter(t *testing.T) {
	w := httptest.NewRecorder()
	rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

	rw.WriteHeader(http.StatusNotFound)

	if rw.statusCode != http.StatusNotFound {
		t.Errorf("Expected status code 404, got %d", rw.statusCode)
	}

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected underlying recorder code 404, got %d", w.Code)
	}
}

func TestBuildDashboardDataWithoutNexus(t *testing.T) {
	webServer := createTestWebServer()
	data := webServer.buildDashboardData()

	if data.Title != "Dashboard" {
		t.Errorf("Expected title 'Dashboard', got %s", data.Title)
	}

	if data.MinionCount != 0 {
		t.Errorf("Expected minion count 0 (no nexus server), got %d", data.MinionCount)
	}

	if data.SystemStatus != "warning" {
		t.Errorf("Expected system status 'warning' (no minions), got %s", data.SystemStatus)
	}

	if data.WebPort != 8086 {
		t.Errorf("Expected web port 8086, got %d", data.WebPort)
	}
}

func TestGetConnectedMinionsWithoutNexus(t *testing.T) {
	webServer := createTestWebServer()
	minions := webServer.getConnectedMinions()

	if len(minions) != 0 {
		t.Errorf("Expected 0 minions (no nexus server), got %d", len(minions))
	}
}

func TestGetDatabaseStatusWithoutNexus(t *testing.T) {
	webServer := createTestWebServer()
	status := webServer.getDatabaseStatus()

	if status != "disconnected" {
		t.Errorf("Expected 'disconnected' (no nexus server), got %s", status)
	}
}

// Test that validates platform names are correct
func TestValidPlatformNames(t *testing.T) {
	webServer := createTestWebServer()

	validPlatforms := []string{
		"linux-amd64",
		"linux-arm64",
		"windows-amd64.exe",
		"windows-arm64.exe",
		"darwin-amd64",
		"darwin-arm64",
	}

	for _, platform := range validPlatforms {
		path := fmt.Sprintf("minion/%s", platform)
		req := httptest.NewRequest(http.MethodGet, "/download/"+path, nil)
		w := httptest.NewRecorder()

		webServer.serveBinaryFile(w, req, path)

		resp := w.Result()
		// Should get 404 (not found) rather than 400 (bad request) for valid platforms
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("Platform %s: expected status 404 (valid platform), got %d", platform, resp.StatusCode)
		}
	}
}

// Integration test that actually starts HTTP server and tests content serving
func TestWebServerIntegration(t *testing.T) {
	webServer := createTestWebServer()

	// Setup routes manually like StartWebServer does
	mux := http.NewServeMux()

	// Static assets from file system
	staticDir := fmt.Sprintf("%s/static", webServer.config.WebRoot)
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

	// Create test server
	testServer := httptest.NewServer(mux)
	defer testServer.Close()

	// Test dashboard endpoint
	resp, err := http.Get(testServer.URL + "/")
	if err != nil {
		t.Fatalf("Failed to GET dashboard: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Dashboard: expected status 200, got %d", resp.StatusCode)
	}

	// Test API health endpoint
	resp, err = http.Get(testServer.URL + "/api/health")
	if err != nil {
		t.Fatalf("Failed to GET health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Health: expected status 200, got %d", resp.StatusCode)
	}

	// Verify JSON response content
	var healthResp HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&healthResp); err != nil {
		t.Fatalf("Failed to decode health JSON: %v", err)
	}

	if healthResp.Status != "healthy" {
		t.Errorf("Expected healthy status, got %s", healthResp.Status)
	}

	// Test API status endpoint
	resp, err = http.Get(testServer.URL + "/api/status")
	if err != nil {
		t.Fatalf("Failed to GET status: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Status: expected status 200, got %d", resp.StatusCode)
	}

	// Test download index
	resp, err = http.Get(testServer.URL + "/download/")
	if err != nil {
		t.Fatalf("Failed to GET download index: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Download index: expected status 200, got %d", resp.StatusCode)
	}
}
