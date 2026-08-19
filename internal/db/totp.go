package db

import (
	"strings"
	"time"
)

func (d *DB) migrateTOTP() error {
	_, err := d.Exec(`
		CREATE TABLE IF NOT EXISTS totp_secrets (
			user_id      INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
			secret       TEXT    NOT NULL,
			enabled      BOOLEAN NOT NULL DEFAULT 0,
			backup_codes TEXT    NOT NULL DEFAULT '',
			created_at   DATETIME NOT NULL
		);
	`)
	return err
}

// TOTPGetSecret gibt Secret + enabled-Status zurück
func (d *DB) TOTPGetSecret(userID int64) (secret string, enabled bool, err error) {
	err = d.QueryRow(
		"SELECT secret, enabled FROM totp_secrets WHERE user_id = ?", userID,
	).Scan(&secret, &enabled)
	return
}

// TOTPSetSecret speichert ein noch nicht aktiviertes Secret
func (d *DB) TOTPSetSecret(userID int64, secret string) error {
	_, err := d.Exec(
		`INSERT INTO totp_secrets (user_id, secret, enabled, backup_codes, created_at) VALUES (?,?,0,'',?)
		 ON CONFLICT(user_id) DO UPDATE SET secret=excluded.secret, enabled=0, backup_codes=''`,
		userID, secret, time.Now(),
	)
	return err
}

// TOTPEnable aktiviert 2FA und speichert Backup-Codes (kommasepariert)
func (d *DB) TOTPEnable(userID int64, backupCodes []string) error {
	_, err := d.Exec(
		`UPDATE totp_secrets SET enabled=1, backup_codes=? WHERE user_id=?`,
		strings.Join(backupCodes, ","), userID,
	)
	return err
}

// TOTPDisable deaktiviert 2FA
func (d *DB) TOTPDisable(userID int64) error {
	_, err := d.Exec("DELETE FROM totp_secrets WHERE user_id = ?", userID)
	return err
}

// TOTPUseBackupCode prüft und verbraucht einen Backup-Code
func (d *DB) TOTPUseBackupCode(userID int64, code string) bool {
	var stored string
	err := d.QueryRow("SELECT backup_codes FROM totp_secrets WHERE user_id=? AND enabled=1", userID).Scan(&stored)
	if err != nil || stored == "" {
		return false
	}
	codes := strings.Split(stored, ",")
	remaining := make([]string, 0, len(codes))
	found := false
	for _, c := range codes {
		if strings.TrimSpace(c) == strings.TrimSpace(code) {
			found = true
		} else {
			remaining = append(remaining, c)
		}
	}
	if found {
		d.Exec("UPDATE totp_secrets SET backup_codes=? WHERE user_id=?", strings.Join(remaining, ","), userID)
	}
	return found
}

// TOTPIsEnabled gibt zurück ob 2FA für einen User aktiviert ist
func (d *DB) TOTPIsEnabled(userID int64) bool {
	var enabled bool
	d.QueryRow("SELECT enabled FROM totp_secrets WHERE user_id = ?", userID).Scan(&enabled)
	return enabled
}
