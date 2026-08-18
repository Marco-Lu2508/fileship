package model

import "time"

type AuditEntry struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"user_id"`
	Username  string    `json:"username"`
	Action    string    `json:"action"`
	Path      string    `json:"path"`
	IP        string    `json:"ip"`
	CreatedAt time.Time `json:"created_at"`
}

type ShareLink struct {
	Token     string     `json:"token"`
	Path      string     `json:"path"`
	IsDir     bool       `json:"is_dir"`
	CreatedBy int64      `json:"created_by"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

type ListOptions struct {
	Path    string `json:"path"`
	Search  string `json:"search"`
	SortBy  string `json:"sort_by"`
	SortAsc bool   `json:"sort_asc"`
	Page    int    `json:"page"`
	PerPage int    `json:"per_page"`
}

type ListResult struct {
	Files   []FileInfo `json:"files"`
	Total   int        `json:"total"`
	Page    int        `json:"page"`
	PerPage int        `json:"per_page"`
}
