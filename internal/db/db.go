package db

import (
	"database/sql"
	"time"

	"github.com/yourname/fileship/internal/model"
	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/bcrypt"
)

type DB struct{ *sql.DB }

func Open(path string) (*DB, error) {
	conn, err := sql.Open("sqlite3", path+"?_journal=WAL&_foreign_keys=on")
	if err != nil {
		return nil, err
	}
	d := &DB{conn}
	return d, d.migrate()
}

func (d *DB) migrate() error {
	if err := d.migrateExtra(); err != nil {
		return err
	}
	_, err := d.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			username      TEXT    UNIQUE NOT NULL,
			password_hash TEXT    NOT NULL,
			is_admin      BOOLEAN NOT NULL DEFAULT 0,
			root_path     TEXT    NOT NULL DEFAULT '/',
			created_at    DATETIME NOT NULL
		);
		CREATE TABLE IF NOT EXISTS refresh_tokens (
			token      TEXT PRIMARY KEY,
			user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			expires_at DATETIME NOT NULL
		);
	`)
	return err
}

func (d *DB) CreateAdminIfNotExists(username, password, rootPath string) error {
	var count int
	d.QueryRow("SELECT COUNT(*) FROM users WHERE is_admin = 1").Scan(&count)
	if count > 0 {
		return nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = d.Exec(
		"INSERT INTO users (username, password_hash, is_admin, root_path, created_at) VALUES (?,?,1,?,?)",
		username, string(hash), rootPath, time.Now(),
	)
	return err
}

func (d *DB) GetUserByUsername(username string) (*model.User, error) {
	u := &model.User{}
	err := d.QueryRow(
		"SELECT id, username, password_hash, is_admin, root_path, created_at FROM users WHERE username = ?",
		username,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.IsAdmin, &u.RootPath, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (d *DB) GetUserByID(id int64) (*model.User, error) {
	u := &model.User{}
	err := d.QueryRow(
		"SELECT id, username, password_hash, is_admin, root_path, created_at FROM users WHERE id = ?",
		id,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.IsAdmin, &u.RootPath, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (d *DB) ListUsers() ([]model.User, error) {
	rows, err := d.Query("SELECT id, username, is_admin, root_path, created_at FROM users")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []model.User
	for rows.Next() {
		var u model.User
		rows.Scan(&u.ID, &u.Username, &u.IsAdmin, &u.RootPath, &u.CreatedAt)
		users = append(users, u)
	}
	return users, nil
}

func (d *DB) CreateUser(username, password string, isAdmin bool, rootPath string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = d.Exec(
		"INSERT INTO users (username, password_hash, is_admin, root_path, created_at) VALUES (?,?,?,?,?)",
		username, string(hash), isAdmin, rootPath, time.Now(),
	)
	return err
}

func (d *DB) DeleteUser(id int64) error {
	_, err := d.Exec("DELETE FROM users WHERE id = ?", id)
	return err
}

func (d *DB) SaveRefreshToken(token string, userID int64, expires time.Time) error {
	_, err := d.Exec("INSERT INTO refresh_tokens (token, user_id, expires_at) VALUES (?,?,?)", token, userID, expires)
	return err
}

func (d *DB) GetRefreshToken(token string) (int64, error) {
	var userID int64
	var expires time.Time
	err := d.QueryRow("SELECT user_id, expires_at FROM refresh_tokens WHERE token = ?", token).Scan(&userID, &expires)
	if err != nil {
		return 0, err
	}
	if time.Now().After(expires) {
		d.Exec("DELETE FROM refresh_tokens WHERE token = ?", token)
		return 0, sql.ErrNoRows
	}
	return userID, nil
}

func (d *DB) DeleteRefreshToken(token string) error {
	_, err := d.Exec("DELETE FROM refresh_tokens WHERE token = ?", token)
	return err
}
