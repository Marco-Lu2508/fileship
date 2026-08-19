package db

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/yourname/fileship/internal/model"
)

// ErrTokenExpired wird zurückgegeben wenn ein API-Token abgelaufen ist
var ErrTokenExpired = errors.New("api token expired")

func (d *DB) migrateTokens() error {
	_, err := d.Exec(`
		CREATE TABLE IF NOT EXISTS api_tokens (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			name         TEXT    NOT NULL,
			token_hash   TEXT    NOT NULL UNIQUE,
			token_prefix TEXT    NOT NULL,
			last_used    DATETIME,
			expires_at   DATETIME,
			created_at   DATETIME NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_api_tokens_user ON api_tokens(user_id);
		CREATE INDEX IF NOT EXISTS idx_api_tokens_hash ON api_tokens(token_hash);
	`)
	return err
}

// CreateAPIToken erzeugt ein neues API-Token; gibt das Klartext-Token einmalig zurück
func (d *DB) CreateAPIToken(userID int64, name string, expiresAt *time.Time) (string, *model.APIToken, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	plain := "fsk_" + hex.EncodeToString(raw)
	prefix := plain[:12] + "…"
	hash := sha256Hex(plain)

	res, err := d.Exec(
		`INSERT INTO api_tokens (user_id, name, token_hash, token_prefix, expires_at, created_at)
		 VALUES (?,?,?,?,?,?)`,
		userID, name, hash, prefix, expiresAt, time.Now(),
	)
	if err != nil {
		return "", nil, err
	}
	id, _ := res.LastInsertId()

	t := &model.APIToken{
		ID:        id,
		UserID:    userID,
		Name:      name,
		Prefix:    prefix,
		ExpiresAt: expiresAt,
		CreatedAt: time.Now(),
	}
	return plain, t, nil
}

// ValidateAPIToken prüft ein API-Token und gibt userID + isAdmin zurück
func (d *DB) ValidateAPIToken(plain string) (userID int64, isAdmin bool, err error) {
	hash := sha256Hex(plain)
	var expiresAt *time.Time
	err = d.QueryRow(
		`SELECT at.user_id, u.is_admin, at.expires_at
		 FROM api_tokens at JOIN users u ON u.id = at.user_id
		 WHERE at.token_hash = ?`, hash,
	).Scan(&userID, &isAdmin, &expiresAt)
	if err != nil {
		return 0, false, err
	}
	if expiresAt != nil && time.Now().After(*expiresAt) {
		d.Exec("DELETE FROM api_tokens WHERE token_hash = ?", hash) //nolint
		return 0, false, ErrTokenExpired
	}
	go d.Exec("UPDATE api_tokens SET last_used = ? WHERE token_hash = ?", time.Now(), hash) //nolint
	return userID, isAdmin, nil
}

// ListAPITokens gibt alle Tokens eines Users zurück (ohne Hash)
func (d *DB) ListAPITokens(userID int64) ([]model.APIToken, error) {
	rows, err := d.Query(
		`SELECT id, user_id, name, token_prefix, last_used, expires_at, created_at
		 FROM api_tokens WHERE user_id = ? ORDER BY created_at DESC`, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tokens []model.APIToken
	for rows.Next() {
		var t model.APIToken
		rows.Scan(&t.ID, &t.UserID, &t.Name, &t.Prefix, &t.LastUsed, &t.ExpiresAt, &t.CreatedAt)
		tokens = append(tokens, t)
	}
	return tokens, nil
}

// DeleteAPIToken löscht ein Token (nur wenn es dem User gehört)
func (d *DB) DeleteAPIToken(id int64, userID int64) error {
	_, err := d.Exec("DELETE FROM api_tokens WHERE id = ? AND user_id = ?", id, userID)
	return err
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", h)
}
