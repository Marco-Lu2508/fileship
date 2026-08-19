package db

import (
	"github.com/yourname/fileship/internal/model"
)

func (d *DB) migrateSources() error {
	_, err := d.Exec(`
		CREATE TABLE IF NOT EXISTS user_sources (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			name       TEXT    NOT NULL,
			root_path  TEXT    NOT NULL,
			is_default BOOLEAN NOT NULL DEFAULT 0,
			sort_order INTEGER NOT NULL DEFAULT 0,
			UNIQUE(user_id, name)
		);
		CREATE INDEX IF NOT EXISTS idx_sources_user ON user_sources(user_id);
	`)
	return err
}

// ListSources gibt alle Quellen eines Users zurück
func (d *DB) ListSources(userID int64) ([]model.UserSource, error) {
	rows, err := d.Query(
		`SELECT id, user_id, name, root_path, is_default, sort_order
		 FROM user_sources WHERE user_id = ? ORDER BY sort_order ASC, name ASC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sources []model.UserSource
	for rows.Next() {
		var s model.UserSource
		rows.Scan(&s.ID, &s.UserID, &s.Name, &s.RootPath, &s.IsDefault, &s.SortOrder)
		sources = append(sources, s)
	}
	return sources, nil
}

// GetDefaultSource gibt die Default-Quelle zurück; nil wenn keine existiert
func (d *DB) GetDefaultSource(userID int64) *model.UserSource {
	s := &model.UserSource{}
	err := d.QueryRow(
		`SELECT id, user_id, name, root_path, is_default, sort_order
		 FROM user_sources WHERE user_id = ? AND is_default = 1 LIMIT 1`,
		userID,
	).Scan(&s.ID, &s.UserID, &s.Name, &s.RootPath, &s.IsDefault, &s.SortOrder)
	if err != nil {
		return nil
	}
	return s
}

// GetSourceByID gibt eine Quelle per ID zurück (nur wenn sie dem User gehört)
func (d *DB) GetSourceByID(userID int64, sourceID int64) *model.UserSource {
	s := &model.UserSource{}
	err := d.QueryRow(
		`SELECT id, user_id, name, root_path, is_default, sort_order
		 FROM user_sources WHERE user_id = ? AND id = ?`,
		userID, sourceID,
	).Scan(&s.ID, &s.UserID, &s.Name, &s.RootPath, &s.IsDefault, &s.SortOrder)
	if err != nil {
		return nil
	}
	return s
}

// CreateSource erstellt eine neue Quelle
func (d *DB) CreateSource(userID int64, name, rootPath string, isDefault bool) (*model.UserSource, error) {
	// Wenn is_default, vorherige defaults deaktivieren
	if isDefault {
		d.Exec("UPDATE user_sources SET is_default = 0 WHERE user_id = ?", userID)
	}
	res, err := d.Exec(
		`INSERT INTO user_sources (user_id, name, root_path, is_default, sort_order)
		 VALUES (?,?,?,?, (SELECT COALESCE(MAX(sort_order),0)+1 FROM user_sources WHERE user_id = ?))`,
		userID, name, rootPath, isDefault, userID,
	)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &model.UserSource{ID: id, UserID: userID, Name: name, RootPath: rootPath, IsDefault: isDefault}, nil
}

// UpdateSource aktualisiert Name/Pfad/Default einer Quelle
func (d *DB) UpdateSource(userID int64, sourceID int64, name, rootPath string, isDefault bool) error {
	if isDefault {
		d.Exec("UPDATE user_sources SET is_default = 0 WHERE user_id = ?", userID)
	}
	_, err := d.Exec(
		`UPDATE user_sources SET name=?, root_path=?, is_default=? WHERE user_id=? AND id=?`,
		name, rootPath, isDefault, userID, sourceID,
	)
	return err
}

// DeleteSource löscht eine Quelle
func (d *DB) DeleteSource(userID int64, sourceID int64) error {
	_, err := d.Exec("DELETE FROM user_sources WHERE user_id = ? AND id = ?", userID, sourceID)
	return err
}
