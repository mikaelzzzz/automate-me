package trusted

import (
	"context"
	"errors"
	"fmt"
	"time"

	"automate-me/ap2core"
	"automate-me/app/internal/catalog"
	"automate-me/app/internal/store"
)

// Standing spending authority: the user signs one envelope up front, and the
// agent completes purchases inside it without stopping to ask.
//
// The distinction that matters, and the one the demo should make loudly: the
// agent still cannot sign anything. It cannot reach the key, it cannot mint an
// authorization, and it cannot widen one. All it gained is the ability to
// *proceed* when a check the user already signed says the purchase is inside
// the envelope. Every purchase still produces a real per-transaction Checkout
// Mandate and Payment Mandate, and every one still lands in the audit trail.
//
// Granting is itself an explicit consent action: GrantSpendingAuthority must
// only be reached from the consent endpoint, exactly like a purchase mandate.

// authorityAudience is the aud of a Spending Authorization. It is the Trusted
// Surface itself, not the merchant: this artifact is an instruction from the
// user to their own trusted surface and is never sent over the wire.
const authorityAudience = "trusted-surface"

// DefaultAuthorityTTL bounds how long a standing authorization survives. An
// envelope the user forgets about is the failure mode worth designing against,
// so it expires on its own.
const DefaultAuthorityTTL = 30 * 24 * time.Hour

// DemoMerchantID is the one merchant this build transacts with (the app is
// wired to a single MERCHANT_URL). Authorizations are always scoped to it
// rather than issued for "any merchant", so a merchant the user never
// authorized cannot inherit their envelope.
const DemoMerchantID = "automate-me-demo-merchant"

// SpendingAuthority is the UI-facing view of a standing authorization. The JWT
// itself is deliberately not included: it is a bearer artifact and the browser
// has no use for it.
type SpendingAuthority struct {
	Active           bool     `json:"active"`
	MaxPerPurchase   int64    `json:"max_per_purchase_cents"`
	Currency         string   `json:"currency"`
	AllowedMerchants []string `json:"allowed_merchants,omitempty"`
	ExpiresAt        string   `json:"expires_at,omitempty"`
}

// GrantSpendingAuthority signs a standing authorization for the user and keeps
// it. Calling this IS the user's consent to the envelope.
//
// Re-granting replaces the previous authorization outright rather than adding
// to it, so the active cap is always the last number the user actually saw.
func (s *Surface) GrantSpendingAuthority(userID string, maxPerPurchaseCents int64, currency string, merchants []string, ttl time.Duration) (SpendingAuthority, error) {
	if maxPerPurchaseCents <= 0 {
		return SpendingAuthority{}, errors.New("spending authority: cap must be positive")
	}
	if currency == "" {
		return SpendingAuthority{}, errors.New("spending authority: currency required")
	}
	if ttl <= 0 {
		ttl = DefaultAuthorityTTL
	}
	signer, err := s.SignerFor(userID)
	if err != nil {
		return SpendingAuthority{}, err
	}
	now := s.Now()
	tok, err := ap2core.SignSpendingAuthorization(signer,
		ap2core.Amount{Amount: maxPerPurchaseCents, Currency: currency},
		merchants, authorityAudience, ap2core.NewNonce(), now, ttl)
	if err != nil {
		return SpendingAuthority{}, err
	}

	s.mu.Lock()
	s.auths[userID] = tok
	s.mu.Unlock()

	return s.SpendingAuthorityFor(userID), nil
}

// RevokeSpendingAuthority drops the standing authorization. The next purchase
// goes back through the consent screen.
func (s *Surface) RevokeSpendingAuthority(userID string) {
	s.mu.Lock()
	delete(s.auths, userID)
	s.mu.Unlock()
}

// authorizationFor returns the user's verified authorization content. A stored
// token that no longer verifies (expired, or signed by a key this process no
// longer has after a restart) is treated as absent and dropped.
func (s *Surface) authorizationFor(userID string) (ap2core.SpendingAuthorizationContent, bool) {
	s.mu.Lock()
	tok, ok := s.auths[userID]
	s.mu.Unlock()
	if !ok {
		return ap2core.SpendingAuthorizationContent{}, false
	}
	signer, err := s.SignerFor(userID)
	if err != nil {
		return ap2core.SpendingAuthorizationContent{}, false
	}
	content, err := ap2core.VerifySpendingAuthorization(tok, &signer.Key.PublicKey, authorityAudience, s.Now())
	if err != nil {
		s.mu.Lock()
		delete(s.auths, userID)
		s.mu.Unlock()
		return ap2core.SpendingAuthorizationContent{}, false
	}
	return content, true
}

// SpendingAuthorityFor reports the user's current envelope for the UI.
func (s *Surface) SpendingAuthorityFor(userID string) SpendingAuthority {
	content, ok := s.authorizationFor(userID)
	if !ok {
		return SpendingAuthority{Active: false}
	}
	return SpendingAuthority{
		Active:           true,
		MaxPerPurchase:   content.MaxPerPurchase.Amount,
		Currency:         content.MaxPerPurchase.Currency,
		AllowedMerchants: content.AllowedMerchants,
		ExpiresAt:        time.Unix(content.EXP, 0).UTC().Format(time.RFC3339),
	}
}

// ExecuteAutonomousPurchase attempts a purchase under the user's standing
// authorization, with no consent screen.
//
// Three outcomes, and the caller must distinguish them:
//   - Completed: bought, under the envelope the user signed.
//   - NeedsConsent: a constraint refused it (over the cap, wrong merchant,
//     expired, or no authorization at all). Nothing was signed. Show the
//     consent screen. This returns a nil error.
//   - error: the rail itself failed.
func (s *Surface) ExecuteAutonomousPurchase(ctx context.Context, userID, proposalID, productID string, quantity int) (ConsentResult, error) {
	content, ok := s.authorizationFor(userID)
	if !ok {
		return ConsentResult{
			NeedsConsent:  true,
			FailureReason: "no standing spending authorization: this purchase needs your confirmation",
		}, nil
	}
	// The gate closes over the verified authorization and is applied to the
	// merchant-signed checkout inside the dance, before any signature.
	gate := func(co ap2core.Checkout) error {
		if err := content.Permits(co.Total, co.Merchant.ID, s.Now()); err != nil {
			return fmt.Errorf("%w — this purchase needs your confirmation", err)
		}
		return nil
	}
	return s.execute(ctx, userID, proposalID, productID, quantity, gate)
}

// AutoExecutable reports whether a proposal is a candidate for an autonomous
// purchase at all: the user approved it, and its recipe is one the agent can
// actually execute.
//
// Price is deliberately not consulted here. The only total that may be trusted
// is the one the merchant signed, and that is checked by the gate inside the
// dance — a cap enforced against a locally computed price would be a cap an
// attacker could move.
func AutoExecutable(p store.Proposal, class catalog.Class) bool {
	return p.Status == store.ProposalApproved && class == catalog.ClassExecutable
}
