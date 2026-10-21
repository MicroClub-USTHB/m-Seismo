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
type vault struct {
	cipher.AEAD
	previous []cipher.AEAD
}

func newVault(encoded string, previous ...string) (*vault, error) {
	if len(previous) > 5 || (encoded == "" && len(previous) > 0) {
		return nil, fmt.Errorf("credential rotation requires an active encryption key and at most five previous keys")
	}
	if encoded == "" {
		return &vault{}, nil
	}
	v := &vault{}
	for i, value := range append([]string{encoded}, previous...) {
		aead, err := encryptionCipher(value)
		if err != nil {
			return nil, err
		}
		if i == 0 {
			v.AEAD = aead
		} else {
			v.previous = append(v.previous, aead)
		}
	}
	return v, nil
}

func encryptionCipher(encoded string) (cipher.AEAD, error) {
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
	return aead, nil
}

func (v *vault) seal(appID uuid.UUID, secret string) ([]byte, error) {
	if v.AEAD == nil {
		return nil, fmt.Errorf("sentry connections require crash_providers.encryption_key")
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
	for _, aead := range append([]cipher.AEAD{v.AEAD}, v.previous...) {
		plain, err := aead.Open(nil, data[:aead.NonceSize()], data[aead.NonceSize():], appID[:])
		if err == nil {
			return string(plain), nil
		}
	}
	return "", fmt.Errorf("crash provider credentials cannot be decrypted")
}
