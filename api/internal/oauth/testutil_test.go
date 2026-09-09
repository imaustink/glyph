package oauth

import (
	"crypto/sha256"
	"encoding/base64"
	"testing"
	"time"
)

func timeNowPlus(t *testing.T, d time.Duration) time.Time {
	t.Helper()
	return time.Now().Add(d)
}

func timeNowMinus(t *testing.T, d time.Duration) time.Time {
	t.Helper()
	return time.Now().Add(-d)
}

func b64URLSHA256(s string) string {
	sum := sha256.Sum256([]byte(s))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
