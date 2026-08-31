package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"automate-me/app/internal/store"
	"automate-me/app/internal/trusted"
)

// Standing spending authority: the HTTP face of the envelope the user signs
// once so the agent can buy inside it without a consent screen.
//
// Granting one is a Trusted Surface action, exactly like confirming a single
// purchase — it reaches the signing key, so it lives here beside the consent
// endpoint and never behind an agent tool. An LLM can ask the user to widen
// their envelope; it cannot widen it.

// defaultAuthorityDays is what the UI offers when it does not say.
const defaultAuthorityDays = 30

type grantAuthorityRequest struct {
	// MaxPerPurchaseCents caps a single checkout total, in BRL centavos.
	MaxPerPurchaseCents int64 `json:"max_per_purchase_cents"`
	// TTLDays bounds the envelope's life. Zero means defaultAuthorityDays.
	TTLDays int `json:"ttl_days"`
}

func (h *Handler) authority(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.Trusted.SpendingAuthorityFor(h.UserID(r)))
}

// grantAuthority signs the standing authorization. Reaching this endpoint IS
// the user's consent to the envelope.
func (h *Handler) grantAuthority(w http.ResponseWriter, r *http.Request) {
	uid := h.UserID(r)
	var req grantAuthorityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.MaxPerPurchaseCents <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "max_per_purchase_cents must be positive"})
		return
	}
	days := req.TTLDays
	if days <= 0 {
		days = defaultAuthorityDays
	}
	got, err := h.Trusted.GrantSpendingAuthority(uid, req.MaxPerPurchaseCents, "BRL",
		[]string{trusted.DemoMerchantID}, time.Duration(days)*24*time.Hour)
	if err != nil {
		httpErr(w, err)
		return
	}
	slog.Info("spending authority granted",
		"user", uid, "cap_cents", req.MaxPerPurchaseCents, "expires_at", got.ExpiresAt)
	writeJSON(w, http.StatusOK, got)
}

func (h *Handler) revokeAuthority(w http.ResponseWriter, r *http.Request) {
	uid := h.UserID(r)
	h.Trusted.RevokeSpendingAuthority(uid)
	slog.Info("spending authority revoked", "user", uid)
	writeJSON(w, http.StatusOK, h.Trusted.SpendingAuthorityFor(uid))
}

// autonomousAttempt is what an approval reports back about buying on its own.
// Attempted is false when the proposal was never a candidate — not executable,
// or no product behind the recipe — which is different from having tried and
// been refused.
type autonomousAttempt struct {
	Attempted bool `json:"attempted"`
	// Purchased is true only when the whole AP2 dance completed.
	Purchased bool `json:"purchased"`
	// NeedsConsent is true when a constraint refused it and nothing was
	// signed. The UI opens the consent screen.
	NeedsConsent bool   `json:"needs_consent"`
	Reason       string `json:"reason,omitempty"`
	// MandateRecordID links to the audit trail when the purchase completed.
	MandateRecordID string `json:"mandate_record_id,omitempty"`
	TotalCents      int64  `json:"total_cents,omitempty"`
}

// tryAutonomousPurchase attempts to complete an approved proposal under the
// user's standing authorization.
//
// It never returns an error: an autonomous attempt that cannot proceed is a
// normal outcome that must degrade to the consent screen, not an approval
// failure. A rail fault is logged and reported the same way — the user's
// approval still stands either way.
func (h *Handler) tryAutonomousPurchase(ctx context.Context, uid string, p store.Proposal) autonomousAttempt {
	recipe, ok := findRecipe(p.RecipeID)
	if !ok || recipe.ProductID == "" {
		return autonomousAttempt{Attempted: false}
	}
	if !trusted.AutoExecutable(p, recipe.Class) {
		return autonomousAttempt{Attempted: false}
	}
	if !h.Trusted.SpendingAuthorityFor(uid).Active {
		return autonomousAttempt{
			Attempted: false, NeedsConsent: true,
			Reason: "no standing spending authorization: this purchase needs your confirmation",
		}
	}

	res, err := h.Trusted.ExecuteAutonomousPurchase(ctx, uid, p.ID, recipe.ProductID, 1)
	if err != nil {
		slog.Error("autonomous purchase failed", "user", uid, "proposal", p.ID, "err", err)
		return autonomousAttempt{
			Attempted: true, NeedsConsent: true,
			Reason: "the purchase could not be completed automatically; please confirm it yourself",
		}
	}
	out := autonomousAttempt{
		Attempted:       true,
		Purchased:       res.Completed,
		NeedsConsent:    res.NeedsConsent,
		Reason:          res.FailureReason,
		MandateRecordID: res.MandateRecordID,
		TotalCents:      res.Checkout.Total.Amount,
	}
	if res.Completed {
		slog.Info("autonomous purchase completed",
			"user", uid, "proposal", p.ID, "total_cents", out.TotalCents, "mandate", res.MandateRecordID)
	}
	return out
}
