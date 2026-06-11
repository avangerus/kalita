// Package identity manages actor identities and keys: agents hold Ed25519
// keys and sign their requests; the node key seals journal checkpoints; humans
// will sign approvals with WebAuthn (week 4). Anonymous actors do not exist.
package identity

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
)

// GenerateKey creates a new Ed25519 key pair.
func GenerateKey() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	return ed25519.GenerateKey(nil)
}

// SaveKey writes a private key as PKCS#8 PEM with owner-only permissions.
func SaveKey(path string, priv ed25519.PrivateKey) error {
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return err
	}
	block := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, block, 0o600)
}

// LoadKey reads a PKCS#8 PEM Ed25519 private key.
func LoadKey(path string) (ed25519.PrivateKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(raw)
	if block == nil || block.Type != "PRIVATE KEY" {
		return nil, fmt.Errorf("identity: %s is not a PEM private key", path)
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	priv, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("identity: %s is not an Ed25519 key", path)
	}
	return priv, nil
}

// LoadOrCreateNodeKey returns the node key, generating it on first start.
// The node key signs journal checkpoints (EVENT-STORE-v0 §4).
func LoadOrCreateNodeKey(dir string) (ed25519.PrivateKey, error) {
	path := filepath.Join(dir, "node.key")
	if _, err := os.Stat(path); err == nil {
		return LoadKey(path)
	}
	_, priv, err := GenerateKey()
	if err != nil {
		return nil, err
	}
	if err := SaveKey(path, priv); err != nil {
		return nil, err
	}
	return priv, nil
}
