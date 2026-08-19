package db

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/yourname/fileship/internal/model"
)

func (d *DB) migrateExtra() error {
	_, err := d.Exec(`
		CREATE TABLE IF NOT EXISTS audit_log (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id    INTEGER NOT NULL,
			username   TEXT    NOT NULL,
			action     TEXT    NOT NULL,
			path       TEXT    NOT NULL,
			ip         TEXT    NOT NULL,
			created_at DATETIME NOT NULL
		);
		CREATE TABLE IF NOT EXISTS share_links (
			token          TEXT PRIMARY KEY,
			path           TEXT    NOT NULL,
			is_dir         BOOLEAN NOT NULL DEFAULT 0,
			created_by     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			expires_at     DATETIME,
			created_at     DATETIME NOT NULL,
			password_hash  TEXT    NOT NULL DEFAULT '',
			download_limit INTEGER NOT NULL DEFAULT 0,
			download_count INTEGER NOT NULL DEFAULT 0,
			allow_upload   BOOLEAN NOT NULL DEFAULT 0,
			allow_edit     BOOLEAN NOT NULL DEFAULT 0
		);
	`)
	if err != nil {
		return err
	}
	// Migration für bereits existierende share_links-Tabellen
	for _, stmt := range []string{
		"ALTER TABLE share_links ADD COLUMN password_hash  TEXT    NOT NULL DEFAULT ''",
		"ALTER TABLE share_links ADD COLUMN download_limit INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE share_links ADD COLUMN download_count INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE share_links ADD COLUMN allow_upload   BOOLEAN NOT NULL DEFAULT 0",
		"ALTER TABLE share_links ADD COLUMN allow_edit     BOOLEAN NOT NULL DEFAULT 0",
	} {
		if _, err2 := d.Exec(stmt); err2 != nil && !isDuplicateColumnError(err2) {
			return err2
		}
	}
	return nil
}

// Audit Log

func (d *DB) LogAction(userID int64, username, action, path, ip string) {
	d.Exec(
		"INSERT INTO audit_log (user_id, username, action, path, ip, created_at) VALUES (?,?,?,?,?,?)",
		userID, username, action, path, ip, time.Now(),
	)
}

func (d *DB) GetAuditLog(limit int) ([]model.AuditEntry, error) {
	rows, err := d.Query(
		"SELECT id, user_id, username, action, path, ip, created_at FROM audit_log ORDER BY created_at DESC LIMIT ?",
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []model.AuditEntry
	for rows.Next() {
		var e model.AuditEntry
		rows.Scan(&e.ID, &e.UserID, &e.Username, &e.Action, &e.Path, &e.IP, &e.CreatedAt)
		entries = append(entries, e)
	}
	return entries, nil
}

// Share Links

func (d *DB) CreateShareLink(path string, isDir bool, createdBy int64, expiresAt *time.Time, passwordHash string, downloadLimit int, allowUpload bool, allowEdit bool) (string, error) {
	b := make([]byte, 16)
	rand.Read(b)
	token := hex.EncodeToString(b)
	_, err := d.Exec(
		"INSERT INTO share_links (token, path, is_dir, created_by, expires_at, created_at, password_hash, download_limit, download_count, allow_upload, allow_edit) VALUES (?,?,?,?,?,?,?,?,0,?,?)",
		token, path, isDir, createdBy, expiresAt, time.Now(), passwordHash, downloadLimit, allowUpload, allowEdit,
	)
	return token, err
}

func (d *DB) GetShareLink(token string) (*model.ShareLink, error) {
	s := &model.ShareLink{}
	err := d.QueryRow(
		"SELECT token, path, is_dir, created_by, expires_at, created_at, password_hash, download_limit, download_count, allow_upload, allow_edit FROM share_links WHERE token = ?",
		token,
	).Scan(&s.Token, &s.Path, &s.IsDir, &s.CreatedBy, &s.ExpiresAt, &s.CreatedAt, &s.PasswordHash, &s.DownloadLimit, &s.DownloadCount, &s.AllowUpload, &s.AllowEdit)
	if err != nil {
		return nil, err
	}
	if s.ExpiresAt != nil && time.Now().After(*s.ExpiresAt) {
		d.Exec("DELETE FROM share_links WHERE token = ?", token)
		return nil, nil
	}
	if s.DownloadLimit > 0 && s.DownloadCount >= s.DownloadLimit {
		return nil, nil // limit erreicht
	}
	return s, nil
}

// IncrementShareDownload erhöht den Download-Counter
func (d *DB) IncrementShareDownload(token string) {
	d.Exec("UPDATE share_links SET download_count = download_count + 1 WHERE token = ?", token)
}

func (d *DB) ListShareLinks(userID int64) ([]model.ShareLink, error) {
	rows, err := d.Query(
		"SELECT token, path, is_dir, created_by, expires_at, created_at, password_hash, download_limit, download_count, allow_upload, allow_edit FROM share_links WHERE created_by = ? ORDER BY created_at DESC",
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var links []model.ShareLink
	for rows.Next() {
		var s model.ShareLink
		rows.Scan(&s.Token, &s.Path, &s.IsDir, &s.CreatedBy, &s.ExpiresAt, &s.CreatedAt, &s.PasswordHash, &s.DownloadLimit, &s.DownloadCount, &s.AllowUpload, &s.AllowEdit)
		links = append(links, s)
	}
	return links, nil
}

func (d *DB) DeleteShareLink(token string, userID int64) error {
	_, err := d.Exec("DELETE FROM share_links WHERE token = ? AND created_by = ?", token, userID)
	return err
}

// User Update

func (d *DB) UpdateUserPassword(id int64, hash string) error {
	_, err := d.Exec("UPDATE users SET password_hash = ? WHERE id = ?", hash, id)
	return err
}

func (d *DB) UpdateUserRootPath(id int64, rootPath string) error {
	_, err := d.Exec("UPDATE users SET root_path = ? WHERE id = ?", rootPath, id)
	return err
}

func (d *DB) GetStorageUsage(rootPath string) (int64, error) {
	var total int64
	rows, err := d.Query("SELECT 0") // placeholder — berechnet im fs package
	if err != nil {
		return 0, err
	}
	rows.Close()
	return total, nil
}
