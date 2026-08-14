package ap2core

import (
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// typMandate is our header typ for direct (single-hop) closed mandates.
// The spec leaves the root/terminal typ unpinned for the direct Human Present
// model; "dc+sd-jwt" follows SD-JWT VC convention. Documented choice, not
// conformance.
const typMandate = "dc+sd-jwt"

var es256Only = jwt.WithValidMethods([]string{"ES256"})

func signWithTyp(s *Signer, claims jwt.MapClaims, typ string) (string, error) {
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	if typ != "" {
		tok.Header["typ"] = typ
	}
	if s.KeyID != "" {
		tok.Header["kid"] = s.KeyID
	}
	out, err := tok.SignedString(s.Key)
	if err != nil {
		return "", fmt.Errorf("sign jws: %w", err)
	}
	return out, nil
}

// parseVerified parses a compact JWS, enforcing ES256 (algorithm-confusion
// protection) and signature validity against pub. Returns the raw claims.
func parseVerified(token string, pub *ecdsa.PublicKey) (jwt.MapClaims, error) {
	claims := jwt.MapClaims{}
	_, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		return pub, nil
	}, es256Only, jwt.WithIssuedAt())
	if err != nil {
		return nil, fmt.Errorf("verify jws: %w", err)
	}
	return claims, nil
}

// reencode round-trips a claims subtree into a typed struct.
func reencode(v any, out any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

func nowUnix(now time.Time) int64 { return now.UTC().Unix() }
