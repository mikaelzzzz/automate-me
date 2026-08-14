// Package transport serves the merchant's deterministic AP2 rail as plain
// HTTP+JSON. AP2 MUST-rule: "Validation and processing MUST happen in
// deterministic code regardless of whether the role is agentic"
// (specification.md:96-98) — so mandates never pass through an LLM; the A2A
// surface carries only the conversational skills.
package transport

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"automate-me/ap2core"
	"automate-me/merchant/internal/domain"
)

type Handler struct {
	M *domain.Merchant
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /ap2/info", h.info)
	mux.HandleFunc("GET /ap2/catalog", h.catalog)
	mux.HandleFunc("POST /ap2/create-checkout", h.createCheckout)
	mux.HandleFunc("POST /ap2/checkout-mandate", h.checkoutMandate)
	mux.HandleFunc("POST /ap2/payment-mandate", h.paymentMandate)
}

type infoResponse struct {
	Merchant ap2core.Merchant `json:"merchant"`
	JWK      ap2core.JWK      `json:"jwk"`
	Note     string           `json:"note"`
}

func (h *Handler) info(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, infoResponse{
		Merchant: h.M.Info,
		JWK:      h.M.PublicJWK(),
		Note:     "Simulated merchant for the Automate.me demo. No real payments.",
	})
}

func (h *Handler) catalog(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.M.SearchCatalog(r.URL.Query().Get("q")))
}

type createCheckoutRequest struct {
	Items   map[string]int `json:"items"`
	UserJWK ap2core.JWK    `json:"user_jwk"`
}

type createCheckoutResponse struct {
	CheckoutID  string           `json:"checkout_id"`
	Checkout    ap2core.Checkout `json:"checkout"`
	CheckoutJWT string           `json:"checkout_jwt"`
	MerchantJWK ap2core.JWK      `json:"merchant_jwk"`
}

func (h *Handler) createCheckout(w http.ResponseWriter, r *http.Request) {
	var req createCheckoutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	st, err := h.M.CreateCheckout(req.Items, req.UserJWK)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err)
		return
	}
	writeJSON(w, http.StatusOK, createCheckoutResponse{
		CheckoutID:  st.Checkout.ID,
		Checkout:    st.Checkout,
		CheckoutJWT: st.CheckoutJWT,
		MerchantJWK: h.M.PublicJWK(),
	})
}

type mandateRequest struct {
	CheckoutID string `json:"checkout_id"`
	MandateJWS string `json:"mandate_jws"`
}

type mandateResponse struct {
	ReceiptJWT string `json:"receipt_jwt"`
	Accepted   bool   `json:"accepted"`
}

func (h *Handler) checkoutMandate(w http.ResponseWriter, r *http.Request) {
	h.mandate(w, r, h.M.SubmitCheckoutMandate)
}

func (h *Handler) paymentMandate(w http.ResponseWriter, r *http.Request) {
	h.mandate(w, r, h.M.SubmitPaymentMandate)
}

func (h *Handler) mandate(w http.ResponseWriter, r *http.Request, submit func(string, string) (string, bool, error)) {
	var req mandateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	receipt, accepted, err := submit(req.CheckoutID, req.MandateJWS)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err)
		return
	}
	// Rejections still carry a signed Error receipt (AP2 MUST) — HTTP 200.
	writeJSON(w, http.StatusOK, mandateResponse{ReceiptJWT: receipt, Accepted: accepted})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("encode response", "err", err)
	}
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}
