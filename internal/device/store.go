package device

import (
	"database/sql"
	"fmt"
	"time"
)

type Device struct {
	ID       string
	Name     string
	IP       string
	Secret   string
	LastSeen time.Time
	Online   bool
}

func Create(db *sql.DB, d *Device) error {
	_, err := db.Exec("INSERT INTO devices (id, name, ip, secret) VALUES (?, ?, ?, ?)",
		d.ID, d.Name, d.IP, d.Secret)
	return err
}

func GetByID(db *sql.DB, id string) (*Device, error) {
	d := &Device{}
	var lastSeen sql.NullTime
	err := db.QueryRow("SELECT id, name, ip, secret, last_seen, online FROM devices WHERE id = ?", id).
		Scan(&d.ID, &d.Name, &d.IP, &d.Secret, &lastSeen, &d.Online)
	if err != nil {
		return nil, fmt.Errorf("device %s: %w", id, err)
	}
	if lastSeen.Valid {
		d.LastSeen = lastSeen.Time
	}
	return d, nil
}

func GetBySecret(db *sql.DB, secret string) (*Device, error) {
	d := &Device{}
	var lastSeen sql.NullTime
	err := db.QueryRow("SELECT id, name, ip, secret, last_seen, online FROM devices WHERE secret = ?", secret).
		Scan(&d.ID, &d.Name, &d.IP, &d.Secret, &lastSeen, &d.Online)
	if err != nil {
		return nil, fmt.Errorf("device by secret: %w", err)
	}
	if lastSeen.Valid {
		d.LastSeen = lastSeen.Time
	}
	return d, nil
}

func List(db *sql.DB) ([]Device, error) {
	rows, err := db.Query("SELECT id, name, ip, secret, last_seen, online FROM devices")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var devs []Device
	for rows.Next() {
		var d Device
		var lastSeen sql.NullTime
		if err := rows.Scan(&d.ID, &d.Name, &d.IP, &d.Secret, &lastSeen, &d.Online); err != nil {
			return nil, err
		}
		if lastSeen.Valid {
			d.LastSeen = lastSeen.Time
		}
		devs = append(devs, d)
	}
	return devs, nil
}

func Delete(db *sql.DB, id string) error {
	res, err := db.Exec("DELETE FROM devices WHERE id = ?", id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("device %s not found", id)
	}
	return nil
}

func UpdateOnline(db *sql.DB, id string, online bool) error {
	_, err := db.Exec("UPDATE devices SET online = ?, last_seen = datetime('now') WHERE id = ?", online, id)
	return err
}
