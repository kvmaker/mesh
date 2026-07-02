package device

import (
	"database/sql"
	"fmt"
	"net"
)

func Allocate(db *sql.DB, network string) (string, error) {
	_, ipNet, err := net.ParseCIDR(network)
	if err != nil {
		return "", fmt.Errorf("parse network: %w", err)
	}
	base := ipNet.IP.To4()

	rows, err := db.Query("SELECT ip FROM devices")
	if err != nil {
		return "", err
	}
	defer rows.Close()
	used := make(map[string]bool)
	for rows.Next() {
		var ip string
		rows.Scan(&ip)
		used[ip] = true
	}

	for i := 2; i <= 254; i++ {
		candidate := fmt.Sprintf("%d.%d.%d.%d", base[0], base[1], base[2], i)
		if !used[candidate] {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no available IPs")
}
