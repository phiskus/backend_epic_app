package auth

import (
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
func BuildAssertion(cfg *config.Config, privKey *rsa.PrivateKey, kid string) (string, error) {
	now := time.Now()

	claims := jwt.MapClaims{
		"iss": cfg.EpicClientID, // issuer = client ID
		"sub": cfg.EpicClientID, // subject = client ID
		"aud": cfg.EpicTokenURL, // audience = token endpoint
		"jti": generateJTI(),    // unique token ID (replay prevention)
		"exp": now.Add(5 * time.Minute).Unix(),
		"iat": now.Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid // Epic matches this against the JWKS `kid`

	signed, err := token.SignedString(privKey)
	if err != nil {
		return "", fmt.Errorf("sign JWT: %w", err)
	}
	return signed, nil
}

// generateJTI returns a unique identifier for this JWT using the current
// nanosecond timestamp encoded as a base-36-style hex string.
// For production, a UUID library could be used; this avoids extra deps.
func generateJTI() string {
	return fmt.Sprintf("%x", time.Now().UnixNano())
}
