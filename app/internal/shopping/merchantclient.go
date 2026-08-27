// Package shopping executes the deterministic side of a purchase: talking to
// the merchant's AP2 endpoints and running the mandate dance. No LLM ever
// touches a JWS — the Shopping Agent (LLM) only *initiates* purchases by
// creating an approved proposal; execution happens here, triggered by the
// Trusted Surface consent endpoint.
package shopping

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"google.golang.org/api/idtoken"

	"automate-me/ap2core"
)

// MerchantClient calls the merchant's deterministic AP2 rail.
type MerchantClient struct {
	BaseURL string
	HTTP    *http.Client
}

// NewMerchantClient talks to the merchant without credentials (local dev).
func NewMerchantClient(baseURL string) *MerchantClient {
	return &MerchantClient{BaseURL: baseURL, HTTP: &http.Client{Timeout: 15 * time.Second}}
}

// NewAuthenticatedMerchantClient attaches a Google-signed ID token with the
// merchant URL as audience, so the merchant Cloud Run service stays private:
// only identities holding roles/run.invoker on it (this service's runtime SA)
// can reach the AP2 rail. Needs a service-account credential source (Cloud Run
// metadata server); user ADC is not supported by idtoken.
func NewAuthenticatedMerchantClient(ctx context.Context, baseURL string) (*MerchantClient, error) {
	hc, err := idtoken.NewClient(ctx, baseURL)
	if err != nil {
		return nil, fmt.Errorf("merchant id-token client: %w", err)
	}
	hc.Timeout = 15 * time.Second
	return &MerchantClient{BaseURL: baseURL, HTTP: hc}, nil
}

type CreateCheckoutResult struct {
	CheckoutID  string           `json:"checkout_id"`
	Checkout    ap2core.Checkout `json:"checkout"`
	CheckoutJWT string           `json:"checkout_jwt"`
	MerchantJWK ap2core.JWK      `json:"merchant_jwk"`
}

type mandateResult struct {
	ReceiptJWT string `json:"receipt_jwt"`
	Accepted   bool   `json:"accepted"`
}

func (c *MerchantClient) CreateCheckout(ctx context.Context, items map[string]int, userJWK ap2core.JWK) (CreateCheckoutResult, error) {
	var out CreateCheckoutResult
	err := c.post(ctx, "/ap2/create-checkout", map[string]any{"items": items, "user_jwk": userJWK}, &out)
	return out, err
}

func (c *MerchantClient) SubmitCheckoutMandate(ctx context.Context, checkoutID, mandateJWS string) (mandateResult, error) {
	var out mandateResult
	err := c.post(ctx, "/ap2/checkout-mandate", map[string]any{"checkout_id": checkoutID, "mandate_jws": mandateJWS}, &out)
	return out, err
}

func (c *MerchantClient) SubmitPaymentMandate(ctx context.Context, checkoutID, mandateJWS string) (mandateResult, error) {
	var out mandateResult
	err := c.post(ctx, "/ap2/payment-mandate", map[string]any{"checkout_id": checkoutID, "mandate_jws": mandateJWS}, &out)
	return out, err
}

func (c *MerchantClient) post(ctx context.Context, path string, body, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("merchant %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&e)
		return fmt.Errorf("merchant %s: status %d: %s", path, resp.StatusCode, e.Error)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
