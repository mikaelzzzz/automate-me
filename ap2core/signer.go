package ap2core

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"regexp"
)

// Signer holds an EC P-256 private key. AP2 requires a non-deterministic
// signature scheme (ECDSA, never Ed25519) so that the entropy in the signature
// protects checkout_hash against rainbow-table attacks
// (specification.md:154-157).
type Signer struct {
	Key   *ecdsa.PrivateKey
	KeyID string
}

// GenerateSigner creates a fresh P-256 signer.
func GenerateSigner(keyID string) (*Signer, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate P-256 key: %w", err)
	}
	return &Signer{Key: key, KeyID: keyID}, nil
}

// JWK is an EC P-256 public key per ap2/types/jwk.json (RFC 7517 subset).
type JWK struct {
	Kty string `json:"kty"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
	Alg string `json:"alg,omitempty"`
	Kid string `json:"kid,omitempty"`
}

// PublicJWK exports the signer's public key.
func (s *Signer) PublicJWK() JWK {
	x := s.Key.PublicKey.X.FillBytes(make([]byte, 32))
	y := s.Key.PublicKey.Y.FillBytes(make([]byte, 32))
	return JWK{
		Kty: "EC",
		Crv: "P-256",
		X:   base64.RawURLEncoding.EncodeToString(x),
		Y:   base64.RawURLEncoding.EncodeToString(y),
		Alg: "ES256",
		Kid: s.KeyID,
	}
}

var b64u43 = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

// ParseJWK validates and converts a JWK to an ECDSA public key. The schema
// pins kty=EC, crv=P-256 and 43-char base64url coordinates
// (ap2/types/jwk.json).
func ParseJWK(j JWK) (*ecdsa.PublicKey, error) {
	if j.Kty != "EC" {
		return nil, fmt.Errorf("jwk: kty must be EC, got %q", j.Kty)
	}
	if j.Crv != "" && j.Crv != "P-256" {
		return nil, fmt.Errorf("jwk: crv must be P-256, got %q", j.Crv)
	}
	if j.Alg != "" && j.Alg != "ES256" {
		return nil, fmt.Errorf("jwk: alg must be ES256, got %q", j.Alg)
	}
	if !b64u43.MatchString(j.X) || !b64u43.MatchString(j.Y) {
		return nil, errors.New("jwk: x and y must be 43-char base64url (32 bytes)")
	}
	xb, err := base64.RawURLEncoding.DecodeString(j.X)
	if err != nil {
		return nil, fmt.Errorf("jwk: decode x: %w", err)
	}
	yb, err := base64.RawURLEncoding.DecodeString(j.Y)
	if err != nil {
		return nil, fmt.Errorf("jwk: decode y: %w", err)
	}
	pub := &ecdsa.PublicKey{
		Curve: elliptic.P256(),
		X:     new(big.Int).SetBytes(xb),
		Y:     new(big.Int).SetBytes(yb),
	}
	if !pub.Curve.IsOnCurve(pub.X, pub.Y) {
		return nil, errors.New("jwk: point not on P-256 curve")
	}
	return pub, nil
}
