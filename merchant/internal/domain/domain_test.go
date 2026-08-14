package domain

import (
	"strings"
	"testing"
	"time"

	"automate-me/ap2core"
)

var t0 = time.Unix(1777342370, 0)

func newTestMerchant(t *testing.T) (*Merchant, *ap2core.Signer) {
	t.Helper()
	ms, err := ap2core.GenerateSigner("merchant-key")
	if err != nil {
		t.Fatal(err)
	}
	user, err := ap2core.GenerateSigner("user-key")
	if err != nil {
		t.Fatal(err)
	}
	m := New(ap2core.Merchant{ID: "merchant_1", Name: "Demo Merchant", Website: "https://demo-merchant.example"}, ms, DemoCatalog())
	m.SetClock(func() time.Time { return t0 })
	return m, user
}

func TestSearchCatalog(t *testing.T) {
	m, _ := newTestMerchant(t)
	if got := m.SearchCatalog("dishwasher"); len(got) != 1 || got[0].ID != "dw-500" {
		t.Fatalf("search dishwasher = %+v", got)
	}
	if got := m.SearchCatalog(""); len(got) != len(DemoCatalog()) {
		t.Fatal("empty query should return full catalog")
	}
}

func TestFullPurchaseFlow(t *testing.T) {
	m, user := newTestMerchant(t)

	st, err := m.CreateCheckout(map[string]int{"dw-500": 1}, user.PublicJWK())
	if err != nil {
		t.Fatal(err)
	}
	if st.Checkout.Total.Amount != 3000_00 {
		t.Fatalf("total = %d", st.Checkout.Total.Amount)
	}

	// checkout mandate
	cm, err := ap2core.SignClosedCheckoutMandate(user, st.CheckoutJWT, "merchant", ap2core.NewNonce(), t0)
	if err != nil {
		t.Fatal(err)
	}
	receipt, accepted, err := m.SubmitCheckoutMandate(st.Checkout.ID, cm)
	if err != nil || !accepted {
		t.Fatalf("checkout mandate rejected: %v", err)
	}
	cr, err := ap2core.VerifyCheckoutReceipt(receipt, &m.signer.Key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if cr.Status != ap2core.StatusSuccess || cr.OrderID == "" {
		t.Fatalf("bad receipt: %+v", cr)
	}

	// payment mandate
	pm, err := ap2core.SignClosedPaymentMandate(user, st.CheckoutJWT,
		m.Info, st.Checkout.Total,
		ap2core.PaymentInstrument{ID: "sim-1", Type: "card", Description: "simulated"},
		"credential-provider", ap2core.NewNonce(), t0)
	if err != nil {
		t.Fatal(err)
	}
	pReceipt, accepted, err := m.SubmitPaymentMandate(st.Checkout.ID, pm)
	if err != nil || !accepted {
		t.Fatalf("payment mandate rejected: %v", err)
	}
	pr, err := ap2core.VerifyPaymentReceipt(pReceipt, &m.signer.Key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if pr.Status != ap2core.StatusSuccess || pr.PSPConfirmationID == "" {
		t.Fatalf("bad payment receipt: %+v", pr)
	}
	if !m.checkouts[st.Checkout.ID].Settled {
		t.Fatal("checkout not settled")
	}
}

func TestPaymentBeforeCheckoutMandateRejected(t *testing.T) {
	m, user := newTestMerchant(t)
	st, _ := m.CreateCheckout(map[string]int{"gas-13kg": 1}, user.PublicJWK())
	pm, _ := ap2core.SignClosedPaymentMandate(user, st.CheckoutJWT, m.Info, st.Checkout.Total,
		ap2core.PaymentInstrument{ID: "sim-1", Type: "card"}, "credential-provider", ap2core.NewNonce(), t0)

	receipt, accepted, err := m.SubmitPaymentMandate(st.Checkout.ID, pm)
	if err != nil {
		t.Fatal(err)
	}
	if accepted {
		t.Fatal("payment accepted before checkout mandate")
	}
	pr, err := ap2core.VerifyPaymentReceipt(receipt, &m.signer.Key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if pr.Status != ap2core.StatusError || pr.Error != ap2core.ErrCodeInvalidMandate {
		t.Fatalf("expected invalid_mandate error receipt, got %+v", pr)
	}
}

func TestForgedMandateGetsErrorReceipt(t *testing.T) {
	m, user := newTestMerchant(t)
	attacker, _ := ap2core.GenerateSigner("attacker")
	st, _ := m.CreateCheckout(map[string]int{"dw-500": 1}, user.PublicJWK())

	cm, _ := ap2core.SignClosedCheckoutMandate(attacker, st.CheckoutJWT, "merchant", ap2core.NewNonce(), t0)
	receipt, accepted, err := m.SubmitCheckoutMandate(st.Checkout.ID, cm)
	if err != nil {
		t.Fatal(err)
	}
	if accepted {
		t.Fatal("forged mandate accepted")
	}
	cr, err := ap2core.VerifyCheckoutReceipt(receipt, &m.signer.Key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if cr.Status != ap2core.StatusError || cr.Error != ap2core.ErrCodeInvalidCredential {
		t.Fatalf("expected invalid_credential receipt, got %+v", cr)
	}
	if !strings.Contains(cr.ErrorDescription, "verify") {
		t.Logf("description: %s", cr.ErrorDescription)
	}
}

func TestAmountMismatchRejected(t *testing.T) {
	m, user := newTestMerchant(t)
	st, _ := m.CreateCheckout(map[string]int{"dw-500": 1}, user.PublicJWK())
	cm, _ := ap2core.SignClosedCheckoutMandate(user, st.CheckoutJWT, "merchant", ap2core.NewNonce(), t0)
	if _, accepted, _ := m.SubmitCheckoutMandate(st.Checkout.ID, cm); !accepted {
		t.Fatal("setup: checkout mandate rejected")
	}
	// pay a different amount than the checkout total
	pm, _ := ap2core.SignClosedPaymentMandate(user, st.CheckoutJWT, m.Info,
		ap2core.Amount{Amount: 1_00, Currency: "BRL"},
		ap2core.PaymentInstrument{ID: "sim-1", Type: "card"}, "credential-provider", ap2core.NewNonce(), t0)
	_, accepted, err := m.SubmitPaymentMandate(st.Checkout.ID, pm)
	if err != nil {
		t.Fatal(err)
	}
	if accepted {
		t.Fatal("mismatched amount accepted")
	}
}

func TestCheckoutMandateIdempotentOrder(t *testing.T) {
	m, user := newTestMerchant(t)
	st, _ := m.CreateCheckout(map[string]int{"dw-500": 1}, user.PublicJWK())
	cm, _ := ap2core.SignClosedCheckoutMandate(user, st.CheckoutJWT, "merchant", ap2core.NewNonce(), t0)

	r1, _, _ := m.SubmitCheckoutMandate(st.Checkout.ID, cm)
	r2, _, _ := m.SubmitCheckoutMandate(st.Checkout.ID, cm)
	c1, _ := ap2core.VerifyCheckoutReceipt(r1, &m.signer.Key.PublicKey)
	c2, _ := ap2core.VerifyCheckoutReceipt(r2, &m.signer.Key.PublicKey)
	if c1.OrderID != c2.OrderID {
		t.Fatalf("retry produced a different order: %s vs %s", c1.OrderID, c2.OrderID)
	}
}

func TestCreateCheckoutRejectsBadInput(t *testing.T) {
	m, user := newTestMerchant(t)
	if _, err := m.CreateCheckout(map[string]int{}, user.PublicJWK()); err == nil {
		t.Fatal("empty items accepted")
	}
	if _, err := m.CreateCheckout(map[string]int{"nope": 1}, user.PublicJWK()); err == nil {
		t.Fatal("unknown product accepted")
	}
	if _, err := m.CreateCheckout(map[string]int{"dw-500": 0}, user.PublicJWK()); err == nil {
		t.Fatal("zero quantity accepted")
	}
	if _, err := m.CreateCheckout(map[string]int{"dw-500": 1}, ap2core.JWK{Kty: "oct"}); err == nil {
		t.Fatal("invalid user key accepted")
	}
}
