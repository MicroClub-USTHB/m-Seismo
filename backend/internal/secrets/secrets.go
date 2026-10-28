// Package secrets encrypts small values stored in the database, such as
// the OAuth tokens of GitLab and Bitbucket connections, with AES-256-GCM.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"

	"go.uber.org/fx"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/config"
)

// Module provides *Cipher from secrets.encryption_key.
var Module = fx.Module("secrets",
	fx.Provide(FromConfig),
)

// KeySize is the AES-256 key length in bytes.
const KeySize = 32

// ErrNoKey is returned by Seal and Open when no key is configured.
var ErrNoKey = errors.New("secrets: secrets.encryption_key is not set")

// devKeySeed derives the key used in development when none is set, so a
// fresh checkout can connect GitLab and Bitbucket without extra setup.
const devKeySeed = "m-seismo-development-only-encryption-key"

// Cipher seals and opens values with AES-256-GCM. Every ciphertext is
// bound to associated data (the row it belongs to), so a ciphertext copied
// into another row does not decrypt.
//
// A Cipher without a key is valid but every Seal and Open fails with
// ErrNoKey, which lets a deployment that uses no OAuth tracker run without
// configuring one.
type Cipher struct {
	aead cipher.AEAD
}

// New builds a Cipher from a 32-byte key; a nil key gives a keyless Cipher.
func New(key []byte) (*Cipher, error) {
	if key == nil {
		return &Cipher{}, nil
	}
	if len(key) != KeySize {
		return nil, fmt.Errorf("secrets: key must be %d bytes, got %d", KeySize, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secrets: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secrets: %w", err)
	}
	return &Cipher{aead: aead}, nil
}

// FromConfig decodes secrets.encryption_key (base64, 32 bytes). In
// development an empty key falls back to a fixed development key, with a
// warning; elsewhere an empty key gives a keyless Cipher (config
// validation already refuses that when an OAuth tracker is configured).
func FromConfig(cfg *config.Config) (*Cipher, error) {
	raw := cfg.Secrets.EncryptionKey
	if raw == "" {
		if cfg.Env != "development" {
			return New(nil)
		}
		slog.Warn("secrets.encryption_key is not set; using the development key")
		sum := sha256.Sum256([]byte(devKeySeed))
		return New(sum[:])
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("secrets: secrets.encryption_key must be base64: %w", err)
	}
	return New(key)
}

// Seal encrypts plaintext bound to aad. The nonce is prepended to the
// result.
func (c *Cipher) Seal(plaintext, aad []byte) ([]byte, error) {
	if c == nil || c.aead == nil {
		return nil, ErrNoKey
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("secrets: nonce: %w", err)
	}
	return c.aead.Seal(nonce, nonce, plaintext, aad), nil
}

// Open decrypts a value produced by Seal with the same aad.
func (c *Cipher) Open(ciphertext, aad []byte) ([]byte, error) {
	if c == nil || c.aead == nil {
		return nil, ErrNoKey
	}
	n := c.aead.NonceSize()
	if len(ciphertext) < n {
		return nil, errors.New("secrets: ciphertext too short")
	}
	plain, err := c.aead.Open(nil, ciphertext[:n], ciphertext[n:], aad)
	if err != nil {
		return nil, errors.New("secrets: cannot decrypt (wrong key or tampered data)")
	}
	return plain, nil
}
