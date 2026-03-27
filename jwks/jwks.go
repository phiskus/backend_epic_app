package jwks

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"

	"epic_lab_reporter/auth"
)

// jwk represents a single JSON Web Key (RFC 7517).
type jwk struct {
	Kty string `json:"kty"` // Key type — always "RSA" here
	Use string `json:"use"` // Public key use — "sig" (signature)
	Alg string `json:"alg"` // Algorithm — "RS256"
	Kid string `json:"kid"` // Key ID — must match JWT header `kid`
	N   string `json:"n"`   // RSA modulus, base64url-encoded (no padding)
	E   string `json:"e"`   // RSA public exponent, base64url-encoded (no padding)
}

// jwkSet is the JSON container returned at /.well-known/jwks.json.
type jwkSet struct {
	Keys []jwk `json:"keys"`
}

// Handler loads the RSA public key from pubKeyPath and returns an
// http.HandlerFunc that serves the JWKS response.
// Called once at startup — if the key can't be loaded, the service won't start.
func Handler(pubKeyPath string) (http.HandlerFunc, error) {
	pub, err := auth.LoadPublicKey(pubKeyPath)
	if err != nil {
		return nil, fmt.Errorf("jwks: load public key: %w", err)
	}

	kid, err := auth.DeriveKID(pub)
	if err != nil {
		return nil, fmt.Errorf("jwks: derive kid: %w", err)
	}

	// Encode RSA modulus (N) and exponent (E) as base64url without padding.
	// big.Int.Bytes() returns big-endian unsigned bytes — correct for JWK.
	nBytes := pub.N.Bytes()
	eBytes := big.NewInt(int64(pub.E)).Bytes()

	key := jwk{
		Kty: "RSA",
		Use: "sig",
		Alg: "RS256",
		Kid: kid,
		N:   base64.RawURLEncoding.EncodeToString(nBytes),
		E:   base64.RawURLEncoding.EncodeToString(eBytes),
	}

	payload, err := json.Marshal(jwkSet{Keys: []jwk{key}})
	if err != nil {
		return nil, fmt.Errorf("jwks: marshal response: %w", err)
	}

	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// no-store forces Epic to always fetch fresh keys — recommended for dev/testing
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	}, nil
}
