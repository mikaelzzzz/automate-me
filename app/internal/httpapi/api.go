// Package httpapi serves the SPA-facing JSON API (dashboard data + Trusted
// Surface consent). Distinct from the adkrest chat API mounted at /api.
package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"automate-me/app/internal/catalog"
	"automate-me/app/internal/engine"
	"automate-me/app/internal/store"
	"automate-me/app/internal/trusted"
)

type Handler struct {
	Store   store.Store
	Trusted *trusted.Surface
	// UserID resolves the acting user (demo: fixed).
	UserID func(*http.Request) string
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /app/api/pnl", h.pnl)
	mux.HandleFunc("GET /app/api/proposals", h.proposals)
	mux.HandleFunc("POST /app/api/proposals/{id}/approve", h.approve)
	mux.HandleFunc("POST /app/api/trusted/consent", h.consent)
	mux.HandleFunc("GET /app/api/ledger", h.ledger)
}

type pnlResponse struct {
	Tasks           []pnlTask `json:"tasks"`
	TotalHoursMonth float64   `json:"total_hours_month"`
	TotalCents      int64     `json:"total_cents_month"`
	HourlyRateCents int64     `json:"hourly_rate_cents"`
}
type pnlTask struct {
	store.Task
	HoursMonth float64 `json:"hours_month"`
	CostCents  int64   `json:"cost_cents_month"`
}

func (h *Handler) pnl(w http.ResponseWriter, r *http.Request) {
	uid := h.UserID(r)
	u, err := h.Store.GetUser(r.Context(), uid)
	if err != nil {
		httpErr(w, err)
		return
	}
	tasks, err := h.Store.ListTasks(r.Context(), uid)
	if err != nil {
		httpErr(w, err)
		return
	}
	resp := pnlResponse{HourlyRateCents: u.HourlyRateCents}
	for _, t := range tasks {
		et := engine.Task{Name: t.Name, EstMinutes: t.EstMinutes, FreqPerMonth: t.FreqPerMon}
		cost := engine.CostOfInactionCents(et, u.HourlyRateCents)
		hours := engine.MinutesPerMonth(et) / 60
		resp.Tasks = append(resp.Tasks, pnlTask{Task: t, HoursMonth: hours, CostCents: cost})
		resp.TotalHoursMonth += hours
		resp.TotalCents += cost
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) proposals(w http.ResponseWriter, r *http.Request) {
	ps, err := h.Store.ListProposals(r.Context(), h.UserID(r))
	if err != nil {
		httpErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ps)
}

func (h *Handler) approve(w http.ResponseWriter, r *http.Request) {
	uid := h.UserID(r)
	p, err := h.Store.GetProposal(r.Context(), r.PathValue("id"))
	if err != nil {
		httpErr(w, err)
		return
	}
	if p.UserID != uid {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "not your proposal"})
		return
	}
	p.Status = store.ProposalApproved
	if err := h.Store.PutProposal(r.Context(), p); err != nil {
		httpErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

type consentRequest struct {
	ProposalID string `json:"proposal_id"`
	Quantity   int    `json:"quantity"`
}

// consent is the Trusted Surface entry point: the UI shows the checkout and
// the user's confirm click POSTs here. Deterministic path only.
func (h *Handler) consent(w http.ResponseWriter, r *http.Request) {
	uid := h.UserID(r)
	var req consentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	p, err := h.Store.GetProposal(r.Context(), req.ProposalID)
	if err != nil {
		httpErr(w, err)
		return
	}
	recipe, ok := findRecipe(p.RecipeID)
	if !ok || recipe.ProductID == "" {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "proposal has no purchasable product"})
		return
	}
	qty := req.Quantity
	if qty <= 0 {
		qty = 1
	}
	res, err := h.Trusted.ExecuteConsentedPurchase(r.Context(), uid, p.ID, recipe.ProductID, qty)
	if err != nil {
		httpErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (h *Handler) ledger(w http.ResponseWriter, r *http.Request) {
	entries, err := h.Store.ListLedger(r.Context(), h.UserID(r))
	if err != nil {
		httpErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

func findRecipe(id string) (catalog.Recipe, bool) {
	for _, r := range catalog.Seed() {
		if r.ID == id {
			return r, true
		}
	}
	return catalog.Recipe{}, false
}

func httpErr(w http.ResponseWriter, err error) {
	code := http.StatusInternalServerError
	if errors.Is(err, store.ErrNotFound) {
		code = http.StatusNotFound
	}
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("encode response", "err", err)
	}
}
