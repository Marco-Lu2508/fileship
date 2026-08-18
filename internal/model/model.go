package model

import "time"

type User struct {
	ID           int64     `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	IsAdmin      bool      `json:"is_admin"`
	RootPath     string    `json:"root_path"`
	CreatedAt    time.Time `json:"created_at"`
}

type UserWithQuota struct {
	ID           int64     `json:"id"`
	Username     string    `json:"username"`
	IsAdmin      bool      `json:"is_admin"`
	RootPath     string    `json:"root_path"`
	QuotaBytes   int64     `json:"quota_bytes"`
	AllowedTypes string    `json:"allowed_types"`
	CreatedAt    time.Time `json:"created_at"`
	DiskUsage    int64     `json:"disk_usage,omitempty"`
}

type FileInfo struct {
	Name    string    `json:"name"`
	Path    string    `json:"path"`
	Size    int64     `json:"size"`
	IsDir   bool      `json:"is_dir"`
	ModTime time.Time `json:"mod_time"`
	MimeType string   `json:"mime_type,omitempty"`
}

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

type WSEvent struct {
	Type    string `json:"type"`
	Path    string `json:"path"`
	Payload any    `json:"payload,omitempty"`
}
