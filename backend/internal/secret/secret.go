// Package secret encrypts small secrets (OAuth refresh tokens, ...) before
// they are stored in the database, so a database leak alone does not expose
// them. It uses AES-256-GCM with a key from the configuration.
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
)

// KeySize is the length of the key, in bytes, before base64 encoding.
const KeySize = 32

// Box seals and opens secrets with one key.
type Box struct {
	aead cipher.AEAD
}

// New builds a Box from a base64-encoded 32-byte key, e.g. the output of
// `openssl rand -base64 32`.
func New(encodedKey string) (*Box, error) {
	key, err := base64.StdEncoding.DecodeString(encodedKey)
	if err != nil {
		return nil, fmt.Errorf("secret: decode key: %w", err)
	}
	if len(key) != KeySize {
		return nil, fmt.Errorf("secret: key is %d bytes, want %d", len(key), KeySize)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secret: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secret: %w", err)
	}

	return &Box{aead: aead}, nil
}

// Seal encrypts plaintext. The random nonce is prepended to the result.
func (b *Box) Seal(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("secret: nonce: %w", err)
	}
	return b.aead.Seal(nonce, nonce, plaintext, nil), nil
}

// Open decrypts a value produced by Seal with the same key.
func (b *Box) Open(sealed []byte) ([]byte, error) {
	n := b.aead.NonceSize()
	if len(sealed) < n {
		return nil, errors.New("secret: sealed value too short")
	}
	plaintext, err := b.aead.Open(nil, sealed[:n], sealed[n:], nil)
	if err != nil {
		return nil, fmt.Errorf("secret: open: %w", err)
	}
	return plaintext, nil
}
