package types

import "time"

type Workflow struct {
	ID           string       `json:"workflow_id"`
	UserID       string       `json:"user_id"`
	Name         string       `json:"name"`
	Version      int          `json:"version"`
	Trigger      Trigger      `json:"trigger"`
	Nodes        []Node       `json:"nodes"`
	Connections  []Connection `json:"connections"`
	ErrorHandler ErrorHandler `json:"error_handler"`
	CreatedAt    time.Time    `json:"created_at"`
	UpdatedAt    time.Time    `json:"updated_at"`
}

type Trigger struct {
	Type string `json:"type"`           // "schedule" | "webhook" | "manual"
	Cron string `json:"cron,omitempty"`
	Tz   string `json:"tz,omitempty"`
	Path string `json:"path,omitempty"` // for webhook
}

type Connection struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type ErrorHandler struct {
	Notify string `json:"notify"` // "telegram" | "email" | "none"
	Retry  int    `json:"retry"`
}
