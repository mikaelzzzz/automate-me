package ap2core

import (
	"encoding/base64"
	"testing"
	"time"
)

var t0 = time.Unix(1777342370, 0)

func jwsB64(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }

func testCheckout() Checkout {
	return Checkout{
		ID:       "chk_1",
		Merchant: Merchant{ID: "merchant_1", Name: "Demo Merchant", Website: "https://demo-merchant.example"},
		Items:    []CheckoutItem{{ID: "dw-500", Title: "Dishwasher 500", Price: Amount{Amount: 3000_00, Currency: "BRL"}, Quantity: 1}},
		Total:    Amount{Amount: 3000_00, Currency: "BRL"},
	}
}

func newSigners(t *testing.T) (merchant, user *Signer) {
	t.Helper()
	m, err := GenerateSigner("merchant-key-1")
	if err != nil {
		t.Fatal(err)
	}
	u, err := GenerateSigner("user-key-1")
	if err != nil {
		t.Fatal(err)
	}
	return m, u
}

func TestFullHappyPath(t *testing.T) {
	merchant, user := newSigners(t)

	checkoutJWT, err := SignCheckoutJWT(merchant, testCheckout(), "https://demo-merchant.example", t0)
	if err != nil {
		t.Fatal(err)
	}
	gotCheckout, err := VerifyCheckoutJWT(checkoutJWT, &merchant.Key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if gotCheckout.Total.Amount != 3000_00 || gotCheckout.Merchant.ID != "merchant_1" {
		t.Fatalf("checkout round-trip mismatch: %+v", gotCheckout)
	}

	cm, err := SignClosedCheckoutMandate(user, checkoutJWT, "merchant", NewNonce(), t0)
	if err != nil {
		t.Fatal(err)
	}
	cmContent, err := VerifyClosedCheckoutMandate(cm, &user.Key.PublicKey, "merchant", checkoutJWT)
	if err != nil {
		t.Fatal(err)
	}
	if cmContent.CheckoutHash != CheckoutHash(checkoutJWT) {
		t.Fatal("checkout_hash not bound")
	}

	pm, err := SignClosedPaymentMandate(user, checkoutJWT,
		gotCheckout.Merchant, gotCheckout.Total,
		PaymentInstrument{ID: "sim-card-1", Type: "card", Description: "Card ••••4242 (simulated)"},
		"credential-provider", NewNonce(), t0)
	if err != nil {
		t.Fatal(err)
	}
	pmContent, err := VerifyClosedPaymentMandate(pm, &user.Key.PublicKey, "credential-provider", CheckoutHash(checkoutJWT))
	if err != nil {
		t.Fatal(err)
	}
	if pmContent.TransactionID != cmContent.CheckoutHash {
		t.Fatal("payment mandate not bound to same checkout as checkout mandate")
	}

	// receipts
	cr, err := NewCheckoutReceiptSuccess(merchant, "https://demo-merchant.example", cm, "order_42", t0)
	if err != nil {
		t.Fatal(err)
	}
	crv, err := VerifyCheckoutReceipt(cr, &merchant.Key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if crv.Reference != ReferenceHash(cm) {
		t.Fatal("checkout receipt reference mismatch")
	}
	pr, err := NewPaymentReceiptSuccess(merchant, "https://demo-merchant.example", pm, "pay_1", "psp_1", "net_1", t0)
	if err != nil {
		t.Fatal(err)
	}
	prv, err := VerifyPaymentReceipt(pr, &merchant.Key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if prv.Reference != ReferenceHash(pm) {
		t.Fatal("payment receipt reference mismatch")
	}
}

func TestFailurePathTamperedCheckout(t *testing.T) {
	// design §5 failure path: hash-binding mismatch → abort.
	merchant, user := newSigners(t)
	jwt1, err := SignCheckoutJWT(merchant, testCheckout(), "iss", t0)
	if err != nil {
		t.Fatal(err)
	}
	c2 := testCheckout()
	c2.Total.Amount = 1_00 // manipulated price
	jwt2, err := SignCheckoutJWT(merchant, c2, "iss", t0)
	if err != nil {
		t.Fatal(err)
	}

	cm, err := SignClosedCheckoutMandate(user, jwt1, "merchant", NewNonce(), t0)
	if err != nil {
		t.Fatal(err)
	}
	// merchant MUST verify against the LATEST checkout jwt (MUST #8)
	if _, err := VerifyClosedCheckoutMandate(cm, &user.Key.PublicKey, "merchant", jwt2); err == nil {
		t.Fatal("mandate for old checkout accepted against manipulated checkout")
	}
}

func TestFailurePathWrongKey(t *testing.T) {
	// design §5 failure path: invalid signature → reject with reason.
	merchant, user := newSigners(t)
	attacker, err := GenerateSigner("attacker")
	if err != nil {
		t.Fatal(err)
	}
	checkoutJWT, err := SignCheckoutJWT(merchant, testCheckout(), "iss", t0)
	if err != nil {
		t.Fatal(err)
	}
	cm, err := SignClosedCheckoutMandate(attacker, checkoutJWT, "merchant", NewNonce(), t0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyClosedCheckoutMandate(cm, &user.Key.PublicKey, "merchant", checkoutJWT); err == nil {
		t.Fatal("mandate signed by non-user key accepted")
	}
}

func TestFailurePathVCTMismatch(t *testing.T) {
	// design §5 failure path: vct mismatch → reject (exact match incl. suffix).
	merchant, user := newSigners(t)
	checkoutJWT, err := SignCheckoutJWT(merchant, testCheckout(), "iss", t0)
	if err != nil {
		t.Fatal(err)
	}
	tok, err := signMandate(user, CheckoutMandateContent{
		VCT:          "mandate.checkout", // stale, suffix-less spelling from the SDK README
		CheckoutJWT:  checkoutJWT,
		CheckoutHash: CheckoutHash(checkoutJWT),
	}, "merchant", NewNonce(), t0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyClosedCheckoutMandate(tok, &user.Key.PublicKey, "merchant", checkoutJWT); err == nil {
		t.Fatal("suffix-less vct accepted; spec requires exact match including version suffix")
	}
}

func TestAudAndNonceEnforced(t *testing.T) {
	merchant, user := newSigners(t)
	checkoutJWT, _ := SignCheckoutJWT(merchant, testCheckout(), "iss", t0)

	if _, err := SignClosedCheckoutMandate(user, checkoutJWT, "", "nonce", t0); err == nil {
		t.Fatal("empty aud accepted at signing")
	}
	if _, err := SignClosedCheckoutMandate(user, checkoutJWT, "merchant", "", t0); err == nil {
		t.Fatal("empty nonce accepted at signing")
	}
	cm, _ := SignClosedCheckoutMandate(user, checkoutJWT, "merchant", NewNonce(), t0)
	if _, err := VerifyClosedCheckoutMandate(cm, &user.Key.PublicKey, "credential-provider", checkoutJWT); err == nil {
		t.Fatal("wrong audience accepted")
	}
}

func TestAlgorithmConfusionRejected(t *testing.T) {
	// alg:none / non-ES256 must never verify.
	_, user := newSigners(t)
	header := `{"alg":"none","typ":"dc+sd-jwt"}`
	payload := `{"delegate_payload":[{"vct":"mandate.checkout.1"}],"aud":"merchant","nonce":"x","iat":1}`
	forged := jwsB64(header) + "." + jwsB64(payload) + "."
	if _, err := VerifyClosedCheckoutMandate(forged, &user.Key.PublicKey, "merchant", "whatever"); err == nil {
		t.Fatal("alg:none token accepted")
	}
}

func TestPaymentMandateRequiredFields(t *testing.T) {
	merchant, user := newSigners(t)
	checkoutJWT, _ := SignCheckoutJWT(merchant, testCheckout(), "iss", t0)
	_, err := SignClosedPaymentMandate(user, checkoutJWT, Merchant{}, Amount{Amount: 100, Currency: "BRL"},
		PaymentInstrument{ID: "i", Type: "card"}, "credential-provider", NewNonce(), t0)
	if err == nil {
		t.Fatal("missing payee accepted")
	}
	_, err = SignClosedPaymentMandate(user, checkoutJWT, Merchant{ID: "m"}, Amount{Amount: 0, Currency: "BRL"},
		PaymentInstrument{ID: "i", Type: "card"}, "credential-provider", NewNonce(), t0)
	if err == nil {
		t.Fatal("zero amount accepted")
	}
}

func TestReceiptConditionalFields(t *testing.T) {
	merchant, _ := newSigners(t)
	if _, err := NewCheckoutReceiptSuccess(merchant, "iss", "tok", "", t0); err == nil {
		t.Fatal("Success receipt without order_id accepted")
	}
	if _, err := NewCheckoutReceiptError(merchant, "iss", "tok", ErrCodeInvalidMandate, "", t0); err == nil {
		t.Fatal("Error receipt without description accepted")
	}
	if _, err := NewPaymentReceiptError(merchant, "iss", "tok", "", ErrCodeInvalidMandate, "bad", t0); err == nil {
		t.Fatal("payment Error receipt without payment_id accepted (payment_id is unconditional)")
	}
}

func TestJWKRoundTrip(t *testing.T) {
	_, user := newSigners(t)
	jwk := user.PublicJWK()
	pub, err := ParseJWK(jwk)
	if err != nil {
		t.Fatal(err)
	}
	if pub.X.Cmp(user.Key.PublicKey.X) != 0 || pub.Y.Cmp(user.Key.PublicKey.Y) != 0 {
		t.Fatal("JWK round-trip changed the key")
	}
	if _, err := ParseJWK(JWK{Kty: "OKP", Crv: "Ed25519", X: jwk.X, Y: jwk.Y}); err == nil {
		t.Fatal("Ed25519 JWK accepted; AP2 pins EC P-256")
	}
}

func TestReferenceHashManner(t *testing.T) {
	// ReferenceHash follows the reference SDK (bare leaf JWS); the sd_hash
	// manner from the spec prose is kept as a named variant. See hash.go for
	// the documented deviation.
	if ReferenceHash("abc") != b64uSHA256("abc") {
		t.Fatal("ReferenceHash must hash the bare leaf JWS (SDK manner)")
	}
	if ReferenceHashSDManner("abc") != b64uSHA256("abc~") {
		t.Fatal("ReferenceHashSDManner must include the trailing tilde")
	}
	if CheckoutHash("abc") != b64uSHA256("abc") {
		t.Fatal("CheckoutHash must hash the bare compact JWS string")
	}
}
