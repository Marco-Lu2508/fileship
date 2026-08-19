package db

import (
	"path/filepath"
	"time"

	"github.com/yourname/fileship/internal/model"
)

func (d *DB) migrateTrash() error {
	_, err := d.Exec(`
		CREATE TABLE IF NOT EXISTS trash (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			orig_path    TEXT    NOT NULL,
			trash_name   TEXT    NOT NULL UNIQUE,
			name         TEXT    NOT NULL,
			is_dir       BOOLEAN NOT NULL DEFAULT 0,
			size         INTEGER NOT NULL DEFAULT 0,
			deleted_at   DATETIME NOT NULL,
			expires_at   DATETIME NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_trash_user    ON trash(user_id);
		CREATE INDEX IF NOT EXISTS idx_trash_expires ON trash(expires_at);
	`)
	return err
}

// TrashPut registriert ein Element im Papierkorb
func (d *DB) TrashPut(userID int64, origPath, trashName, name string, isDir bool, size int64, retentionDays int) error {
	now := time.Now()
	expires := now.AddDate(0, 0, retentionDays)
	_, err := d.Exec(
		`INSERT INTO trash (user_id, orig_path, trash_name, name, is_dir, size, deleted_at, expires_at)
		 VALUES (?,?,?,?,?,?,?,?)`,
		userID, origPath, trashName, name, isDir, size, now, expires,
	)
	return err
}

// TrashList gibt alle Papierkorb-Einträge eines Users zurück
func (d *DB) TrashList(userID int64) ([]model.TrashItem, error) {
	rows, err := d.Query(
		`SELECT id, user_id, orig_path, trash_name, name, is_dir, size, deleted_at, expires_at
		 FROM trash WHERE user_id = ? ORDER BY deleted_at DESC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []model.TrashItem
	for rows.Next() {
		var t model.TrashItem
		rows.Scan(&t.ID, &t.UserID, &t.OrigPath, &t.TrashName, &t.Name, &t.IsDir, &t.Size, &t.DeletedAt, &t.ExpiresAt)
		items = append(items, t)
	}
	return items, nil
}

// TrashGet gibt einen einzelnen Papierkorb-Eintrag zurück
func (d *DB) TrashGet(userID int64, trashName string) (*model.TrashItem, error) {
	t := &model.TrashItem{}
	err := d.QueryRow(
		`SELECT id, user_id, orig_path, trash_name, name, is_dir, size, deleted_at, expires_at
		 FROM trash WHERE user_id = ? AND trash_name = ?`,
		userID, trashName,
	).Scan(&t.ID, &t.UserID, &t.OrigPath, &t.TrashName, &t.Name, &t.IsDir, &t.Size, &t.DeletedAt, &t.ExpiresAt)
	if err != nil {
		return nil, err
	}
	return t, nil
}

// TrashDelete entfernt einen Eintrag aus der DB
func (d *DB) TrashDelete(userID int64, trashName string) error {
	_, err := d.Exec("DELETE FROM trash WHERE user_id = ? AND trash_name = ?", userID, trashName)
	return err
}

// TrashPurgeExpired löscht alle abgelaufenen Einträge und gibt ihre trash_names zurück
func (d *DB) TrashPurgeExpired(userID int64) ([]string, error) {
	rows, err := d.Query(
		`SELECT trash_name FROM trash WHERE user_id = ? AND expires_at < ?`,
		userID, time.Now(),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		rows.Scan(&n)
		names = append(names, n)
	}
	if len(names) > 0 {
		d.Exec(`DELETE FROM trash WHERE user_id = ? AND expires_at < ?`, userID, time.Now())
	}
	return names, nil
}

// trashDir gibt das Papierkorb-Verzeichnis für einen User zurück
func TrashDir(rootPath string) string {
	return filepath.Join(filepath.Dir(rootPath), ".fileship_trash")
}
