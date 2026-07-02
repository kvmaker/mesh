package token

import (
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"fmt"
)

func Generate() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate random bytes: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func Save(db *sql.DB, value string) error {
	_, err := db.Exec(
		"INSERT OR REPLACE INTO meta (key, value) VALUES ('token', ?)",
		value,
	)
	if err != nil {
		return fmt.Errorf("save token: %w", err)
	}
	return nil
}

func Load(db *sql.DB) (string, error) {
	var value string
	err := db.QueryRow("SELECT value FROM meta WHERE key = 'token'").Scan(&value)
	if err != nil {
		return "", fmt.Errorf("load token: %w", err)
	}
	return value, nil
}

func Verify(db *sql.DB, input string) bool {
	stored, err := Load(db)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(stored), []byte(input)) == 1
}
