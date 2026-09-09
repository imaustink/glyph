package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"math/big"
	"testing"
)

func encodeBigInt(n *big.Int) string {
	return base64.RawURLEncoding.EncodeToString(n.Bytes())
}

// TestParseRSAKey_RejectsWeakModulus is a regression test: parseRSAKey used
// to accept an RSA key of any size, including one too weak to offer any
// real security margin, from a JWKS response.
func TestParseRSAKey_RejectsWeakModulus(t *testing.T) {
	// parseRSAKey only ever reads N and E as raw bytes — it never has to be
	// a real, factorizable RSA keypair for this test, so build a
	// deliberately small modulus directly rather than asking crypto/rsa to
	// generate one (which itself now refuses anything below 1024 bits).
	weakN := big.NewInt(1)
	weakN.Lsh(weakN, 512) // a 512-bit-ish modulus
	k := jwk{
		Kty: "RSA",
		N:   encodeBigInt(weakN),
		E:   encodeBigInt(big.NewInt(65537)),
	}
	if _, err := parseRSAKey(k); err == nil {
		t.Error("expected an error for a 512-bit RSA modulus, got none")
	}
}

func TestParseRSAKey_AcceptsStrongModulus(t *testing.T) {
	strong, err := rsa.GenerateKey(rand.Reader, minRSAModulusBits)
	if err != nil {
		t.Fatalf("generate strong key: %v", err)
	}
	k := jwk{
		Kty: "RSA",
		N:   encodeBigInt(strong.N),
		E:   encodeBigInt(big.NewInt(int64(strong.E))),
	}
	pub, err := parseRSAKey(k)
	if err != nil {
		t.Fatalf("unexpected error for a %d-bit RSA modulus: %v", minRSAModulusBits, err)
	}
	if pub.N.Cmp(strong.N) != 0 {
		t.Error("parsed modulus does not match input")
	}
}
