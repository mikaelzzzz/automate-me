// Package httpapi serves the SPA-facing JSON API (dashboard data + Trusted
// Surface consent). Distinct from the adkrest chat API mounted at /api.
package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"automate-me/app/internal/briefing"
	"automate-me/app/internal/catalog"
	"automate-me/app/internal/engine"
	"automate-me/app/internal/store"
	"automate-me/app/internal/trusted"
)

type Handler struct {
	Store   store.Store
	Trusted *trusted.Surface
	// Briefing is nil when MAPS_API_KEY is not configured (endpoints answer 503).
	Briefing *briefing.Builder
	Blocks   briefing.BlockWriter
	// Events is where the day's appointments come from: the connected Google
	// Calendar, or the seeded São Paulo day when none is.
	Events briefing.EventSource
	// Live powers the voice session; zero value disables it.
	Live LiveDeps
	// UserID resolves the acting user (demo: fixed).
	UserID func(*http.Request) string
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /app/api/pnl", h.pnl)
	mux.HandleFunc("GET /app/api/proposals", h.proposals)
	mux.HandleFunc("POST /app/api/proposals/{id}/approve", h.approve)
	mux.HandleFunc("POST /app/api/trusted/consent", h.consent)
	mux.HandleFunc("GET /app/api/ledger", h.ledger)
	mux.HandleFunc("GET /app/api/mandates", h.mandates)
	mux.HandleFunc("GET /app/api/agenda", h.agenda)
	mux.HandleFunc("GET /app/api/briefing", h.briefing)
	mux.HandleFunc("POST /app/api/briefing/run", h.runBriefing)
	mux.HandleFunc("POST /app/api/briefing/{id}/block", h.briefingBlock)
	mux.HandleFunc("POST /app/api/live/session", h.liveSession)
	mux.HandleFunc("POST /app/api/live/tool", h.liveTool)
	mux.HandleFunc("POST /app/api/live/remember", h.liveRemember)
}

type briefingResponse struct {
	Day       string               `json:"day"`
	Cards     []store.BriefingCard `json:"cards"`
	Available bool                 `json:"available"`
	// Note states the shape of the day the cards came from — how many
	// appointments were remote, unplaced or ignored.
	Note string `json:"note,omitempty"`
}

// agendaResponse is the day as the calendar has it — every row, not only the
// ones worth a route. The SPA shows this above the briefing cards so the user
// reads their own day, and sees exactly why a row was or was not priced.
type agendaResponse struct {
	Day       string           `json:"day"`
	Source    string           `json:"source"`
	Available bool             `json:"available"`
	Note      string           `json:"note"`
	Trips     int              `json:"trips"`
	Remote    int              `json:"remote"`
	NoPlace   int              `json:"no_place"`
	Skipped   int              `json:"skipped"`
	Entries   []briefing.Entry `json:"entries"`
}

// agenda reads the connected calendar (or the seeded day) for the day being
// briefed. Read-only: it never writes and never costs a Routes call.
func (h *Handler) agenda(w http.ResponseWriter, r *http.Request) {
	if h.Briefing == nil {
		writeJSON(w, http.StatusOK, agendaResponse{Available: false, Entries: []briefing.Entry{}})
		return
	}
	day := h.Briefing.DayFor(h.Briefing.Now())
	sched, err := h.Briefing.Schedule(r.Context(), h.Events, day)
	if err != nil {
		httpErr(w, err)
		return
	}
	source := "seed"
	if h.Events != nil {
		source = h.Events.SourceLabel()
	}
	entries := sched.Entries
	if entries == nil {
		entries = []briefing.Entry{}
	}
	writeJSON(w, http.StatusOK, agendaResponse{
		Day: h.Briefing.DayKey(day), Source: source, Available: true, Note: sched.Note,
		Trips: len(sched.Events), Remote: sched.Remote, NoPlace: sched.NoPlace, Skipped: sched.Skipped,
		Entries: entries,
	})
}

func (h *Handler) briefing(w http.ResponseWriter, r *http.Request) {
	if h.Briefing == nil {
		writeJSON(w, http.StatusOK, briefingResponse{Available: false, Cards: []store.BriefingCard{}})
		return
	}
	day := h.Briefing.DayKey(h.Briefing.DayFor(h.Briefing.Now()))
	cards, err := h.Store.ListBriefing(r.Context(), h.UserID(r), day)
	if err != nil {
		httpErr(w, err)
		return
	}
	if cards == nil {
		cards = []store.BriefingCard{}
	}
	writeJSON(w, http.StatusOK, briefingResponse{Day: day, Cards: cards, Available: true})
}

// runBriefing is what Cloud Scheduler (and the "Plan my day" button) hits:
// fan out one route worker per appointment, price the traffic, check
// weather and flood layers, persist the cards.
func (h *Handler) runBriefing(w http.ResponseWriter, r *http.Request) {
	if h.Briefing == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "MAPS_API_KEY not configured"})
		return
	}
	uid := h.UserID(r)
	u, err := h.Store.GetUser(r.Context(), uid)
	if err != nil {
		httpErr(w, err)
		return
	}
	day := h.Briefing.DayFor(h.Briefing.Now())
	sched, err := h.Briefing.Schedule(r.Context(), h.Events, day)
	if err != nil {
		httpErr(w, err)
		return
	}
	started := time.Now()
	cards := h.Briefing.Build(r.Context(), uid, u.HourlyRateCents, sched.Events)
	for _, c := range cards {
		if err := h.Store.PutBriefingCard(r.Context(), c); err != nil {
			httpErr(w, err)
			return
		}
	}
	slog.Info("briefing built", "day", h.Briefing.DayKey(day), "cards", len(cards),
		"remote", sched.Remote, "no_place", sched.NoPlace, "skipped", sched.Skipped,
		"took", time.Since(started).Round(time.Millisecond))
	writeJSON(w, http.StatusOK, briefingResponse{Day: h.Briefing.DayKey(day), Cards: cards, Available: true, Note: sched.Note})
}

// briefingBlock writes the "Leave at HH:MM" block for one card.
func (h *Handler) briefingBlock(w http.ResponseWriter, r *http.Request) {
	if h.Briefing == nil || h.Blocks == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "briefing not configured"})
		return
	}
	uid := h.UserID(r)
	day := h.Briefing.DayKey(h.Briefing.DayFor(h.Briefing.Now()))
	cards, err := h.Store.ListBriefing(r.Context(), uid, day)
	if err != nil {
		httpErr(w, err)
		return
	}
	for _, c := range cards {
		if c.ID != r.PathValue("id") {
			continue
		}
		id, mode, err := h.Blocks.WriteDepartureBlock(r.Context(), c)
		if err != nil {
			httpErr(w, err)
			return
		}
		c.CalendarBlockID, c.CalendarBlockMode = id, mode
		if err := h.Store.PutBriefingCard(r.Context(), c); err != nil {
			httpErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, c)
		return
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such briefing card today"})
}

// proposalView enriches a proposal with catalog facts the SPA needs to render
// it without a client-side copy of the catalog.
type proposalView struct {
	store.Proposal
	RecipeTitle       string `json:"recipe_title"`
	RecipeDescription string `json:"recipe_description"`
	RecipeClass       string `json:"recipe_class"`
	// Executable: the agent can buy it through the AP2 rail (has a product).
	Executable bool   `json:"executable"`
	ProductID  string `json:"product_id,omitempty"`
	// The Value Engine's inputs, so the consent screen can show the same
	// arithmetic the ranking used instead of restating a conclusion.
	UpfrontCents       int64   `json:"upfront_cents"`
	RunningCents       int64   `json:"monthly_running_cents"`
	MinutesSavedPerOcc int     `json:"minutes_saved_per_occurrence"`
	TaskMinutes        int     `json:"task_minutes"`
	TaskFreqPerMonth   float64 `json:"task_freq_per_month"`
	HourlyRateCents    int64   `json:"hourly_rate_cents"`
}

type ledgerView struct {
	store.LedgerEntry
	RecipeTitle string `json:"recipe_title"`
}

func recipeTitle(id string) string {
	if r, ok := findRecipe(id); ok {
		return r.Title
	}
	return id
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
	u, err := h.Store.GetUser(r.Context(), h.UserID(r))
	if err != nil {
		httpErr(w, err)
		return
	}
	tasks, err := h.Store.ListTasks(r.Context(), h.UserID(r))
	if err != nil {
		httpErr(w, err)
		return
	}
	byID := make(map[string]store.Task, len(tasks))
	for _, t := range tasks {
		byID[t.ID] = t
	}
	out := make([]proposalView, 0, len(ps))
	for _, p := range ps {
		v := proposalView{Proposal: p, RecipeTitle: p.RecipeID, HourlyRateCents: u.HourlyRateCents}
		if rec, ok := findRecipe(p.RecipeID); ok {
			v.RecipeTitle = rec.Title
			v.RecipeDescription = rec.Description
			v.RecipeClass = string(rec.Class)
			v.Executable = rec.Class == catalog.ClassExecutable && rec.ProductID != ""
			v.ProductID = rec.ProductID
			v.UpfrontCents = rec.Cost.UpfrontCents
			v.RunningCents = rec.Cost.MonthlyRunningCents
			v.MinutesSavedPerOcc = rec.Cost.MinutesSavedPerOcc
		}
		if t, ok := byID[p.TaskID]; ok {
			v.TaskMinutes = t.EstMinutes
			v.TaskFreqPerMonth = t.FreqPerMon
			if v.MinutesSavedPerOcc > t.EstMinutes {
				v.MinutesSavedPerOcc = t.EstMinutes
			}
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, out)
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
	out := make([]ledgerView, 0, len(entries))
	for _, e := range entries {
		out = append(out, ledgerView{LedgerEntry: e, RecipeTitle: recipeTitle(e.RecipeID)})
	}
	writeJSON(w, http.StatusOK, out)
}

// mandates exposes the AP2 audit trail (signed JWTs) so the ledger can show
// verifiable receipts. Demo scope: everything for the demo user.
func (h *Handler) mandates(w http.ResponseWriter, r *http.Request) {
	recs, err := h.Store.ListMandateRecords(r.Context(), h.UserID(r))
	if err != nil {
		httpErr(w, err)
		return
	}
	if recs == nil {
		recs = []store.MandateRecord{}
	}
	writeJSON(w, http.StatusOK, recs)
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
