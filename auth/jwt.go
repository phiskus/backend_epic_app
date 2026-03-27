package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"epic_lab_reporter/config"
)

// BuildAssertion creates a signed JWT for the SMART Backend Services
// client_credentials flow (RFC 7523 / SMART App Launch 2.0).
//
// Epic validates the signature by fetching the JWKS URL registered in the
// developer portal, looking up the key whose `kid` matches the JWT header,
// and verifying the RS256 signature.
//
// The optional `jku` header explicitly tells Epic which JWKS URL to fetch,
// overriding any cached URL lookup — highly recommended during development.
func BuildAssertion(cfg *config.Config, privKey *rsa.PrivateKey, kid string) (string, error) {
	now := time.Now()

	claims := jwt.MapClaims{
		"iss": cfg.EpicClientID, // issuer = client ID
		"sub": cfg.EpicClientID, // subject = client ID
		"aud": cfg.EpicTokenURL, // audience = token endpoint
		"jti": generateJTI(),    // UUID — Epic may validate format
		"exp": now.Add(5 * time.Minute).Unix(),
		"nbf": now.Unix(),       // not-before — required by Epic
		"iat": now.Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid              // Epic matches this against the JWKS `kid`
	token.Header["jku"] = cfg.EpicJWKSURL // tell Epic exactly where to fetch the JWKS

	signed, err := token.SignedString(privKey)
	if err != nil {
		return "", fmt.Errorf("sign JWT: %w", err)
	}
	return signed, nil
}

// generateJTI returns a random UUID v4 string (RFC 4122).
// Epic's documentation shows UUID-format JTIs — we match that exactly.
func generateJTI() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant bits
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
