package crashprovider

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"github.com/google/uuid"
)

// vault binds ciphertext to the app ID, so moving a credential to another
// app's row does not make it usable there. An empty key disables new Sentry
// connections without preventing the rest of m-Seismo from starting.
type vault struct{ cipher.AEAD }

func newVault(encoded string) (*vault, error) {
	if encoded == "" {
		return &vault{}, nil
	}
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("crash_providers.encryption_key must be a base64-encoded 32-byte key")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &vault{aead}, nil
}

func (v *vault) seal(appID uuid.UUID, secret string) ([]byte, error) {
	if v.AEAD == nil {
		return nil, fmt.Errorf("Sentry connections require crash_providers.encryption_key")
	}
	nonce := make([]byte, v.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return v.Seal(nonce, nonce, []byte(secret), appID[:]), nil
}

func (v *vault) open(appID uuid.UUID, data []byte) (string, error) {
	if v.AEAD == nil || len(data) < v.NonceSize() {
		return "", fmt.Errorf("crash provider credentials cannot be decrypted")
	}
	plain, err := v.Open(nil, data[:v.NonceSize()], data[v.NonceSize():], appID[:])
	if err != nil {
		return "", fmt.Errorf("crash provider credentials cannot be decrypted")
	}
	return string(plain), nil
}
