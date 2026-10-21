package secret

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"testing"
)

func newKey(t *testing.T) string {
	t.Helper()
	key := make([]byte, KeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(key)
}

func TestSealOpen(t *testing.T) {
	box, err := New(newKey(t))
	if err != nil {
		t.Fatal(err)
	}

	sealed, err := box.Seal([]byte("refresh-token"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("refresh-token")) {
		t.Fatal("sealed value contains the plaintext")
	}

	got, err := box.Open(sealed)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "refresh-token" {
		t.Errorf("Open = %q, want %q", got, "refresh-token")
	}
}

func TestOpenRejectsTamperedValue(t *testing.T) {
	box, _ := New(newKey(t))
	sealed, _ := box.Seal([]byte("refresh-token"))
	sealed[len(sealed)-1] ^= 1

	if _, err := box.Open(sealed); err == nil {
		t.Fatal("want an error for a tampered value")
	}
}

func TestOpenRejectsOtherKey(t *testing.T) {
	a, _ := New(newKey(t))
	b, _ := New(newKey(t))
	sealed, _ := a.Seal([]byte("refresh-token"))

	if _, err := b.Open(sealed); err == nil {
		t.Fatal("want an error when opening with another key")
	}
}

func TestNewRejectsBadKeys(t *testing.T) {
	for _, key := range []string{"", "not base64!", base64.StdEncoding.EncodeToString([]byte("short"))} {
		if _, err := New(key); err == nil {
			t.Errorf("New(%q): want an error", key)
		}
	}
}
