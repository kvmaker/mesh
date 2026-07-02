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
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func Save(db *sql.DB, value string) error {
	_, err := db.Exec("INSERT OR REPLACE INTO meta (key, value) VALUES ('token', ?)", value)
	return err
}

func Load(db *sql.DB) (string, error) {
	var v string
	err := db.QueryRow("SELECT value FROM meta WHERE key = 'token'").Scan(&v)
	if err != nil {
		return "", fmt.Errorf("load token: %w", err)
	}
	return v, nil
}

func Verify(db *sql.DB, input string) bool {
	stored, err := Load(db)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(stored), []byte(input)) == 1
}
