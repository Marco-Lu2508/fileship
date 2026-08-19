package db

import (
	"path/filepath"
	"strings"

	"github.com/yourname/fileship/internal/model"
)

func (d *DB) migratePermissions() error {
	_, err := d.Exec(`
		CREATE TABLE IF NOT EXISTS path_permissions (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			path       TEXT    NOT NULL,
			can_read   BOOLEAN NOT NULL DEFAULT 1,
			can_write  BOOLEAN NOT NULL DEFAULT 1,
			can_delete BOOLEAN NOT NULL DEFAULT 1,
			can_share  BOOLEAN NOT NULL DEFAULT 1,
			UNIQUE(user_id, path)
		);
		CREATE INDEX IF NOT EXISTS idx_perms_user ON path_permissions(user_id);
	`)
	return err
}

// SetPathPermission erstellt oder überschreibt eine Pfad-Permission
func (d *DB) SetPathPermission(p model.PathPermission) error {
	_, err := d.Exec(
		`INSERT INTO path_permissions (user_id, path, can_read, can_write, can_delete, can_share)
		 VALUES (?,?,?,?,?,?)
		 ON CONFLICT(user_id, path) DO UPDATE SET
		   can_read=excluded.can_read, can_write=excluded.can_write,
		   can_delete=excluded.can_delete, can_share=excluded.can_share`,
		p.UserID, p.Path, p.CanRead, p.CanWrite, p.CanDelete, p.CanShare,
	)
	return err
}

// DeletePathPermission entfernt eine Pfad-Permission
func (d *DB) DeletePathPermission(id int64) error {
	_, err := d.Exec("DELETE FROM path_permissions WHERE id = ?", id)
	return err
}

// ListPathPermissions gibt alle Regeln eines Users zurück
func (d *DB) ListPathPermissions(userID int64) ([]model.PathPermission, error) {
	rows, err := d.Query(
		`SELECT id, user_id, path, can_read, can_write, can_delete, can_share
		 FROM path_permissions WHERE user_id = ? ORDER BY path ASC`, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var perms []model.PathPermission
	for rows.Next() {
		var p model.PathPermission
		rows.Scan(&p.ID, &p.UserID, &p.Path, &p.CanRead, &p.CanWrite, &p.CanDelete, &p.CanShare)
		perms = append(perms, p)
	}
	return perms, nil
}

// CheckPermission prüft ob eine Aktion auf einem Pfad erlaubt ist.
// Gibt (true, nil) wenn keine Regeln = alles erlaubt.
// Längste Präfix-Übereinstimmung gewinnt.
func (d *DB) CheckPermission(userID int64, relPath string, action string) (bool, error) {
	perms, err := d.ListPathPermissions(userID)
	if err != nil || len(perms) == 0 {
		return true, nil // keine Regeln = alles erlaubt
	}

	relPath = filepath.ToSlash(relPath)
	var best *model.PathPermission
	bestLen := -1

	for i := range perms {
		rule := filepath.ToSlash(perms[i].Path)
		// Exakter Match oder Präfix-Match
		if relPath == rule || strings.HasPrefix(relPath, rule+"/") || strings.HasPrefix(relPath, rule) {
			if len(rule) > bestLen {
				bestLen = len(rule)
				best = &perms[i]
			}
		}
	}

	if best == nil {
		return true, nil // kein Treffer = erlaubt
	}

	switch action {
	case "read":
		return best.CanRead, nil
	case "write":
		return best.CanWrite, nil
	case "delete":
		return best.CanDelete, nil
	case "share":
		return best.CanShare, nil
	}
	return true, nil
}
