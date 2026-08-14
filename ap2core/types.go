// Package ap2core implements the AP2 v0.2 artifacts used by Automate.me:
// merchant-signed Checkout JWTs, user-signed closed Checkout/Payment Mandates
// (Human Present, direct model), and merchant/processor-signed Receipts.
//
// Spec reference: docs/research/ap2-v02-schema.md (transcribed from
// github.com/google-agentic-commerce/AP2, docs/ap2/*). Where the spec is
// silent, choices are documented at the definition site and never claimed as
// conformance.
package ap2core

// The four vct strings. Implementations MUST match exactly, including the
// numeric version suffix (specification.md:138-143).
const (
	VCTCheckoutClosed = "mandate.checkout.1"
	VCTCheckoutOpen   = "mandate.checkout.open.1"
	VCTPaymentClosed  = "mandate.payment.1"
	VCTPaymentOpen    = "mandate.payment.open.1"
)

// Normative error codes (agent_authorization.md:521-535).
const (
	ErrCodeInvalidCredential    = "invalid_credential"
	ErrCodeUnresolvedConstraint = "unresolved_constraint"
	ErrCodeInvalidMandate       = "invalid_mandate"
	ErrCodeMandatesNotSupported = "mandates_not_supported"
)

// Amount is money in ISO 4217 minor units (e.g. 27999 = $279.99).
type Amount struct {
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
}

// Merchant identifies the payee (ap2/types/merchant.json).
type Merchant struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Website string `json:"website,omitempty"`
}

// PaymentInstrument is the instrument charged (ap2/types/payment_instrument.json).
type PaymentInstrument struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

// CheckoutItem is one line item of a Checkout.
type CheckoutItem struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Price    Amount `json:"price"`
	Quantity int    `json:"quantity"`
}

// Checkout is the payload carried inside the merchant-signed Checkout JWT.
// AP2 is agnostic to its contents (specification.md:383-386); this shape is a
// UCP-compatible minimum for our catalog-checkout merchant.
type Checkout struct {
	ID       string         `json:"id"`
	Merchant Merchant       `json:"merchant"`
	Items    []CheckoutItem `json:"items"`
	Total    Amount         `json:"total"`
}
