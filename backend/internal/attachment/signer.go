// Package attachment serves a report's attachments through private,
// expiring links (FR-48), instead of the files themselves being pasted or
// named in an issue.
package attachment

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"

	"github.com/google/uuid"
)

// Signer produces and checks the signature of an attachment link. The
// signed message ties the signature to one attachment id and expiry, so
// neither can be changed without invalidating it.
type Signer struct {
	secret []byte
}

func NewSigner(secret []byte) *Signer {
	return &Signer{secret: secret}
}

// Sign returns the hex-encoded HMAC-SHA256 of id and expires (a Unix
// timestamp).
func (s *Signer) Sign(id uuid.UUID, expires int64) string {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(id.String()))
	mac.Write([]byte(":"))
	mac.Write([]byte(strconv.FormatInt(expires, 10)))
	return hex.EncodeToString(mac.Sum(nil))
}

// Verify reports whether sig is the signature Sign would produce for id
// and expires. It does not check expires against the current time.
func (s *Signer) Verify(id uuid.UUID, expires int64, sig string) bool {
	want, err := hex.DecodeString(sig)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(id.String()))
	mac.Write([]byte(":"))
	mac.Write([]byte(strconv.FormatInt(expires, 10)))
	return hmac.Equal(want, mac.Sum(nil))
}
