package device

import (
	"database/sql"
	"fmt"
	"time"
)

type Device struct {
	ID        string
	Name      string
	PublicKey string
	IP        string
	Secret    string
	LastSeen  time.Time
	Online    bool
	Passive   bool
}

func Create(db *sql.DB, d *Device) error {
	_, err := db.Exec(
		"INSERT INTO devices (id, name, public_key, ip, secret, passive) VALUES (?, ?, ?, ?, ?, ?)",
		d.ID, d.Name, d.PublicKey, d.IP, d.Secret, d.Passive,
	)
	if err != nil {
		return fmt.Errorf("insert device: %w", err)
	}
	return nil
}

func GetByID(db *sql.DB, id string) (*Device, error) {
	d := &Device{}
	var lastSeen sql.NullTime
	err := db.QueryRow(
		"SELECT id, name, public_key, ip, secret, last_seen, online, passive FROM devices WHERE id = ?", id,
	).Scan(&d.ID, &d.Name, &d.PublicKey, &d.IP, &d.Secret, &lastSeen, &d.Online, &d.Passive)
	if err != nil {
		return nil, fmt.Errorf("get device %s: %w", id, err)
	}
	if lastSeen.Valid {
		d.LastSeen = lastSeen.Time
	}
	return d, nil
}

func GetByPublicKey(db *sql.DB, pubkey string) (*Device, error) {
	d := &Device{}
	var lastSeen sql.NullTime
	err := db.QueryRow(
		"SELECT id, name, public_key, ip, secret, last_seen, online, passive FROM devices WHERE public_key = ?", pubkey,
	).Scan(&d.ID, &d.Name, &d.PublicKey, &d.IP, &d.Secret, &lastSeen, &d.Online, &d.Passive)
	if err != nil {
		return nil, fmt.Errorf("get device by pubkey: %w", err)
	}
	if lastSeen.Valid {
		d.LastSeen = lastSeen.Time
	}
	return d, nil
}

func List(db *sql.DB) ([]Device, error) {
	rows, err := db.Query("SELECT id, name, public_key, ip, secret, last_seen, online, passive FROM devices")
	if err != nil {
		return nil, fmt.Errorf("list devices: %w", err)
	}
	defer rows.Close()

	var devices []Device
	for rows.Next() {
		var d Device
		var lastSeen sql.NullTime
		if err := rows.Scan(&d.ID, &d.Name, &d.PublicKey, &d.IP, &d.Secret, &lastSeen, &d.Online, &d.Passive); err != nil {
			return nil, fmt.Errorf("scan device: %w", err)
		}
		if lastSeen.Valid {
			d.LastSeen = lastSeen.Time
		}
		devices = append(devices, d)
	}
	return devices, nil
}

func Delete(db *sql.DB, id string) error {
	result, err := db.Exec("DELETE FROM devices WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete device: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("device %s not found", id)
	}
	return nil
}

func UpdateHeartbeat(db *sql.DB, id string) error {
	_, err := db.Exec("UPDATE devices SET last_seen = datetime('now'), online = 1 WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("update heartbeat: %w", err)
	}
	return nil
}

func MarkOffline(db *sql.DB, timeout time.Duration) error {
	secs := int(timeout.Seconds())
	_, err := db.Exec(
		fmt.Sprintf("UPDATE devices SET online = 0 WHERE online = 1 AND last_seen < datetime('now', '-%d seconds')", secs),
	)
	if err != nil {
		return fmt.Errorf("mark offline: %w", err)
	}
	return nil
}
