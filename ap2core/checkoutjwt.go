package ap2core

import (
	"crypto/ecdsa"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// SignCheckoutJWT produces the merchant-signed Checkout JWT
// (specification.md:126-128). Plain compact JWS, ES256 — the signature's
// entropy is what makes checkout_hash safe against rainbow tables.
func SignCheckoutJWT(s *Signer, c Checkout, issuer string, now time.Time) (string, error) {
	if c.ID == "" || c.Merchant.ID == "" || len(c.Items) == 0 {
		return "", errors.New("checkout: id, merchant.id and items are required")
	}
	var checkoutMap map[string]any
	if err := reencode(c, &checkoutMap); err != nil {
		return "", err
	}
	claims := jwt.MapClaims{
		"iss":      issuer,
		"iat":      nowUnix(now),
		"checkout": checkoutMap,
	}
	return signWithTyp(s, claims, "checkout+jwt")
}

// VerifyCheckoutJWT verifies the merchant signature and returns the Checkout.
func VerifyCheckoutJWT(token string, merchantPub *ecdsa.PublicKey) (Checkout, error) {
	claims, err := parseVerified(token, merchantPub)
	if err != nil {
		return Checkout{}, err
	}
	rawCheckout, ok := claims["checkout"]
	if !ok {
		return Checkout{}, errors.New("checkout jwt: missing checkout claim")
	}
	var c Checkout
	if err := reencode(rawCheckout, &c); err != nil {
		return Checkout{}, fmt.Errorf("checkout jwt: decode checkout: %w", err)
	}
	if c.ID == "" || c.Merchant.ID == "" || len(c.Items) == 0 {
		return Checkout{}, errors.New("checkout jwt: incomplete checkout payload")
	}
	return c, nil
}
