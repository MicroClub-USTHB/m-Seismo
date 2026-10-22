package secrets

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"testing"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/config"
)

func newKey(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, KeySize)
	if _, err := rand.Read(k); err != nil {
		t.Fatal(err)
	}
	return k
}

func TestSealOpen(t *testing.T) {
	c, err := New(newKey(t))
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte(`{"access_token":"x"}`)
	aad := []byte("connection-1")

	a, err := c.Seal(plain, aad)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := c.Seal(plain, aad)
	if bytes.Equal(a, b) {
		t.Error("two seals of the same value must differ (random nonce)")
	}
	if bytes.Contains(a, plain) {
		t.Error("ciphertext contains the plaintext")
	}

	got, err := c.Open(a, aad)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("Open = %q, %v", got, err)
	}
	if _, err := c.Open(a, []byte("connection-2")); err == nil {
		t.Error("a ciphertext must not open for another row")
	}
	a[len(a)-1] ^= 1
	if _, err := c.Open(a, aad); err == nil {
		t.Error("a tampered ciphertext must not open")
	}
	if _, err := c.Open([]byte("x"), aad); err == nil {
		t.Error("a too-short ciphertext must not open")
	}

	other, _ := New(newKey(t))
	if _, err := other.Open(b, aad); err == nil {
		t.Error("another key must not open it")
	}
}

func TestKeyless(t *testing.T) {
	c, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Seal([]byte("x"), nil); !errors.Is(err, ErrNoKey) {
		t.Errorf("Seal: err = %v, want ErrNoKey", err)
	}
	if _, err := c.Open([]byte("x"), nil); !errors.Is(err, ErrNoKey) {
		t.Errorf("Open: err = %v, want ErrNoKey", err)
	}
	if _, err := New([]byte("short")); err == nil {
		t.Error("a short key must be rejected")
	}
}

func TestFromConfig(t *testing.T) {
	dev, err := FromConfig(&config.Config{Env: "development"})
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := dev.Seal([]byte("x"), nil)
	if err != nil {
		t.Fatalf("development falls back to a key: %v", err)
	}
	dev2, _ := FromConfig(&config.Config{Env: "development"})
	if _, err := dev2.Open(sealed, nil); err != nil {
		t.Error("the development key must be stable across restarts")
	}

	prod, err := FromConfig(&config.Config{Env: "production"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prod.Seal([]byte("x"), nil); !errors.Is(err, ErrNoKey) {
		t.Errorf("production without a key: err = %v, want ErrNoKey", err)
	}

	cfg := &config.Config{Env: "production"}
	cfg.Secrets.EncryptionKey = base64.StdEncoding.EncodeToString(newKey(t))
	if c, err := FromConfig(cfg); err != nil || c.aead == nil {
		t.Errorf("configured key: %v", err)
	}
	cfg.Secrets.EncryptionKey = "not base64!"
	if _, err := FromConfig(cfg); err == nil {
		t.Error("a non-base64 key must be rejected")
	}
}
