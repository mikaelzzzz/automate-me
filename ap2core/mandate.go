package ap2core

import (
	"crypto/ecdsa"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Closed mandates in the Human Present direct model: the non-agentic Trusted
// Surface signs with the user's key after explicit consent
// (flows.md:57-61). The Mandate Content travels in a `delegate_payload` array
// in the JWT payload, with `iat`, `aud` and `nonce` alongside
// (agent_authorization.md; wire examples). No open mandates and no selective
// disclosure in this build — all Mandate Content claims are plain.

// CheckoutMandateContent is the closed Checkout Mandate content
// (checkout_mandate.json: vct, checkout_jwt, checkout_hash required).
type CheckoutMandateContent struct {
	VCT          string `json:"vct"`
	CheckoutJWT  string `json:"checkout_jwt"`
	CheckoutHash string `json:"checkout_hash"`
	IAT          int64  `json:"iat,omitempty"`
	EXP          int64  `json:"exp,omitempty"`
}

// PaymentMandateContent is the closed Payment Mandate content
// (payment_mandate.json: vct, transaction_id, payee, payment_amount,
// payment_instrument required).
type PaymentMandateContent struct {
	VCT               string            `json:"vct"`
	TransactionID     string            `json:"transaction_id"`
	Payee             Merchant          `json:"payee"`
	PaymentAmount     Amount            `json:"payment_amount"`
	PaymentInstrument PaymentInstrument `json:"payment_instrument"`
	IAT               int64             `json:"iat,omitempty"`
	EXP               int64             `json:"exp,omitempty"`
}

func signMandate(s *Signer, content any, aud, nonce string, now time.Time) (string, error) {
	if aud == "" || nonce == "" {
		return "", errors.New("mandate: aud and nonce are required")
	}
	var contentMap map[string]any
	if err := reencode(content, &contentMap); err != nil {
		return "", err
	}
	claims := jwt.MapClaims{
		"delegate_payload": []any{contentMap},
		"iat":              nowUnix(now),
		"aud":              aud,
		"nonce":            nonce,
	}
	return signWithTyp(s, claims, typMandate)
}

func verifyMandateEnvelope(token string, userPub *ecdsa.PublicKey, wantAud string) (map[string]any, error) {
	claims, err := parseVerified(token, userPub)
	if err != nil {
		return nil, err
	}
	aud, _ := claims["aud"].(string)
	if aud != wantAud {
		return nil, fmt.Errorf("mandate: aud %q, want %q", aud, wantAud)
	}
	if nonce, _ := claims["nonce"].(string); nonce == "" {
		return nil, errors.New("mandate: missing nonce")
	}
	dp, ok := claims["delegate_payload"].([]any)
	if !ok || len(dp) != 1 {
		return nil, errors.New("mandate: delegate_payload must be a single-element array")
	}
	content, ok := dp[0].(map[string]any)
	if !ok {
		return nil, errors.New("mandate: delegate_payload[0] must be an object")
	}
	return content, nil
}

// SignClosedCheckoutMandate signs vct mandate.checkout.1 binding to the given
// Checkout JWT. aud is the verifier ("merchant" in the wire examples).
func SignClosedCheckoutMandate(s *Signer, checkoutJWT, aud, nonce string, now time.Time) (string, error) {
	if checkoutJWT == "" {
		return "", errors.New("checkout mandate: checkout_jwt required")
	}
	content := CheckoutMandateContent{
		VCT:          VCTCheckoutClosed,
		CheckoutJWT:  checkoutJWT,
		CheckoutHash: CheckoutHash(checkoutJWT),
		IAT:          nowUnix(now),
	}
	return signMandate(s, content, aud, nonce, now)
}

// VerifyClosedCheckoutMandate applies the Merchant verification rules
// (specification.md:302-317): signature, exact vct, and checkout_hash matching
// the hash of the latest Checkout JWT sent for approval.
func VerifyClosedCheckoutMandate(token string, userPub *ecdsa.PublicKey, wantAud, latestCheckoutJWT string) (CheckoutMandateContent, error) {
	raw, err := verifyMandateEnvelope(token, userPub, wantAud)
	if err != nil {
		return CheckoutMandateContent{}, err
	}
	var c CheckoutMandateContent
	if err := reencode(raw, &c); err != nil {
		return CheckoutMandateContent{}, err
	}
	if c.VCT != VCTCheckoutClosed {
		return CheckoutMandateContent{}, fmt.Errorf("checkout mandate: vct %q, want %q", c.VCT, VCTCheckoutClosed)
	}
	if c.CheckoutJWT != latestCheckoutJWT {
		return CheckoutMandateContent{}, errors.New("checkout mandate: checkout_jwt does not match the checkout sent for approval")
	}
	if c.CheckoutHash != CheckoutHash(latestCheckoutJWT) {
		return CheckoutMandateContent{}, errors.New("checkout mandate: checkout_hash mismatch")
	}
	return c, nil
}

// SignClosedPaymentMandate signs vct mandate.payment.1. transaction_id is set
// to the checkout_hash of the approved Checkout JWT (§4.2). aud is the payment
// verifier ("credential-provider" in the wire examples).
func SignClosedPaymentMandate(s *Signer, checkoutJWT string, payee Merchant, amount Amount, instrument PaymentInstrument, aud, nonce string, now time.Time) (string, error) {
	content := PaymentMandateContent{
		VCT:               VCTPaymentClosed,
		TransactionID:     CheckoutHash(checkoutJWT),
		Payee:             payee,
		PaymentAmount:     amount,
		PaymentInstrument: instrument,
		IAT:               nowUnix(now),
	}
	if err := validatePaymentContent(content); err != nil {
		return "", err
	}
	return signMandate(s, content, aud, nonce, now)
}

// VerifyClosedPaymentMandate applies the Credential Provider / Processor rules
// (specification.md:319-342): signature, exact vct, transaction_id bound to
// the checkout hash, and all required fields present.
func VerifyClosedPaymentMandate(token string, userPub *ecdsa.PublicKey, wantAud, wantCheckoutHash string) (PaymentMandateContent, error) {
	raw, err := verifyMandateEnvelope(token, userPub, wantAud)
	if err != nil {
		return PaymentMandateContent{}, err
	}
	var p PaymentMandateContent
	if err := reencode(raw, &p); err != nil {
		return PaymentMandateContent{}, err
	}
	if p.VCT != VCTPaymentClosed {
		return PaymentMandateContent{}, fmt.Errorf("payment mandate: vct %q, want %q", p.VCT, VCTPaymentClosed)
	}
	if p.TransactionID != wantCheckoutHash {
		return PaymentMandateContent{}, errors.New("payment mandate: transaction_id not bound to checkout")
	}
	if err := validatePaymentContent(p); err != nil {
		return PaymentMandateContent{}, err
	}
	return p, nil
}

func validatePaymentContent(p PaymentMandateContent) error {
	switch {
	case p.Payee.ID == "":
		return errors.New("payment mandate: payee.id required")
	case p.PaymentAmount.Amount <= 0 || p.PaymentAmount.Currency == "":
		return errors.New("payment mandate: positive payment_amount with currency required")
	case p.PaymentInstrument.ID == "" || p.PaymentInstrument.Type == "":
		return errors.New("payment mandate: payment_instrument id and type required")
	}
	return nil
}
