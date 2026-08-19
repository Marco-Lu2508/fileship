package db

import (
	"time"

	"github.com/yourname/fileship/internal/model"
)

func (d *DB) migrateFavorites() error {
	_, err := d.Exec(`
		CREATE TABLE IF NOT EXISTS favorites (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			path       TEXT    NOT NULL,
			is_dir     BOOLEAN NOT NULL DEFAULT 0,
			name       TEXT    NOT NULL,
			created_at DATETIME NOT NULL,
			UNIQUE(user_id, path)
		);
		CREATE INDEX IF NOT EXISTS idx_favorites_user ON favorites(user_id);
	`)
	return err
}

func (d *DB) AddFavorite(userID int64, path, name string, isDir bool) error {
	_, err := d.Exec(
		`INSERT OR IGNORE INTO favorites (user_id, path, name, is_dir, created_at) VALUES (?,?,?,?,?)`,
		userID, path, name, isDir, time.Now(),
	)
	return err
}

func (d *DB) RemoveFavorite(userID int64, path string) error {
	_, err := d.Exec("DELETE FROM favorites WHERE user_id = ? AND path = ?", userID, path)
	return err
}

func (d *DB) ListFavorites(userID int64) ([]model.Favorite, error) {
	rows, err := d.Query(
		`SELECT id, user_id, path, name, is_dir, created_at
		 FROM favorites WHERE user_id = ? ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var favs []model.Favorite
	for rows.Next() {
		var f model.Favorite
		rows.Scan(&f.ID, &f.UserID, &f.Path, &f.Name, &f.IsDir, &f.CreatedAt)
		favs = append(favs, f)
	}
	return favs, nil
}

// IsFavoriteSet gibt ein Set aller Favoriten-Pfade für schnelles Lookup zurück
func (d *DB) FavoritePathSet(userID int64) map[string]bool {
	rows, err := d.Query("SELECT path FROM favorites WHERE user_id = ?", userID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	set := make(map[string]bool)
	for rows.Next() {
		var p string
		rows.Scan(&p)
		set[p] = true
	}
	return set
}
