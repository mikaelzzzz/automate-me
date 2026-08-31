// Package profile holds the one thing the product cannot price anything
// without: what an hour of this person's life is worth, and where their day
// starts.
//
// It is shared by the three doors — the onboarding screen (REST), the typed
// chat and the voice session — so the rate can be set by talking or by typing
// and lands in exactly the same place. Changing the rate re-prices everything
// already on record, because a Life P&L computed at an old rate is a wrong
// number, not an old one.
package profile

import (
	"context"
	"fmt"
	"strings"

	"automate-me/app/internal/engine"
	"automate-me/app/internal/proposer"
	"automate-me/app/internal/store"
)

// Input is what onboarding collects. Every field is optional: a partial edit
// (just the work address, say) leaves the rest untouched.
type Input struct {
	Name string `json:"name,omitempty"`
	// HourlyRateCents prices the hour directly.
	HourlyRateCents int64 `json:"hourly_rate_cents,omitempty"`
	// MonthlyIncomeCents + HoursPerWeek derive it instead. When both routes
	// are given, the declared rate wins — the user typed it on purpose.
	MonthlyIncomeCents int64   `json:"monthly_income_cents,omitempty"`
	HoursPerWeek       float64 `json:"hours_per_week,omitempty"`

	HomeAddress string `json:"home_address,omitempty"`
	WorkAddress string `json:"work_address,omitempty"`
	WorkSetup   string `json:"work_setup,omitempty"` // remote | hybrid | onsite
}

// Summary is what the whole record is worth at the current rate — the number
// the confirm step shows, and the one the agent quotes back.
type Summary struct {
	User store.User `json:"user"`
	// Tasks tracked, and what they cost per month at this rate.
	Tasks             int     `json:"tasks"`
	HoursPerMonth     float64 `json:"hours_per_month"`
	CostOfInaction    int64   `json:"cost_of_inaction_cents"`
	Proposals         int     `json:"proposals"`
	BestSavingsCents  int64   `json:"best_monthly_savings_cents"`
	PreviousRateCents int64   `json:"previous_hourly_rate_cents,omitempty"`
	// CostDeltaCents is how much the re-priced record moved. Positive means
	// the same routine is now worth more per month than it was.
	CostDeltaCents int64 `json:"cost_delta_cents,omitempty"`
}

// Get reads the profile and prices the record at the current rate.
func Get(ctx context.Context, s store.Store, userID string) (Summary, error) {
	u, err := s.GetUser(ctx, userID)
	if err != nil {
		return Summary{}, err
	}
	return summarize(ctx, s, u, 0)
}

// Apply validates an edit, saves it, and re-prices everything that was already
// on record. It returns the fresh numbers so the caller never has to guess
// what the change did.
func Apply(ctx context.Context, s store.Store, userID string, in Input) (Summary, error) {
	u, err := s.GetUser(ctx, userID)
	if err != nil {
		return Summary{}, err
	}
	before := u.HourlyRateCents

	if n := strings.TrimSpace(in.Name); n != "" {
		u.Name = n
	}
	switch {
	case in.HourlyRateCents > 0:
		u.HourlyRateCents = in.HourlyRateCents
		u.RateBasis = "declared"
		// Keep the income on file only while it still explains the rate.
		u.MonthlyIncomeCents, u.HoursPerWeek = 0, 0
	case in.MonthlyIncomeCents > 0 || in.HoursPerWeek > 0:
		income, hours := in.MonthlyIncomeCents, in.HoursPerWeek
		if income == 0 {
			income = u.MonthlyIncomeCents
		}
		if hours == 0 {
			hours = u.HoursPerWeek
		}
		rate := engine.HourlyRateFromIncome(income, hours)
		if rate <= 0 {
			return Summary{}, fmt.Errorf("monthly income and hours per week must both be positive (got %d centavos over %v h/week)", income, hours)
		}
		u.HourlyRateCents, u.MonthlyIncomeCents, u.HoursPerWeek = rate, income, hours
		u.RateBasis = "income"
	}
	if u.HourlyRateCents <= 0 {
		return Summary{}, fmt.Errorf("an hourly rate is required: give hourly_rate_cents, or monthly_income_cents with hours_per_week")
	}

	if a := strings.TrimSpace(in.HomeAddress); a != "" {
		u.HomeAddress = a
	}
	if a := strings.TrimSpace(in.WorkAddress); a != "" {
		u.WorkAddress = a
	}
	if w := strings.ToLower(strings.TrimSpace(in.WorkSetup)); w != "" {
		switch w {
		case "remote", "hybrid", "onsite":
			u.WorkSetup = w
		default:
			return Summary{}, fmt.Errorf("work_setup must be remote, hybrid or onsite (got %q)", in.WorkSetup)
		}
	}
	// Working from home means there is no commute to route or to price.
	if u.WorkSetup == "remote" {
		u.WorkAddress = ""
	}
	u.Onboarded = true

	if err := s.PutUser(ctx, u); err != nil {
		return Summary{}, err
	}
	// Proposals carry money computed at the rate of the moment they were made.
	// Re-run the matcher so nothing on screen is priced at a rate the user has
	// just replaced.
	if _, err := proposer.Propose(ctx, s, userID, ""); err != nil {
		return Summary{}, fmt.Errorf("re-pricing proposals at the new rate: %w", err)
	}
	return summarize(ctx, s, u, before)
}

func summarize(ctx context.Context, s store.Store, u store.User, previousRate int64) (Summary, error) {
	tasks, err := s.ListTasks(ctx, u.ID)
	if err != nil {
		return Summary{}, err
	}
	out := Summary{User: u, Tasks: len(tasks)}
	var minutes float64
	for _, t := range tasks {
		et := engine.Task{Name: t.Name, EstMinutes: t.EstMinutes, FreqPerMonth: t.FreqPerMon}
		minutes += engine.MinutesPerMonth(et)
		out.CostOfInaction += engine.CostOfInactionCents(et, u.HourlyRateCents)
		if previousRate > 0 {
			out.CostDeltaCents += engine.CostOfInactionCents(et, u.HourlyRateCents) - engine.CostOfInactionCents(et, previousRate)
		}
	}
	out.HoursPerMonth = float64(int64(minutes/60*10+0.5)) / 10
	if previousRate > 0 && previousRate != u.HourlyRateCents {
		out.PreviousRateCents = previousRate
	} else {
		out.CostDeltaCents = 0
	}

	props, err := s.ListProposals(ctx, u.ID)
	if err != nil {
		return Summary{}, err
	}
	out.Proposals = len(props)
	for _, p := range props {
		if p.MonthlySavingsCents > out.BestSavingsCents {
			out.BestSavingsCents = p.MonthlySavingsCents
		}
	}
	return out, nil
}
