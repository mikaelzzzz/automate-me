package ap2core

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

func b64uSHA256(s string) string {
	sum := sha256.Sum256([]byte(s))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// CheckoutHash computes the binding digest of a Checkout JWT:
// base64url(SHA-256(compact JWS serialization string)) — over the full ASCII
// "header.payload.signature", not the payload (verified against the spec's own
// test vectors, docs/research/ap2-v02-schema.md §4.6).
func CheckoutHash(checkoutJWT string) string {
	return b64uSHA256(checkoutJWT)
}

// PresentedToken is the wire form of a mandate with zero disclosures: the
// compact JWS followed by a trailing "~" (SD-JWT presentation with an empty
// disclosure list).
func PresentedToken(mandateJWS string) string {
	return mandateJWS + "~"
}

// ReferenceHash computes a Receipt's `reference` over the bare leaf JWS.
//
// DOCUMENTED DEVIATION: the spec prose says the reference is "calculated in
// the same manner as sd_hash" (agent_authorization.md:509-513, which would
// include disclosures and the trailing "~"), but the reference SDK
// deliberately hashes only the leaf issuer JWT for stability across hops and
// disclosure choices (docs/research/ap2-v02-schema.md §4.5, §12.1). The two
// manners produce different values. We follow the SDK for interoperability;
// ReferenceHashSDManner is kept for cross-checking.
func ReferenceHash(mandateJWS string) string {
	return b64uSHA256(mandateJWS)
}

// ReferenceHashSDManner is the literal spec-prose reading (sd_hash manner,
// over the presented token including the trailing "~").
func ReferenceHashSDManner(mandateJWS string) string {
	return b64uSHA256(PresentedToken(mandateJWS))
}

// NewNonce returns a 128-bit random hex nonce for KB hops.
func NewNonce() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // crypto/rand failure is unrecoverable
	}
	return hex.EncodeToString(b[:])
}
