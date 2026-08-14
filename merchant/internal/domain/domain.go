// Package domain implements the merchant half of AP2 v0.2 plus the simulated
// Credential Provider and Merchant Payment Processor roles (one actor may hold
// several AP2 roles; every settlement artifact is labeled as simulation).
//
// Transport-agnostic: the A2A layer wraps these methods as skills.
package domain

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"automate-me/ap2core"
)

// Product is a catalog entry.
type Product struct {
	ID    string
	Title string
	Price ap2core.Amount
}

// CheckoutState tracks one checkout through the mandate flow.
type CheckoutState struct {
	Checkout    ap2core.Checkout
	CheckoutJWT string
	UserKey     ap2core.JWK // pinned at create_checkout; all mandate verification uses it
	OrderID     string
	PaymentID   string
	Settled     bool
}

// Merchant is the in-memory demo merchant. State is per-instance and
// intentionally ephemeral (simulation).
type Merchant struct {
	Info    ap2core.Merchant
	signer  *ap2core.Signer
	catalog []Product

	mu        sync.Mutex
	checkouts map[string]*CheckoutState
	seq       int
	now       func() time.Time
}

func New(info ap2core.Merchant, signer *ap2core.Signer, catalog []Product) *Merchant {
	return &Merchant{
		Info:      info,
		signer:    signer,
		catalog:   catalog,
		checkouts: make(map[string]*CheckoutState),
		now:       time.Now,
	}
}

// SetClock overrides time for tests.
func (m *Merchant) SetClock(now func() time.Time) { m.now = now }

// PublicJWK exposes the merchant verification key (returned in the agent card
// / checkout response so the counterparty can verify Checkout JWTs and
// receipts).
func (m *Merchant) PublicJWK() ap2core.JWK { return m.signer.PublicJWK() }

// SearchCatalog returns products whose title contains the query (case-folded
// match is the A2A layer's job; domain keeps it simple).
func (m *Merchant) SearchCatalog(query string) []Product {
	if query == "" {
		return m.catalog
	}
	var out []Product
	for _, p := range m.catalog {
		if containsFold(p.Title, query) || p.ID == query {
			out = append(out, p)
		}
	}
	return out
}

// CreateCheckout builds a Checkout for the given product quantities, pins the
// user's public JWK for later mandate verification, and returns the
// merchant-signed Checkout JWT (specification.md:126-127).
func (m *Merchant) CreateCheckout(items map[string]int, userKey ap2core.JWK) (*CheckoutState, error) {
	if len(items) == 0 {
		return nil, errors.New("create_checkout: no items")
	}
	if _, err := ap2core.ParseJWK(userKey); err != nil {
		return nil, fmt.Errorf("create_checkout: invalid user key: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	var lines []ap2core.CheckoutItem
	var total int64
	currency := ""
	for id, qty := range items {
		p, ok := m.product(id)
		if !ok {
			return nil, fmt.Errorf("create_checkout: unknown product %q", id)
		}
		if qty <= 0 {
			return nil, fmt.Errorf("create_checkout: invalid quantity for %q", id)
		}
		lines = append(lines, ap2core.CheckoutItem{ID: p.ID, Title: p.Title, Price: p.Price, Quantity: qty})
		total += p.Price.Amount * int64(qty)
		currency = p.Price.Currency
	}

	m.seq++
	c := ap2core.Checkout{
		ID:       fmt.Sprintf("chk_%d", m.seq),
		Merchant: m.Info,
		Items:    lines,
		Total:    ap2core.Amount{Amount: total, Currency: currency},
	}
	jwtStr, err := ap2core.SignCheckoutJWT(m.signer, c, m.Info.Website, m.now())
	if err != nil {
		return nil, fmt.Errorf("create_checkout: sign: %w", err)
	}
	st := &CheckoutState{Checkout: c, CheckoutJWT: jwtStr, UserKey: userKey}
	m.checkouts[c.ID] = st
	return st, nil
}

// SubmitCheckoutMandate verifies the closed Checkout Mandate against the
// pinned user key and the latest Checkout JWT, and returns a signed Checkout
// Receipt — on success AND on failure (MUST: specification.md:130-131,316-317).
// The second return reports whether the mandate was accepted.
func (m *Merchant) SubmitCheckoutMandate(checkoutID, mandateJWS string) (receiptJWT string, accepted bool, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	st, ok := m.checkouts[checkoutID]
	if !ok {
		return "", false, fmt.Errorf("unknown checkout %q", checkoutID)
	}
	pub, err := ap2core.ParseJWK(st.UserKey)
	if err != nil {
		return "", false, err
	}
	if _, verr := ap2core.VerifyClosedCheckoutMandate(mandateJWS, pub, "merchant", st.CheckoutJWT); verr != nil {
		r, err := ap2core.NewCheckoutReceiptError(m.signer, m.Info.Website, mandateJWS,
			ap2core.ErrCodeInvalidCredential, verr.Error(), m.now())
		return r, false, err
	}
	// idempotent by checkout ID: re-submission returns the same order
	if st.OrderID == "" {
		st.OrderID = fmt.Sprintf("order_%s", checkoutID)
	}
	r, err := ap2core.NewCheckoutReceiptSuccess(m.signer, m.Info.Website, mandateJWS, st.OrderID, m.now())
	return r, true, err
}

// SubmitPaymentMandate verifies the closed Payment Mandate (acting as the
// simulated Credential Provider + Processor), simulates settlement, and
// returns a signed Payment Receipt on accept or reject
// (specification.md:159-161,332-333).
func (m *Merchant) SubmitPaymentMandate(checkoutID, mandateJWS string) (receiptJWT string, accepted bool, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	st, ok := m.checkouts[checkoutID]
	if !ok {
		return "", false, fmt.Errorf("unknown checkout %q", checkoutID)
	}
	if st.PaymentID == "" {
		st.PaymentID = fmt.Sprintf("pay_%s", checkoutID)
	}
	if st.OrderID == "" {
		// Payment before an accepted Checkout Mandate (MUST #7 ordering)
		r, err := ap2core.NewPaymentReceiptError(m.signer, m.Info.Website, mandateJWS, st.PaymentID,
			ap2core.ErrCodeInvalidMandate, "no accepted checkout mandate for this checkout", m.now())
		return r, false, err
	}
	pub, err := ap2core.ParseJWK(st.UserKey)
	if err != nil {
		return "", false, err
	}
	wantHash := ap2core.CheckoutHash(st.CheckoutJWT)
	pm, verr := ap2core.VerifyClosedPaymentMandate(mandateJWS, pub, "credential-provider", wantHash)
	if verr == nil && pm.PaymentAmount != st.Checkout.Total {
		verr = errors.New("payment_amount does not match checkout total")
	}
	if verr != nil {
		r, err := ap2core.NewPaymentReceiptError(m.signer, m.Info.Website, mandateJWS, st.PaymentID,
			ap2core.ErrCodeInvalidMandate, verr.Error(), m.now())
		return r, false, err
	}
	// SIMULATED settlement — no money moves, ever.
	st.Settled = true
	r, err := ap2core.NewPaymentReceiptSuccess(m.signer, m.Info.Website, mandateJWS,
		st.PaymentID, "psp_sim_"+checkoutID, "net_sim_"+checkoutID, m.now())
	return r, true, err
}

func (m *Merchant) product(id string) (Product, bool) {
	for _, p := range m.catalog {
		if p.ID == id {
			return p, true
		}
	}
	return Product{}, false
}

func containsFold(haystack, needle string) bool {
	h, n := []rune(haystack), []rune(needle)
	if len(n) == 0 || len(n) > len(h) {
		return false
	}
outer:
	for i := 0; i+len(n) <= len(h); i++ {
		for j := range n {
			a, b := h[i+j], n[j]
			if 'A' <= a && a <= 'Z' {
				a += 'a' - 'A'
			}
			if 'A' <= b && b <= 'Z' {
				b += 'a' - 'A'
			}
			if a != b {
				continue outer
			}
		}
		return true
	}
	return false
}

// DemoCatalog is the seeded product list used in DEMO_MODE and tests.
func DemoCatalog() []Product {
	return []Product{
		{ID: "dw-500", Title: "EcoWash 500 Dishwasher", Price: ap2core.Amount{Amount: 3000_00, Currency: "BRL"}},
		{ID: "gas-13kg", Title: "Gas Canister 13kg (refill + delivery)", Price: ap2core.Amount{Amount: 120_00, Currency: "BRL"}},
		{ID: "rv-200", Title: "RoboVac 200", Price: ap2core.Amount{Amount: 2000_00, Currency: "BRL"}},
		{ID: "grocery-basic", Title: "Weekly Grocery Basket (delivery)", Price: ap2core.Amount{Amount: 350_00, Currency: "BRL"}},
	}
}
