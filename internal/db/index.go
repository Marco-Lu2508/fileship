package db

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/yourname/fileship/internal/model"
)

// migrateIndex erstellt die Such-Index-Tabelle und den Ordnergrößen-Cache
func (d *DB) migrateIndex() error {
	_, err := d.Exec(`
		CREATE TABLE IF NOT EXISTS file_index (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			root_path  TEXT    NOT NULL,
			rel_path   TEXT    NOT NULL,
			name       TEXT    NOT NULL,
			is_dir     BOOLEAN NOT NULL DEFAULT 0,
			size       INTEGER NOT NULL DEFAULT 0,
			mod_time   DATETIME NOT NULL,
			mime_type  TEXT    NOT NULL DEFAULT '',
			indexed_at DATETIME NOT NULL,
			UNIQUE(user_id, rel_path)
		);
		CREATE INDEX IF NOT EXISTS idx_file_index_user   ON file_index(user_id);
		CREATE INDEX IF NOT EXISTS idx_file_index_name   ON file_index(user_id, name);
		CREATE INDEX IF NOT EXISTS idx_file_index_dir    ON file_index(user_id, is_dir);

		CREATE TABLE IF NOT EXISTS dir_size_cache (
			root_path  TEXT PRIMARY KEY,
			size_bytes INTEGER NOT NULL DEFAULT 0,
			updated_at DATETIME NOT NULL
		);
	`)
	return err
}

// IndexSearch sucht im File-Index für einen User
func (d *DB) IndexSearch(userID int64, query string, limit int) ([]model.FileInfo, error) {
	if limit <= 0 {
		limit = 200
	}
	pattern := "%" + strings.ToLower(query) + "%"
	rows, err := d.Query(`
		SELECT rel_path, name, is_dir, size, mod_time, mime_type
		FROM file_index
		WHERE user_id = ? AND LOWER(name) LIKE ?
		ORDER BY is_dir DESC, name ASC
		LIMIT ?
	`, userID, pattern, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []model.FileInfo
	for rows.Next() {
		var f model.FileInfo
		var modTime string
		rows.Scan(&f.Path, &f.Name, &f.IsDir, &f.Size, &modTime, &f.MimeType)
		f.ModTime, _ = time.Parse(time.RFC3339, modTime)
		results = append(results, f)
	}
	return results, nil
}

// IndexUpsert fügt Einträge in den Index ein oder aktualisiert sie
func (d *DB) IndexUpsert(userID int64, rootPath string, files []model.FileInfo) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`
		INSERT INTO file_index (user_id, root_path, rel_path, name, is_dir, size, mod_time, mime_type, indexed_at)
		VALUES (?,?,?,?,?,?,?,?,?)
		ON CONFLICT(user_id, rel_path) DO UPDATE SET
			name=excluded.name, is_dir=excluded.is_dir, size=excluded.size,
			mod_time=excluded.mod_time, mime_type=excluded.mime_type, indexed_at=excluded.indexed_at
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	now := time.Now().UTC().Format(time.RFC3339)
	for _, f := range files {
		_, err = stmt.Exec(
			userID, rootPath, f.Path, f.Name, f.IsDir, f.Size,
			f.ModTime.UTC().Format(time.RFC3339), f.MimeType, now,
		)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

// IndexDelete entfernt einen Pfad (und alle Kinder) aus dem Index
func (d *DB) IndexDelete(userID int64, relPath string) {
	d.Exec(`DELETE FROM file_index WHERE user_id = ? AND (rel_path = ? OR rel_path LIKE ?)`,
		userID, relPath, relPath+"/%")
}

// IndexRename benennt Einträge im Index um
func (d *DB) IndexRename(userID int64, oldRel, newRel string) {
	d.Exec(`UPDATE file_index SET rel_path = replace(rel_path, ?, ?) WHERE user_id = ? AND (rel_path = ? OR rel_path LIKE ?)`,
		oldRel, newRel, userID, oldRel, oldRel+"/%")
	// name für root-Eintrag anpassen
	d.Exec(`UPDATE file_index SET name = ? WHERE user_id = ? AND rel_path = ?`,
		filepath.Base(newRel), userID, newRel)
}

// IndexCountForUser gibt die Anzahl indizierter Dateien zurück
func (d *DB) IndexCountForUser(userID int64) int64 {
	var n int64
	d.QueryRow("SELECT COUNT(*) FROM file_index WHERE user_id = ?", userID).Scan(&n)
	return n
}

// DirSizeCacheGet gibt gecachten Wert zurück; ok=false wenn kein Cache oder veraltet
func (d *DB) DirSizeCacheGet(rootPath string, maxAge time.Duration) (int64, bool) {
	var size int64
	var updatedAt time.Time
	err := d.QueryRow("SELECT size_bytes, updated_at FROM dir_size_cache WHERE root_path = ?", rootPath).
		Scan(&size, &updatedAt)
	if err != nil || time.Since(updatedAt) > maxAge {
		return 0, false
	}
	return size, true
}

// DirSizeCacheSet speichert die Ordnergröße
func (d *DB) DirSizeCacheSet(rootPath string, size int64) {
	d.Exec(`INSERT INTO dir_size_cache (root_path, size_bytes, updated_at) VALUES (?,?,?)
		ON CONFLICT(root_path) DO UPDATE SET size_bytes=excluded.size_bytes, updated_at=excluded.updated_at`,
		rootPath, size, time.Now())
}

// --- Indexer ---

// Indexer verwaltet den Hintergrund-Index-Worker
type Indexer struct {
	db      *DB
	mu      sync.Mutex
	running map[int64]bool
}

func NewIndexer(db *DB) *Indexer {
	return &Indexer{db: db, running: make(map[int64]bool)}
}

// TriggerReindex startet einen Neu-Index im Hintergrund (nicht-blockierend, idempotent)
func (ix *Indexer) TriggerReindex(userID int64, rootPath string) {
	ix.mu.Lock()
	if ix.running[userID] {
		ix.mu.Unlock()
		return
	}
	ix.running[userID] = true
	ix.mu.Unlock()

	go func() {
		defer func() {
			ix.mu.Lock()
			ix.running[userID] = false
			ix.mu.Unlock()
		}()
		_ = walkAndIndex(ix.db, userID, rootPath)
	}()
}

func walkAndIndex(d *DB, userID int64, rootPath string) error {
	rootAbs, err := filepath.Abs(rootPath)
	if err != nil {
		return err
	}

	const batchSize = 500
	var batch []model.FileInfo

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		err := d.IndexUpsert(userID, rootAbs, batch)
		batch = batch[:0]
		return err
	}

	err = filepath.WalkDir(rootAbs, func(path string, e os.DirEntry, err error) error {
		if err != nil {
			return nil // Fehler ignorieren, weitermachen
		}
		rel, _ := filepath.Rel(rootAbs, path)
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		// Hidden files überspringen
		if strings.HasPrefix(filepath.Base(path), ".") {
			if e.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		info, err := e.Info()
		if err != nil {
			return nil
		}

		fi := model.FileInfo{
			Name:    e.Name(),
			Path:    rel,
			IsDir:   e.IsDir(),
			Size:    info.Size(),
			ModTime: info.ModTime(),
		}
		if !e.IsDir() {
			fi.MimeType = mimeTypeFromName(e.Name())
		}
		batch = append(batch, fi)
		if len(batch) >= batchSize {
			return flush()
		}
		return nil
	})
	if err != nil {
		return err
	}
	return flush()
}

func mimeTypeFromName(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".svg":
		return "image/svg+xml"
	case ".mp4", ".m4v":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".mov":
		return "video/quicktime"
	case ".mp3":
		return "audio/mpeg"
	case ".ogg", ".oga":
		return "audio/ogg"
	case ".wav":
		return "audio/wav"
	case ".flac":
		return "audio/flac"
	case ".pdf":
		return "application/pdf"
	case ".zip":
		return "application/zip"
	case ".tar", ".gz", ".tgz":
		return "application/x-tar"
	case ".json":
		return "application/json"
	case ".md":
		return "text/markdown"
	case ".html", ".htm":
		return "text/html"
	case ".css":
		return "text/css"
	case ".js", ".mjs":
		return "text/javascript"
	case ".ts":
		return "text/typescript"
	case ".go":
		return "text/x-go"
	case ".py":
		return "text/x-python"
	case ".sh", ".bash":
		return "text/x-shellscript"
	case ".yaml", ".yml":
		return "text/yaml"
	case ".toml":
		return "text/toml"
	case ".xml":
		return "text/xml"
	case ".csv":
		return "text/csv"
	case ".txt", ".log", ".ini", ".conf":
		return "text/plain"
	}
	return "application/octet-stream"
}
