package attachment

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSignerRoundTrip(t *testing.T) {
	s := NewSigner([]byte("secret"))
	id := uuid.New()
	expires := time.Now().Add(time.Hour).Unix()

	sig := s.Sign(id, expires)
	if !s.Verify(id, expires, sig) {
		t.Error("Verify() = false for a signature just produced")
	}
}

func TestSignerRejectsTampering(t *testing.T) {
	s := NewSigner([]byte("secret"))
	id := uuid.New()
	expires := time.Now().Add(time.Hour).Unix()
	sig := s.Sign(id, expires)

	if s.Verify(uuid.New(), expires, sig) {
		t.Error("Verify() accepted a signature for a different id")
	}
	if s.Verify(id, expires+1, sig) {
		t.Error("Verify() accepted a signature for a different expiry")
	}
	if s.Verify(id, expires, sig+"a") {
		t.Error("Verify() accepted a tampered signature")
	}
	if NewSigner([]byte("other")).Verify(id, expires, sig) {
		t.Error("Verify() accepted a signature from a different secret")
	}
}
