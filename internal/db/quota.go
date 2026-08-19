package db

import (
	"strings"

	"github.com/yourname/fileship/internal/model"
)

func isDuplicateColumnError(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "duplicate column name")
}

func (d *DB) migrateQuotas() error {
	for _, statement := range []string{
		"ALTER TABLE users ADD COLUMN quota_bytes INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE users ADD COLUMN allowed_types TEXT NOT NULL DEFAULT ''",
	} {
		if _, err := d.Exec(statement); err != nil && !isDuplicateColumnError(err) {
			return err
		}
	}
	return nil
}

func (d *DB) SetUserQuota(id, quotaBytes int64) error {
	_, err := d.Exec("UPDATE users SET quota_bytes = ? WHERE id = ?", quotaBytes, id)
	return err
}

func (d *DB) SetUserAllowedTypes(id int64, types string) error {
	_, err := d.Exec("UPDATE users SET allowed_types = ? WHERE id = ?", types, id)
	return err
}

func (d *DB) GetUserQuotaAndTypes(id int64) (quotaBytes int64, allowedTypes string, err error) {
	err = d.QueryRow("SELECT quota_bytes, allowed_types FROM users WHERE id = ?", id).
		Scan(&quotaBytes, &allowedTypes)
	return
}

func (d *DB) ListUsersWithQuota() ([]model.UserWithQuota, error) {
	rows, err := d.Query(`
		SELECT id, username, is_admin, root_path, quota_bytes, allowed_types, created_at
		FROM users
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []model.UserWithQuota
	for rows.Next() {
		var u model.UserWithQuota
		rows.Scan(&u.ID, &u.Username, &u.IsAdmin, &u.RootPath, &u.QuotaBytes, &u.AllowedTypes, &u.CreatedAt)
		users = append(users, u)
	}
	return users, nil
}
