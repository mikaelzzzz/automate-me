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

	Store    store.Store
	Merchant *shopping.MerchantClient
	Now      func() time.Time
}

func NewSurface(st store.Store, mc *shopping.MerchantClient) *Surface {
	return &Surface{signers: map[string]*ap2core.Signer{}, Store: st, Merchant: mc, Now: time.Now}
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
}

// ExecuteConsentedPurchase runs the full AP2 v0.2 dance for an approved
// proposal. Calling this endpoint IS the explicit user consent (the UI modal's
// confirm button). Deterministic end to end.
func (s *Surface) ExecuteConsentedPurchase(ctx context.Context, userID, proposalID, productID string, quantity int) (ConsentResult, error) {
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
		Completed: true,
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
