package wg

import (
	"encoding/base64"
	"testing"
)

func TestGenerateKeyPair(t *testing.T) {
	priv, pub, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair failed: %v", err)
	}

	// WireGuard keys are 32 bytes base64 encoded
	privBytes, err := base64.StdEncoding.DecodeString(priv)
	if err != nil {
		t.Fatalf("private key not valid base64: %v", err)
	}
	if len(privBytes) != 32 {
		t.Fatalf("expected 32 byte private key, got %d", len(privBytes))
	}

	pubBytes, err := base64.StdEncoding.DecodeString(pub)
	if err != nil {
		t.Fatalf("public key not valid base64: %v", err)
	}
	if len(pubBytes) != 32 {
		t.Fatalf("expected 32 byte public key, got %d", len(pubBytes))
	}

	// 两次生成不同密钥
	priv2, _, _ := GenerateKeyPair()
	if priv == priv2 {
		t.Fatal("two calls generated same key")
	}
}
