// Package oauth implements Glyph's own OAuth 2.0 authorization server for
// delegated, per-user, agent-initiated access: a client_credentials+subject
// (token exchange) flow for machine-to-machine agent delegation, and a full
// authorization_code+PKCE flow for interactive human consent.
package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
)

// randomToken generates a cryptographically random URL-safe string of n
// underlying bytes (mirrors auth.randomString).
func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// hashToken returns the hex-encoded SHA-256 hash of an opaque token/secret.
// SHA-256 (not bcrypt) is used because these are already 256-bit random
// values, not user-chosen passwords — bcrypt's deliberate slowness would
// only add per-request latency with no security benefit, and every bearer
// request needs a fast hash lookup.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// secureCompareHash reports whether hashToken(candidate) matches storedHash,
// using a constant-time comparison to avoid timing side channels.
func secureCompareHash(candidate, storedHash string) bool {
	return subtle.ConstantTimeCompare([]byte(hashToken(candidate)), []byte(storedHash)) == 1
}

// GenerateClientCredentials returns a new client_id, plaintext client_secret,
// and the secret's hash to persist.
func GenerateClientCredentials() (clientID, clientSecret, secretHash string, err error) {
	idPart, err := randomToken(18)
	if err != nil {
		return "", "", "", err
	}
	clientID = "glyph_client_" + idPart
	clientSecret, err = randomToken(32)
	if err != nil {
		return "", "", "", err
	}
	secretHash = hashToken(clientSecret)
	return clientID, clientSecret, secretHash, nil
}

// GenerateAuthCode returns a new authorization code and its hash to persist.
func GenerateAuthCode() (code, hash string, err error) {
	code, err = randomToken(32)
	if err != nil {
		return "", "", err
	}
	return code, hashToken(code), nil
}

// GenerateAccessToken returns a new opaque access token and its hash.
func GenerateAccessToken() (token, hash string, err error) {
	token, err = randomToken(32)
	if err != nil {
		return "", "", err
	}
	return token, hashToken(token), nil
}

// GenerateRefreshToken returns a new opaque refresh token and its hash.
func GenerateRefreshToken() (token, hash string, err error) {
	token, err = randomToken(32)
	if err != nil {
		return "", "", err
	}
	return token, hashToken(token), nil
}
