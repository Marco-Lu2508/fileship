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

// ShareLink mit erweiterten Feldern für Phase-1-Upgrade
type ShareLink struct {
	Token         string     `json:"token"`
	Path          string     `json:"path"`
	IsDir         bool       `json:"is_dir"`
	CreatedBy     int64      `json:"created_by"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	PasswordHash  string     `json:"-"` // nie an Client senden
	HasPassword   bool       `json:"has_password"`
	DownloadLimit int        `json:"download_limit"`
	DownloadCount int        `json:"download_count"`
	AllowUpload   bool       `json:"allow_upload"`
	AllowEdit     bool       `json:"allow_edit"`
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

// APIToken repräsentiert ein langlebiges API-Token
type APIToken struct {
	ID        int64      `json:"id"`
	UserID    int64      `json:"user_id"`
	Name      string     `json:"name"`
	Prefix    string     `json:"prefix"` // erste Zeichen für Anzeige, nie der volle Token
	LastUsed  *time.Time `json:"last_used,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	// PlainToken wird nur beim Erstellen befüllt, nie gespeichert
	PlainToken string `json:"token,omitempty"`
}

// Favorite repräsentiert eine gemerkte Datei/Ordner
type Favorite struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"user_id"`
	Path      string    `json:"path"`
	Name      string    `json:"name"`
	IsDir     bool      `json:"is_dir"`
	CreatedAt time.Time `json:"created_at"`
}

// PathPermission definiert eine pfad-basierte Zugriffskontrolle
type PathPermission struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"user_id"`
	Path      string `json:"path"`
	CanRead   bool   `json:"can_read"`
	CanWrite  bool   `json:"can_write"`
	CanDelete bool   `json:"can_delete"`
	CanShare  bool   `json:"can_share"`
}

// TrashItem repräsentiert ein gelöschtes Element im Papierkorb
type TrashItem struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"user_id"`
	OrigPath  string    `json:"orig_path"`
	TrashName string    `json:"trash_name"`
	Name      string    `json:"name"`
	IsDir     bool      `json:"is_dir"`
	Size      int64     `json:"size"`
	DeletedAt time.Time `json:"deleted_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// UserSource ist eine benannte Dateiquelle für einen User
type UserSource struct {
	ID          int64  `json:"id"`
	UserID      int64  `json:"user_id"`
	Name        string `json:"name"`
	RootPath    string `json:"root_path"`
	IsDefault   bool   `json:"is_default"`
	SortOrder   int    `json:"sort_order"`
}
