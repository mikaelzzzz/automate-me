package trusted

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"automate-me/ap2core"
	"automate-me/app/internal/shopping"
	"automate-me/app/internal/store"
)

const testMerchantID = "automate-me-demo-merchant"

// fakeMerchant is a real AP2 counterparty: it signs genuine Checkout JWTs, so
// the Trusted Surface's verification is exercised rather than stubbed. It
// counts mandate submissions, which is the assertion that matters — a gate
// that refuses must leave these at zero.
type fakeMerchant struct {
	signer *ap2core.Signer
	total  int64

	mu               sync.Mutex
	checkoutMandates int
	paymentMandates  int
	checkoutsCreated int
}

func newFakeMerchant(t *testing.T, totalCents int64) (*fakeMerchant, *httptest.Server) {
	t.Helper()
	signer, err := ap2core.GenerateSigner("merchant-test")
	if err != nil {
		t.Fatal(err)
	}
	m := &fakeMerchant{signer: signer, total: totalCents}

	mux := http.NewServeMux()
	mux.HandleFunc("/ap2/create-checkout", func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		m.checkoutsCreated++
		m.mu.Unlock()

		co := ap2core.Checkout{
			ID:       "co-test-1",
			Merchant: ap2core.Merchant{ID: testMerchantID, Name: "Automate.me Demo Merchant"},
			Items: []ap2core.CheckoutItem{{
				ID: "item-1", Title: "Test item",
				Price: ap2core.Amount{Amount: m.total, Currency: "BRL"}, Quantity: 1,
			}},
			Total: ap2core.Amount{Amount: m.total, Currency: "BRL"},
		}
		jwtStr, err := ap2core.SignCheckoutJWT(m.signer, co, testMerchantID, time.Now())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(shopping.CreateCheckoutResult{
			CheckoutID: co.ID, Checkout: co, CheckoutJWT: jwtStr,
			MerchantJWK: m.signer.PublicJWK(),
		})
	})
	mux.HandleFunc("/ap2/checkout-mandate", func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		m.checkoutMandates++
		m.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"receipt_jwt": "receipt-checkout", "accepted": true})
	})
	mux.HandleFunc("/ap2/payment-mandate", func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		m.paymentMandates++
		m.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"receipt_jwt": "receipt-payment", "accepted": true})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return m, srv
}

func (m *fakeMerchant) counts() (checkouts, checkoutMandates, paymentMandates int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.checkoutsCreated, m.checkoutMandates, m.paymentMandates
}

func newTestSurface(t *testing.T, totalCents int64) (*Surface, *fakeMerchant) {
	t.Helper()
	m, srv := newFakeMerchant(t, totalCents)
	st := store.NewMemory()
	ctx := context.Background()
	if err := st.PutUser(ctx, store.User{ID: "demo", HourlyRateCents: 5000}); err != nil {
		t.Fatal(err)
	}
	if err := st.PutProposal(ctx, store.Proposal{
		ID: "prop-1", UserID: "demo", TaskID: "t-1", RecipeID: "r-1",
		MonthlySavingsCents: 137500, NetMonthlyCents: 137500,
		Status: store.ProposalApproved, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	return NewSurface(st, shopping.NewMerchantClient(srv.URL)), m
}

func grant(t *testing.T, s *Surface, capCents int64) {
	t.Helper()
	if _, err := s.GrantSpendingAuthority("demo", capCents, "BRL", []string{testMerchantID}, 24*time.Hour); err != nil {
		t.Fatalf("GrantSpendingAuthority: %v", err)
	}
}

// Under the cap, the agent buys with nobody watching.
func TestAutonomousPurchaseUnderCap(t *testing.T) {
	s, m := newTestSurface(t, 120_00) // gas canister
	grant(t, s, 1000_00)

	got, err := s.ExecuteAutonomousPurchase(context.Background(), "demo", "prop-1", "gas-13kg", 1)
	if err != nil {
		t.Fatalf("ExecuteAutonomousPurchase: %v", err)
	}
	if !got.Completed {
		t.Fatalf("Completed = false, reason %q", got.FailureReason)
	}
	if !got.Autonomous {
		t.Error("Autonomous = false, want true")
	}
	if got.NeedsConsent {
		t.Error("NeedsConsent = true, want false")
	}
	if _, cm, pm := m.counts(); cm != 1 || pm != 1 {
		t.Errorf("mandates submitted = (checkout %d, payment %d), want (1, 1)", cm, pm)
	}
}

// The safety property, and the one worth a test on its own: above the cap
// NOTHING is signed. Not a rejected mandate, not a failed record — nothing
// reaches the merchant past the checkout quote.
func TestAutonomousPurchaseOverCapSignsNothing(t *testing.T) {
	s, m := newTestSurface(t, 3000_00) // dishwasher
	grant(t, s, 1000_00)

	got, err := s.ExecuteAutonomousPurchase(context.Background(), "demo", "prop-1", "dw-500", 1)
	if err != nil {
		t.Fatalf("over the cap must not be a Go error, got %v", err)
	}
	if got.Completed {
		t.Fatal("Completed = true, want false: R$3,000 is above the R$1,000 cap")
	}
	if !got.NeedsConsent {
		t.Error("NeedsConsent = false, want true")
	}
	if got.FailureReason == "" {
		t.Error("want a reason the user can read")
	}
	_, cm, pm := m.counts()
	if cm != 0 || pm != 0 {
		t.Errorf("mandates submitted = (checkout %d, payment %d), want (0, 0) — nothing may be signed above the cap", cm, pm)
	}
	if got.MandateRecordID != "" {
		t.Error("a refused purchase must not produce a mandate record")
	}

	// The proposal stays approved, not executed: it is waiting for consent.
	p, err := s.Store.GetProposal(context.Background(), "prop-1")
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != store.ProposalApproved {
		t.Errorf("proposal status = %q, want %q", p.Status, store.ProposalApproved)
	}
}

// With no standing authorization the agent must not even quote a checkout.
func TestAutonomousPurchaseWithoutAuthority(t *testing.T) {
	s, m := newTestSurface(t, 120_00)

	got, err := s.ExecuteAutonomousPurchase(context.Background(), "demo", "prop-1", "gas-13kg", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Completed || !got.NeedsConsent {
		t.Errorf("Completed=%v NeedsConsent=%v, want false/true", got.Completed, got.NeedsConsent)
	}
	if co, cm, pm := m.counts(); co != 0 || cm != 0 || pm != 0 {
		t.Errorf("merchant calls = (%d, %d, %d), want all zero", co, cm, pm)
	}
}

// Revoking sends the next purchase back through the consent screen.
func TestRevokeSpendingAuthority(t *testing.T) {
	s, _ := newTestSurface(t, 120_00)
	grant(t, s, 1000_00)
	if !s.SpendingAuthorityFor("demo").Active {
		t.Fatal("authority should be active after granting")
	}

	s.RevokeSpendingAuthority("demo")

	if s.SpendingAuthorityFor("demo").Active {
		t.Error("authority should be inactive after revoking")
	}
	got, err := s.ExecuteAutonomousPurchase(context.Background(), "demo", "prop-1", "gas-13kg", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !got.NeedsConsent {
		t.Error("a revoked authority must send the purchase back to consent")
	}
}

// The cap must never block a purchase the user confirms by hand — otherwise
// adding autonomy would have broken the flagship R$3,000 demo.
func TestConsentedPurchaseIgnoresTheCap(t *testing.T) {
	s, m := newTestSurface(t, 3000_00) // dishwasher, far above the cap
	grant(t, s, 1000_00)

	got, err := s.ExecuteConsentedPurchase(context.Background(), "demo", "prop-1", "dw-500", 1)
	if err != nil {
		t.Fatalf("ExecuteConsentedPurchase: %v", err)
	}
	if !got.Completed {
		t.Fatalf("Completed = false, reason %q — explicit consent outranks the cap", got.FailureReason)
	}
	if got.Autonomous {
		t.Error("Autonomous = true, want false: this one was confirmed by hand")
	}
	if _, cm, pm := m.counts(); cm != 1 || pm != 1 {
		t.Errorf("mandates submitted = (checkout %d, payment %d), want (1, 1)", cm, pm)
	}
}

// An authorization for another merchant does not cover this one, however cheap.
func TestAutonomousPurchaseWrongMerchant(t *testing.T) {
	s, m := newTestSurface(t, 10_00)
	if _, err := s.GrantSpendingAuthority("demo", 1000_00, "BRL", []string{"some-other-merchant"}, 24*time.Hour); err != nil {
		t.Fatal(err)
	}

	got, err := s.ExecuteAutonomousPurchase(context.Background(), "demo", "prop-1", "gas-13kg", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !got.NeedsConsent {
		t.Error("an unlisted merchant must fall back to consent")
	}
	if _, cm, pm := m.counts(); cm != 0 || pm != 0 {
		t.Errorf("mandates submitted = (%d, %d), want (0, 0)", cm, pm)
	}
}
