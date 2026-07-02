package device

import (
	"database/sql"
	"fmt"
	"net"
	"time"
)

func Allocate(db *sql.DB, network string) (string, error) {
	_, ipNet, err := net.ParseCIDR(network)
	if err != nil {
		return "", fmt.Errorf("parse network: %w", err)
	}

	baseIP := ipNet.IP.To4()
	if baseIP == nil {
		return "", fmt.Errorf("only IPv4 supported")
	}

	// 收集已使用的 IP
	rows, err := db.Query("SELECT ip FROM devices")
	if err != nil {
		return "", fmt.Errorf("query used IPs: %w", err)
	}
	defer rows.Close()

	used := make(map[string]bool)
	for rows.Next() {
		var ip string
		rows.Scan(&ip)
		used[ip] = true
	}

	// 从 .2 开始分配（.1 保留给服务器）
	for i := 2; i <= 254; i++ {
		candidate := fmt.Sprintf("%d.%d.%d.%d", baseIP[0], baseIP[1], baseIP[2], i)
		if !used[candidate] {
			// 创建临时占位符记录以保留此 IP
			tempID := fmt.Sprintf("temp-%d-%d", time.Now().UnixNano(), i)
			_, err := db.Exec(
				"INSERT INTO devices (id, name, public_key, ip, secret, passive) VALUES (?, ?, ?, ?, ?, ?)",
				tempID, "temp", fmt.Sprintf("temp-%d", i), candidate, "temp", 0,
			)
			if err != nil {
				// 如果插入失败（例如 IP 已被占用），继续尝试下一个
				continue
			}
			return candidate, nil
		}
	}

	return "", fmt.Errorf("no available IPs in %s", network)
}

func Release(db *sql.DB, addr string) error {
	result, err := db.Exec("DELETE FROM devices WHERE ip = ?", addr)
	if err != nil {
		return fmt.Errorf("release IP: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("IP %s not found", addr)
	}
	return nil
}
