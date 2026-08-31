// Package trusted is the non-agentic Trusted Surface (AP2 MUST: the Trusted
// Surface MUST be non-agentic, specification.md:78-80). Only this package
// holds user signing keys and produces mandate signatures, and only after the
// explicit consent call from the UI. No LLM-adjacent code path can reach the
// key.
//
// Hackathon trust scoping (design §5): logical separation inside one service;
// production hardening is a dedicated service + service account.
package trusted

import (
	"context"
	"fmt"
	"sync"
	"time"

	"automate-me/ap2core"
	"automate-me/app/internal/shopping"
	"automate-me/app/internal/store"
)

// Surface holds per-user signers and executes the consented purchase dance.
type Surface struct {
	mu      sync.Mutex
	signers map[string]*ap2core.Signer
	// auths holds each user's standing Spending Authorization JWT. Same demo
	// scope as signers: per-process, never persisted, gone on restart. See
	// authority.go.
	auths map[string]string

	Store    store.Store
	Merchant *shopping.MerchantClient
	Now      func() time.Time
}

func NewSurface(st store.Store, mc *shopping.MerchantClient) *Surface {
	return &Surface{
		signers: map[string]*ap2core.Signer{},
		auths:   map[string]string{},
		Store:   st, Merchant: mc, Now: time.Now,
	}
}

// SignerFor returns (creating on first use) the user's P-256 signer. Demo
// scope: keys are per-process; production stores them in Secret Manager.
func (s *Surface) SignerFor(userID string) (*ap2core.Signer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sg, ok := s.signers[userID]; ok {
		return sg, nil
	}
	sg, err := ap2core.GenerateSigner("user-" + userID)
	if err != nil {
		return nil, err
	}
	s.signers[userID] = sg
	return sg, nil
}

// ConsentResult is what the UI shows after the dance completes.
type ConsentResult struct {
	MandateRecordID string           `json:"mandate_record_id"`
	Checkout        ap2core.Checkout `json:"checkout"`
	CheckoutReceipt string           `json:"checkout_receipt_jwt"`
	PaymentReceipt  string           `json:"payment_receipt_jwt"`
	Completed       bool             `json:"completed"`
	FailureReason   string           `json:"failure_reason,omitempty"`
	// Autonomous is true when a standing Spending Authorization covered this
	// purchase and no consent screen was shown.
	Autonomous bool `json:"autonomous"`
	// NeedsConsent is true when the agent tried to buy under a standing
	// authorization and a constraint refused it. Nothing was signed; the UI
	// must fall back to the consent screen. This is a normal outcome, not an
	// error.
	NeedsConsent bool `json:"needs_consent,omitempty"`
}

// ExecuteConsentedPurchase runs the full AP2 v0.2 dance for an approved
// proposal. Calling this endpoint IS the explicit user consent (the UI modal's
// confirm button). Deterministic end to end.
//
// No gate: consent given for this specific checkout outranks any standing
// envelope, so a purchase the user confirms by hand is never capped.
func (s *Surface) ExecuteConsentedPurchase(ctx context.Context, userID, proposalID, productID string, quantity int) (ConsentResult, error) {
	return s.execute(ctx, userID, proposalID, productID, quantity, nil)
}

// execute is the AP2 dance shared by the consented and the autonomous paths.
//
// gate, when non-nil, is the policy check for a purchase nobody is watching.
// It runs after the merchant-signed Checkout JWT has been verified and before
// the first signature, so a refusal costs nothing and signs nothing. A gate
// error comes back as NeedsConsent rather than as a Go error: "ask the human"
// is an expected outcome of an autonomous attempt, not a fault.
func (s *Surface) execute(ctx context.Context, userID, proposalID, productID string, quantity int, gate func(ap2core.Checkout) error) (ConsentResult, error) {
	prop, err := s.Store.GetProposal(ctx, proposalID)
	if err != nil {
		return ConsentResult{}, fmt.Errorf("proposal: %w", err)
	}
	if prop.UserID != userID {
		return ConsentResult{}, fmt.Errorf("proposal %s does not belong to user", proposalID)
	}
	if prop.Status != store.ProposalApproved {
		return ConsentResult{}, fmt.Errorf("proposal %s is %s, want approved", proposalID, prop.Status)
	}
	signer, err := s.SignerFor(userID)
	if err != nil {
		return ConsentResult{}, err
	}
	now := s.Now()

	// 1. create checkout (user public JWK pinned by the merchant)
	co, err := s.Merchant.CreateCheckout(ctx, map[string]int{productID: quantity}, signer.PublicJWK())
	if err != nil {
		return ConsentResult{}, err
	}
	merchantPub, err := ap2core.ParseJWK(co.MerchantJWK)
	if err != nil {
		return ConsentResult{}, fmt.Errorf("merchant jwk: %w", err)
	}
	// 2. verify the merchant-signed Checkout JWT before consenting to it
	checkout, err := ap2core.VerifyCheckoutJWT(co.CheckoutJWT, merchantPub)
	if err != nil {
		return ConsentResult{}, fmt.Errorf("checkout jwt: %w", err)
	}

	// Policy check on the merchant-signed total. Nothing has been signed yet,
	// so a refusal here leaves no trace on the rail.
	if gate != nil {
		if err := gate(checkout); err != nil {
			return ConsentResult{
				Checkout: checkout, Completed: false,
				NeedsConsent: true, FailureReason: err.Error(),
			}, nil
		}
	}

	rec := store.MandateRecord{
		ID: "mnd-" + co.CheckoutID, UserID: userID, ProposalID: proposalID,
		CheckoutID: co.CheckoutID, CheckoutJWT: co.CheckoutJWT,
		Status: "pending", CreatedAt: now, UpdatedAt: now,
	}

	// 3. closed Checkout Mandate → merchant verifies → Checkout Receipt
	cm, err := ap2core.SignClosedCheckoutMandate(signer, co.CheckoutJWT, "merchant", ap2core.NewNonce(), now)
	if err != nil {
		return ConsentResult{}, err
	}
	rec.CheckoutMandate = cm
	cr, err := s.Merchant.SubmitCheckoutMandate(ctx, co.CheckoutID, cm)
	if err != nil {
		return ConsentResult{}, err
	}
	rec.CheckoutReceipt = cr.ReceiptJWT
	if !cr.Accepted {
		return s.fail(ctx, rec, checkout, "checkout mandate rejected by merchant")
	}

	// 4. closed Payment Mandate → simulated CP/processor verifies → Payment Receipt
	pm, err := ap2core.SignClosedPaymentMandate(signer, co.CheckoutJWT, checkout.Merchant, checkout.Total,
		ap2core.PaymentInstrument{ID: "sim-card-1", Type: "card", Description: "Simulated card ••••4242"},
		"credential-provider", ap2core.NewNonce(), now)
	if err != nil {
		return ConsentResult{}, err
	}
	rec.PaymentMandate = pm
	pr, err := s.Merchant.SubmitPaymentMandate(ctx, co.CheckoutID, pm)
	if err != nil {
		return ConsentResult{}, err
	}
	rec.PaymentReceipt = pr.ReceiptJWT
	if !pr.Accepted {
		return s.fail(ctx, rec, checkout, "payment mandate rejected")
	}

	// 5. persist: audit trail, proposal executed, ledger entry (projected)
	rec.Status = "completed"
	rec.UpdatedAt = s.Now()
	if err := s.Store.PutMandateRecord(ctx, rec); err != nil {
		return ConsentResult{}, err
	}
	prop.Status = store.ProposalExecuted
	if err := s.Store.PutProposal(ctx, prop); err != nil {
		return ConsentResult{}, err
	}
	// The purchase starts recovering next week: one projected weekly entry
	// (net savings / 4.33) the Guardian later confirms or corrects.
	weeklyCents, weeklyHours := projectedWeek(ctx, s.Store, userID, prop)
	if err := s.Store.PutLedgerEntry(ctx, store.LedgerEntry{
		ID: "led-" + rec.ID, UserID: userID, WeekStart: now.AddDate(0, 0, 7),
		RecipeID: prop.RecipeID, HoursRecovered: weeklyHours, BRLRecovered: weeklyCents,
		Confirmed: false, MandateRef: rec.ID,
	}); err != nil {
		return ConsentResult{}, err
	}

	return ConsentResult{
		MandateRecordID: rec.ID, Checkout: checkout,
		CheckoutReceipt: rec.CheckoutReceipt, PaymentReceipt: rec.PaymentReceipt,
		Completed: true, Autonomous: gate != nil,
	}, nil
}

// projectedWeek converts a proposal's monthly verdict into one week of
// projected recovery. Integer centavos; hours only for display.
func projectedWeek(ctx context.Context, st store.Store, userID string, p store.Proposal) (int64, float64) {
	const weeksPerMonth = 4.33
	cents := int64(float64(p.NetMonthlyCents)/weeksPerMonth + 0.5)
	if cents < 0 {
		cents = 0
	}
	u, err := st.GetUser(ctx, userID)
	if err != nil || u.HourlyRateCents <= 0 {
		return cents, 0
	}
	hours := float64(p.MonthlySavingsCents) / float64(u.HourlyRateCents) / weeksPerMonth
	return cents, float64(int(hours*10+0.5)) / 10
}

func (s *Surface) fail(ctx context.Context, rec store.MandateRecord, checkout ap2core.Checkout, reason string) (ConsentResult, error) {
	rec.Status = "failed"
	rec.UpdatedAt = s.Now()
	if err := s.Store.PutMandateRecord(ctx, rec); err != nil {
		return ConsentResult{}, err
	}
	return ConsentResult{
		MandateRecordID: rec.ID, Checkout: checkout,
		CheckoutReceipt: rec.CheckoutReceipt, PaymentReceipt: rec.PaymentReceipt,
		Completed: false, FailureReason: reason,
	}, nil
}
