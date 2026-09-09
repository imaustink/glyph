package oauth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
)

// verifyPKCE reports whether verifier matches challenge under the S256 method.
// Only S256 is supported — plain is never accepted, even for confidential
// clients, per the plan's security requirements.
func verifyPKCE(verifier, challenge string) bool {
	if len(verifier) < 43 || len(verifier) > 128 {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	computed := base64.RawURLEncoding.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(computed), []byte(challenge)) == 1
}

func isValidCodeChallenge(challenge string) bool {
	return len(challenge) >= 43 && len(challenge) <= 128
}
