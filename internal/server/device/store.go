package device

import (
	"database/sql"
	"fmt"
	"strings"
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

// isUniqueConflict 判断 err 是否为 SQLite UNIQUE 约束冲突。用消息匹配而非
// 类型断言，避免把 sqlite 驱动从 indirect 提升为 direct 依赖。
func isUniqueConflict(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// isUniqueConflictOn 判断 err 是否为指定列（如 "devices.ip"）的 UNIQUE 冲突。
// SQLite 的错误消息形如 "UNIQUE constraint failed: devices.ip"，带列名，
// 据此可区分冲突来源。
func isUniqueConflictOn(err error, column string) bool {
	return isUniqueConflict(err) && strings.Contains(err.Error(), column)
}

// AllocateAndCreate 分配一个空闲 IP 并创建设备，仅 IP 冲突时重试。
//
// 修复 TOCTOU：Allocate 读 used 集合与 Create 插入之间存在窗口，并发注册
// 可能选到同一 IP。devices.ip 有 UNIQUE 约束，冲突的那次 Create 会失败；
// 此时重新 Allocate（此时对方已提交，used 集合已更新）再试，直到成功或
// 用尽重试次数。
//
// 注意：只对 devices.ip 冲突重试。其它 UNIQUE 冲突（如重复的 devices.id）
// 不是 IP 竞争，重试也无济于事，直接返回原始错误，避免用误导性的
// "allocate unique IP" 掩盖真实的 id 重复问题。
func AllocateAndCreate(db *sql.DB, network, id, name, secret string) (*Device, error) {
	const maxRetries = 5
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		ip, err := Allocate(db, network)
		if err != nil {
			return nil, err
		}
		dev := &Device{ID: id, Name: name, IP: ip, Secret: secret}
		if err := Create(db, dev); err != nil {
			if isUniqueConflictOn(err, "devices.ip") {
				lastErr = err
				continue // IP 被并发注册抢占，重新分配
			}
			return nil, err // id 等其它冲突不可重试，原样返回
		}
		return dev, nil
	}
	return nil, fmt.Errorf("allocate unique IP after %d attempts: %w", maxRetries, lastErr)
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
