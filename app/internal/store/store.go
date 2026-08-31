// Package store defines the persistence boundary for Automate.me (design §4).
// Two implementations: Memory (DEMO_MODE=seed, tests) and Firestore (prod).
package store

import (
	"context"
	"errors"
	"time"
)

var ErrNotFound = errors.New("store: not found")

// Task is a confirmed routine task (design §4 routine_profiles).
type Task struct {
	ID         string  `json:"id" firestore:"id"`
	Name       string  `json:"name" firestore:"name"`
	EstMinutes int     `json:"est_minutes" firestore:"est_minutes"`
	FreqPerMon float64 `json:"freq_per_month" firestore:"freq_per_month"`
	// Source: interview | photo | calendar
	Source    string `json:"source" firestore:"source"`
	Confirmed bool   `json:"confirmed" firestore:"confirmed"`
}

// User holds profile + rate (design §4 users). Money in centavos.
type User struct {
	ID              string `json:"id" firestore:"id"`
	Name            string `json:"name" firestore:"name"`
	Mode            string `json:"mode" firestore:"mode"` // personal | teams
	HourlyRateCents int64  `json:"hourly_rate_cents" firestore:"hourly_rate_cents"`

	// What onboarding asks once and everything else is priced from.
	//
	// RateBasis records how the hour got its price: "declared" when the user
	// typed it, "income" when it was derived from what they earn. Keeping the
	// income and the hours behind a derived rate means the number can be
	// re-derived — and explained — instead of being a figure nobody can trace.
	RateBasis          string  `json:"rate_basis,omitempty" firestore:"rate_basis,omitempty"`
	MonthlyIncomeCents int64   `json:"monthly_income_cents,omitempty" firestore:"monthly_income_cents,omitempty"`
	HoursPerWeek       float64 `json:"hours_per_week,omitempty" firestore:"hours_per_week,omitempty"`
	// Where the day starts and where the work is: the briefing routes from
	// home, and the agent stops asking where "the office" is.
	HomeAddress string `json:"home_address,omitempty" firestore:"home_address,omitempty"`
	WorkAddress string `json:"work_address,omitempty" firestore:"work_address,omitempty"`
	// WorkSetup is "remote" | "hybrid" | "onsite" — it decides whether a
	// commute is even a routine worth pricing.
	WorkSetup string `json:"work_setup,omitempty" firestore:"work_setup,omitempty"`
	// Onboarded is false until the user has priced their hour once.
	Onboarded bool `json:"onboarded" firestore:"onboarded"`
}

// ProposalStatus lifecycle (design §4 proposals).
type ProposalStatus string

const (
	ProposalProposed ProposalStatus = "proposed"
	ProposalApproved ProposalStatus = "approved"
	ProposalExecuted ProposalStatus = "executed"
	ProposalDeclined ProposalStatus = "declined"
)

// Proposal pairs a task with a catalog recipe and the engine's verdict.
type Proposal struct {
	ID                  string         `json:"id" firestore:"id"`
	UserID              string         `json:"user_id" firestore:"user_id"`
	TaskID              string         `json:"task_id" firestore:"task_id"`
	RecipeID            string         `json:"recipe_id" firestore:"recipe_id"`
	MonthlySavingsCents int64          `json:"monthly_savings_cents" firestore:"monthly_savings_cents"`
	NetMonthlyCents     int64          `json:"net_monthly_cents" firestore:"net_monthly_cents"`
	PaybackMonths       float64        `json:"payback_months" firestore:"payback_months"`
	Status              ProposalStatus `json:"status" firestore:"status"`
	CreatedAt           time.Time      `json:"created_at" firestore:"created_at"`
}

// MandateRecord is the AP2 audit trail (design §4 mandates).
type MandateRecord struct {
	ID              string    `json:"id" firestore:"id"`
	UserID          string    `json:"user_id" firestore:"user_id"`
	ProposalID      string    `json:"proposal_id" firestore:"proposal_id"`
	CheckoutID      string    `json:"checkout_id" firestore:"checkout_id"`
	CheckoutJWT     string    `json:"checkout_jwt" firestore:"checkout_jwt"`
	CheckoutMandate string    `json:"checkout_mandate" firestore:"checkout_mandate"`
	CheckoutReceipt string    `json:"checkout_receipt" firestore:"checkout_receipt"`
	PaymentMandate  string    `json:"payment_mandate" firestore:"payment_mandate"`
	PaymentReceipt  string    `json:"payment_receipt" firestore:"payment_receipt"`
	Status          string    `json:"status" firestore:"status"` // pending | completed | failed
	CreatedAt       time.Time `json:"created_at" firestore:"created_at"`
	UpdatedAt       time.Time `json:"updated_at" firestore:"updated_at"`
}

// LedgerEntry (design §4 savings_ledger): projected until the Guardian
// confirms; MandateRef links purchases to verifiable receipts (F9).
type LedgerEntry struct {
	ID             string    `json:"id" firestore:"id"`
	UserID         string    `json:"user_id" firestore:"user_id"`
	WeekStart      time.Time `json:"week_start" firestore:"week_start"`
	RecipeID       string    `json:"recipe_id" firestore:"recipe_id"`
	HoursRecovered float64   `json:"hours_recovered" firestore:"hours_recovered"`
	BRLRecovered   int64     `json:"brl_recovered_cents" firestore:"brl_recovered_cents"`
	Confirmed      bool      `json:"confirmed" firestore:"confirmed"`
	MandateRef     string    `json:"mandate_ref,omitempty" firestore:"mandate_ref,omitempty"`
}

// PlanStatus (design §4 action_plans).
type PlanStatus string

const (
	PlanOnTrack   PlanStatus = "on_track"
	PlanDrifting  PlanStatus = "drifting"
	PlanDone      PlanStatus = "done"
	PlanAbandoned PlanStatus = "abandoned"
)

// SignalType — briefing-block acceptance is a calendar_block signal; no
// separate type (design §4).
type SignalType string

const (
	SignalCalendarBlock SignalType = "calendar_block"
	SignalAP2Delivery   SignalType = "ap2_delivery"
	SignalRecipeRun     SignalType = "recipe_run"
)

// ActionPlan is the Guardian's contract per approved proposal (F10).
type ActionPlan struct {
	ID              string       `json:"id" firestore:"id"`
	UserID          string       `json:"user_id" firestore:"user_id"`
	ProposalID      string       `json:"proposal_id" firestore:"proposal_id"`
	ExpectedHours   float64      `json:"expected_hours_week" firestore:"expected_hours_week"`
	ExpectedCents   int64        `json:"expected_cents_week" firestore:"expected_cents_week"`
	ExpectedSignals []SignalType `json:"expected_signals" firestore:"expected_signals"`
	Status          PlanStatus   `json:"status" firestore:"status"`
	AdherenceScore  float64      `json:"adherence_score" firestore:"adherence_score"`
	LastCheckin     time.Time    `json:"last_checkin" firestore:"last_checkin"`
}

// BriefingCard is one appointment's daily briefing (design §4 briefings).
type BriefingCard struct {
	ID            string    `json:"id" firestore:"id"`
	UserID        string    `json:"user_id" firestore:"user_id"`
	Day           string    `json:"day" firestore:"day"` // YYYY-MM-DD
	EventSummary  string    `json:"event_summary" firestore:"event_summary"`
	EventStart    time.Time `json:"event_start" firestore:"event_start"`
	Origin        string    `json:"origin" firestore:"origin"`
	Destination   string    `json:"destination" firestore:"destination"`
	DepartureTime time.Time `json:"departure_time" firestore:"departure_time"`
	RouteSummary  string    `json:"route_summary" firestore:"route_summary"`
	RouteMinutes  int       `json:"route_minutes" firestore:"route_minutes"`
	// TrafficMinutes = duration − staticDuration; TrafficCents prices it at
	// the user's hourly rate (Value Engine input, measured not guessed).
	TrafficMinutes  int      `json:"traffic_minutes" firestore:"traffic_minutes"`
	TrafficCents    int64    `json:"traffic_cents" firestore:"traffic_cents"`
	Weather         string   `json:"weather" firestore:"weather"`
	WeatherTempC    float64  `json:"weather_temp_c" firestore:"weather_temp_c"`
	RainChancePct   int      `json:"rain_chance_pct" firestore:"rain_chance_pct"`
	Clothing        string   `json:"clothing" firestore:"clothing"`
	FloodRisk       string   `json:"flood_risk" firestore:"flood_risk"` // none | historic | alert
	FloodDetail     string   `json:"flood_detail" firestore:"flood_detail"`
	FloodPoints     int      `json:"flood_points" firestore:"flood_points"`
	AlertHeadline   string   `json:"alert_headline,omitempty" firestore:"alert_headline,omitempty"`
	AlternativeNote string   `json:"alternative_note" firestore:"alternative_note"`
	Notes           []string `json:"notes,omitempty" firestore:"notes,omitempty"`
	// Calendar block written for this departure: "" | simulated | google.
	CalendarBlockID   string `json:"calendar_block_id,omitempty" firestore:"calendar_block_id,omitempty"`
	CalendarBlockMode string `json:"calendar_block_mode,omitempty" firestore:"calendar_block_mode,omitempty"`
}

// Store is the persistence boundary. Implementations must be safe for
// concurrent use.
type Store interface {
	GetUser(ctx context.Context, id string) (User, error)
	PutUser(ctx context.Context, u User) error

	ListTasks(ctx context.Context, userID string) ([]Task, error)
	PutTask(ctx context.Context, userID string, t Task) error
	DeleteTask(ctx context.Context, userID, taskID string) error

	ListProposals(ctx context.Context, userID string) ([]Proposal, error)
	PutProposal(ctx context.Context, p Proposal) error
	GetProposal(ctx context.Context, id string) (Proposal, error)

	PutMandateRecord(ctx context.Context, m MandateRecord) error
	ListMandateRecords(ctx context.Context, userID string) ([]MandateRecord, error)

	ListLedger(ctx context.Context, userID string) ([]LedgerEntry, error)
	PutLedgerEntry(ctx context.Context, e LedgerEntry) error

	ListActionPlans(ctx context.Context, userID string) ([]ActionPlan, error)
	PutActionPlan(ctx context.Context, p ActionPlan) error

	ListBriefing(ctx context.Context, userID, day string) ([]BriefingCard, error)
	PutBriefingCard(ctx context.Context, c BriefingCard) error
}
