package web

import (
	"time"
)

// DashboardData represents data for the dashboard template
type DashboardData struct {
	Title        string       `json:"title"`
	Version      string       `json:"version"`
	Uptime       string       `json:"uptime"`
	SystemStatus string       `json:"system_status"`
	MinionCount  int          `json:"minion_count"`
	MinionPort   int          `json:"minion_port"`
	ConsolePort  int          `json:"console_port"`
	WebPort      int          `json:"web_port"`
	Minions      []MinionInfo `json:"minions"`
}

// MinionInfo represents information about a connected minion
type MinionInfo struct {
	ID          string    `json:"id"`
	Status      string    `json:"status"`
	ConnectedAt time.Time `json:"connected_at"`
	LastSeen    time.Time `json:"last_seen"`
}

// StatusResponse represents the API status response
type StatusResponse struct {
	Version   string           `json:"version"`
	Uptime    string           `json:"uptime"`
	Timestamp string           `json:"timestamp"`
	Servers   ServerStatusInfo `json:"servers"`
	Database  DatabaseStatus   `json:"database"`
}

// ServerStatusInfo represents server status information
type ServerStatusInfo struct {
	Minion  ServerInfo `json:"minion"`
	Console ServerInfo `json:"console"`
	Web     ServerInfo `json:"web"`
}

// ServerInfo represents individual server information
type ServerInfo struct {
	Port        int    `json:"port"`
	Status      string `json:"status"`
	Connections int    `json:"connections,omitempty"`
}

// DatabaseStatus represents database connection status
type DatabaseStatus struct {
	Status string `json:"status"`
	Host   string `json:"host"`
}

// MinionsResponse represents the API minions response
type MinionsResponse struct {
	Count   int          `json:"count"`
	Minions []MinionInfo `json:"minions"`
}

// HealthResponse represents the API health response
type HealthResponse struct {
	Status    string `json:"status"`
	Timestamp string `json:"timestamp"`
}

// ErrorResponse represents an API error response
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}
