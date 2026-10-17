package db

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/yourname/fileship/internal/model"
)

func (d *DB) migrateAgents() error {
	_, err := d.Exec(`
		CREATE TABLE IF NOT EXISTS agent_devices (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			name         TEXT    NOT NULL,
			token_hash   TEXT    NOT NULL UNIQUE,
			token_prefix TEXT    NOT NULL,
			root_path    TEXT    NOT NULL DEFAULT '/',
			hostname     TEXT    NOT NULL DEFAULT '',
			os_info      TEXT    NOT NULL DEFAULT '',
			disk_usage   INTEGER NOT NULL DEFAULT 0,
			last_seen    DATETIME,
			created_at   DATETIME NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_agent_devices_user ON agent_devices(user_id);
	`)
	return err
}

func hashAgentToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// CreateAgentDevice registriert einen neuen Agenten für einen User und gibt einmalig das PlainToken zurück.
func (d *DB) CreateAgentDevice(userID int64, name, rootPath string) (*model.AgentDevice, error) {
	if rootPath == "" {
		rootPath = "/"
	}
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	plain := "fsa_" + hex.EncodeToString(b)
	prefix := plain[:12]
	hash := hashAgentToken(plain)
	now := time.Now()

	res, err := d.Exec(
		`INSERT INTO agent_devices (user_id, name, token_hash, token_prefix, root_path, created_at)
		 VALUES (?,?,?,?,?,?)`,
		userID, name, hash, prefix, rootPath, now,
	)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &model.AgentDevice{
		ID:         id,
		UserID:     userID,
		Name:       name,
		Prefix:     prefix,
		RootPath:   rootPath,
		CreatedAt:  now,
		PlainToken: plain,
	}, nil
}

// ListAgentDevices gibt alle registrierten Agent-Geräte eines Users zurück.
func (d *DB) ListAgentDevices(userID int64) ([]model.AgentDevice, error) {
	rows, err := d.Query(
		`SELECT id, user_id, name, token_prefix, root_path, hostname, os_info, disk_usage, last_seen, created_at
		 FROM agent_devices WHERE user_id = ? ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []model.AgentDevice
	for rows.Next() {
		var dev model.AgentDevice
		rows.Scan(&dev.ID, &dev.UserID, &dev.Name, &dev.Prefix, &dev.RootPath, &dev.Hostname, &dev.OSInfo, &dev.DiskUsage, &dev.LastSeen, &dev.CreatedAt)
		list = append(list, dev)
	}
	return list, nil
}

// GetAgentDeviceByID lädt ein Agent-Gerät eines Users.
func (d *DB) GetAgentDeviceByID(userID, agentID int64) (*model.AgentDevice, error) {
	dev := &model.AgentDevice{}
	err := d.QueryRow(
		`SELECT id, user_id, name, token_prefix, root_path, hostname, os_info, disk_usage, last_seen, created_at
		 FROM agent_devices WHERE user_id = ? AND id = ?`,
		userID, agentID,
	).Scan(&dev.ID, &dev.UserID, &dev.Name, &dev.Prefix, &dev.RootPath, &dev.Hostname, &dev.OSInfo, &dev.DiskUsage, &dev.LastSeen, &dev.CreatedAt)
	if err != nil {
		return nil, err
	}
	return dev, nil
}

// DeleteAgentDevice löscht ein Agent-Gerät eines Users.
func (d *DB) DeleteAgentDevice(userID, agentID int64) error {
	_, err := d.Exec(`DELETE FROM agent_devices WHERE user_id = ? AND id = ?`, userID, agentID)
	return err
}

// ValidateAgentToken prüft ein fsa_... Token und gibt das zugehörige AgentDevice zurück.
func (d *DB) ValidateAgentToken(raw string) (*model.AgentDevice, error) {
	hash := hashAgentToken(raw)
	dev := &model.AgentDevice{}
	err := d.QueryRow(
		`SELECT id, user_id, name, token_prefix, root_path, hostname, os_info, disk_usage, last_seen, created_at
		 FROM agent_devices WHERE token_hash = ?`,
		hash,
	).Scan(&dev.ID, &dev.UserID, &dev.Name, &dev.Prefix, &dev.RootPath, &dev.Hostname, &dev.OSInfo, &dev.DiskUsage, &dev.LastSeen, &dev.CreatedAt)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	dev.LastSeen = &now
	d.Exec(`UPDATE agent_devices SET last_seen = ? WHERE id = ?`, now, dev.ID)
	return dev, nil
}

// UpdateAgentHeartbeat speichert Hostname, OS-Info, Root-Pfad und Speicherbelegung eines verbundenen Agenten.
func (d *DB) UpdateAgentHeartbeat(agentID int64, hostname, osInfo, rootPath string, diskUsage int64) {
	now := time.Now()
	d.Exec(
		`UPDATE agent_devices SET hostname = ?, os_info = ?, root_path = ?, disk_usage = ?, last_seen = ? WHERE id = ?`,
		hostname, osInfo, rootPath, diskUsage, now, agentID,
	)
}
