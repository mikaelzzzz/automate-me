package ap2core

import (
	"crypto/ecdsa"
	"errors"
	"fmt"
	"time"
)

// Spending Authorization: a standing, user-signed delegation that lets the
// agent complete a purchase without stopping for a per-purchase consent
// screen, for as long as every constraint it carries still holds.
//
// NOT AN AP2 ARTIFACT. AP2 v0.2 defines exactly four vct strings
// (specification.md:138-143) and a Spending Authorization is none of them, so
// this type is namespaced under `automate.` and is never presented to the
// merchant or the credential provider. It is an Automate.me extension,
// documented here and not claimed as conformance — the same discipline the
// rest of this package applies to the choices the spec leaves open.
//
// What it does NOT change: the Trusted Surface is still the only holder of the
// user's key, and it is still the only code that signs (AP2 MUST, the Trusted
// Surface is non-agentic, specification.md:78-80). The agent gains no signing
// power. What moves is *when* the human decides — once, up front, over an
// envelope of purchases, instead of once per purchase. The Checkout Mandate
// and the Payment Mandate that actually move money are still ordinary closed
// AP2 mandates signed per transaction.
//
// The constraints are evaluated against the *merchant-signed* Checkout, after
// its JWT is verified and before anything is signed. A total the merchant did
// not sign can never satisfy the cap.
const VCTSpendingAuthorization = "automate.spending_authorization.1"

// SpendingAuthorizationContent is the content of a Spending Authorization.
type SpendingAuthorizationContent struct {
	VCT string `json:"vct"`
	// MaxPerPurchase caps a single checkout total. A checkout at exactly the
	// cap is allowed; anything above it falls back to the consent screen.
	MaxPerPurchase Amount `json:"max_per_purchase"`
	// AllowedMerchants lists merchant IDs the authorization covers. Empty
	// means "any merchant", which we never issue from the UI but which the
	// verifier must still handle explicitly rather than by omission.
	AllowedMerchants []string `json:"allowed_merchants,omitempty"`
	IAT              int64    `json:"iat,omitempty"`
	EXP              int64    `json:"exp,omitempty"`
}

// ConstraintError reports a constraint the authorization does not satisfy. The
// code is the normative AP2 error code for an unmet constraint
// (agent_authorization.md:521-535), reused here so the failure surfaces to the
// caller with the same vocabulary as the rest of the rail.
type ConstraintError struct {
	Code   string
	Reason string
}

func (e *ConstraintError) Error() string { return e.Code + ": " + e.Reason }

func constraintErr(format string, args ...any) *ConstraintError {
	return &ConstraintError{Code: ErrCodeUnresolvedConstraint, Reason: fmt.Sprintf(format, args...)}
}

// SignSpendingAuthorization signs a standing authorization valid for ttl.
// Calling this IS the user's explicit consent to the envelope: the caller must
// only reach it from the Trusted Surface's consent endpoint, exactly as with a
// per-purchase mandate.
//
// An authorization always expires. There is no way to sign an open-ended one,
// because a delegation the user cannot outlive is not a delegation.
func SignSpendingAuthorization(s *Signer, maxPerPurchase Amount, allowedMerchants []string, aud, nonce string, now time.Time, ttl time.Duration) (string, error) {
	if maxPerPurchase.Amount <= 0 || maxPerPurchase.Currency == "" {
		return "", errors.New("spending authorization: positive max_per_purchase with currency required")
	}
	if ttl <= 0 {
		return "", errors.New("spending authorization: positive ttl required")
	}
	content := SpendingAuthorizationContent{
		VCT:              VCTSpendingAuthorization,
		MaxPerPurchase:   maxPerPurchase,
		AllowedMerchants: allowedMerchants,
		IAT:              nowUnix(now),
		EXP:              nowUnix(now.Add(ttl)),
	}
	return signMandate(s, content, aud, nonce, now)
}

// VerifySpendingAuthorization checks the signature, the audience and the exact
// vct, then that the authorization has not expired. It deliberately does not
// look at any particular purchase — use Permits for that.
func VerifySpendingAuthorization(token string, userPub *ecdsa.PublicKey, wantAud string, now time.Time) (SpendingAuthorizationContent, error) {
	raw, err := verifyMandateEnvelope(token, userPub, wantAud)
	if err != nil {
		return SpendingAuthorizationContent{}, err
	}
	var c SpendingAuthorizationContent
	if err := reencode(raw, &c); err != nil {
		return SpendingAuthorizationContent{}, err
	}
	if c.VCT != VCTSpendingAuthorization {
		return SpendingAuthorizationContent{}, fmt.Errorf("spending authorization: vct %q, want %q", c.VCT, VCTSpendingAuthorization)
	}
	if c.MaxPerPurchase.Amount <= 0 || c.MaxPerPurchase.Currency == "" {
		return SpendingAuthorizationContent{}, errors.New("spending authorization: positive max_per_purchase with currency required")
	}
	if c.EXP == 0 {
		return SpendingAuthorizationContent{}, errors.New("spending authorization: exp required")
	}
	if nowUnix(now) >= c.EXP {
		return SpendingAuthorizationContent{}, constraintErr("authorization expired at %s", time.Unix(c.EXP, 0).UTC().Format(time.RFC3339))
	}
	return c, nil
}

// Permits reports whether this authorization covers one concrete checkout.
// Every failure is a *ConstraintError, so the caller can tell "the user must
// confirm this one" apart from "something is wrong with the rail".
//
// Currency is compared exactly and never converted: an authorization for BRL
// does not authorize a USD checkout at any amount.
func (c SpendingAuthorizationContent) Permits(total Amount, merchantID string, now time.Time) error {
	if c.EXP != 0 && nowUnix(now) >= c.EXP {
		return constraintErr("authorization expired at %s", time.Unix(c.EXP, 0).UTC().Format(time.RFC3339))
	}
	if total.Amount <= 0 || total.Currency == "" {
		return constraintErr("checkout total is not a positive amount with a currency")
	}
	if total.Currency != c.MaxPerPurchase.Currency {
		return constraintErr("checkout is in %s, authorization covers %s", total.Currency, c.MaxPerPurchase.Currency)
	}
	if total.Amount > c.MaxPerPurchase.Amount {
		return constraintErr("checkout total %d %s is above the authorized %d %s per purchase",
			total.Amount, total.Currency, c.MaxPerPurchase.Amount, c.MaxPerPurchase.Currency)
	}
	if len(c.AllowedMerchants) > 0 {
		found := false
		for _, m := range c.AllowedMerchants {
			if m == merchantID {
				found = true
				break
			}
		}
		if !found {
			return constraintErr("merchant %q is not in the authorized list", merchantID)
		}
	}
	return nil
}
