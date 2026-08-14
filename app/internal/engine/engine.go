// Package engine is the deterministic Value Engine of Automate.me.
//
// All money math lives here as pure functions over integer centavos.
// LLM agents may call these via tools but never compute money themselves
// (DESIGN_SPEC.md constraint: "Money math ONLY in the deterministic Value Engine").
package engine

import (
	"math"
	"sort"
)

// Task is a recurring routine task as declared by the user (after confirmation).
type Task struct {
	Name         string
	EstMinutes   int     // duration of one occurrence
	FreqPerMonth float64 // occurrences per month (daily ≈ 30.44)
}

// CostOfInactionCents returns the R$/month (in centavos) lost by NOT automating
// the task: minutes × frequency × hourly rate.
func CostOfInactionCents(t Task, hourlyRateCents int64) int64 {
	if t.EstMinutes <= 0 || t.FreqPerMonth <= 0 || hourlyRateCents <= 0 {
		return 0
	}
	hoursPerMonth := float64(t.EstMinutes) / 60.0 * t.FreqPerMonth
	return roundCents(hoursPerMonth * float64(hourlyRateCents))
}

// MinutesPerMonth returns the total minutes/month the task consumes.
func MinutesPerMonth(t Task) float64 {
	if t.EstMinutes <= 0 || t.FreqPerMonth <= 0 {
		return 0
	}
	return float64(t.EstMinutes) * t.FreqPerMonth
}

// Automation is a catalog recipe's cost model applied to a specific task.
type Automation struct {
	Name                string
	UpfrontCents        int64   // one-time cost (0 for subscriptions/services)
	MonthlyRunningCents int64   // recurring cost per month
	MinutesSavedPerOcc  int     // minutes recovered per task occurrence
	FreqPerMonth        float64 // occurrences per month affected
}

// Evaluation is the deterministic verdict on one automation.
type Evaluation struct {
	MonthlySavingsCents int64   // value of time recovered per month
	NetMonthlyCents     int64   // savings − running cost
	PaybackMonths       float64 // upfront ÷ net monthly; 0 when no upfront; +Inf when net ≤ 0
	Proposable          bool    // false when net monthly ≤ 0 (never proposed — PRD §4)
}

// Evaluate applies the PRD §4 formulas:
//
//	payback_months = upfront_cost ÷ (monthly_time_value_recovered − monthly_running_cost)
//
// Zero-upfront automations get PaybackMonths 0 and rank by net monthly savings.
// Negative-net automations are not proposable.
func Evaluate(a Automation, hourlyRateCents int64) Evaluation {
	saved := roundCents(float64(a.MinutesSavedPerOcc) / 60.0 * a.FreqPerMonth * float64(hourlyRateCents))
	if saved < 0 {
		saved = 0
	}
	net := saved - a.MonthlyRunningCents

	ev := Evaluation{MonthlySavingsCents: saved, NetMonthlyCents: net}
	switch {
	case net <= 0:
		ev.PaybackMonths = math.Inf(1)
		ev.Proposable = false
	case a.UpfrontCents <= 0:
		ev.PaybackMonths = 0
		ev.Proposable = true
	default:
		ev.PaybackMonths = float64(a.UpfrontCents) / float64(net)
		ev.Proposable = true
	}
	return ev
}

// Candidate pairs an automation with its evaluation for ranking.
type Candidate struct {
	Automation Automation
	Eval       Evaluation
}

// Rank orders proposable candidates best-first: ascending payback months
// (zero-upfront ⇒ payback 0, i.e. immediate), ties broken by descending net
// monthly savings, then by name for determinism. Non-proposable candidates are
// dropped.
func Rank(cands []Candidate) []Candidate {
	out := make([]Candidate, 0, len(cands))
	for _, c := range cands {
		if c.Eval.Proposable {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Eval.PaybackMonths != b.Eval.PaybackMonths {
			return a.Eval.PaybackMonths < b.Eval.PaybackMonths
		}
		if a.Eval.NetMonthlyCents != b.Eval.NetMonthlyCents {
			return a.Eval.NetMonthlyCents > b.Eval.NetMonthlyCents
		}
		return a.Automation.Name < b.Automation.Name
	})
	return out
}

// BuybackRateCents derives a personal hourly rate from annual income using the
// buyback-rate heuristic: annual ÷ 2,000 working hours ÷ 4.
func BuybackRateCents(annualIncomeCents int64) int64 {
	if annualIncomeCents <= 0 {
		return 0
	}
	return roundCents(float64(annualIncomeCents) / 2000.0 / 4.0)
}

// TrafficCostCents converts a measured congestion delay into money:
// (durationSeconds − staticDurationSeconds) × hourly rate. Negative deltas
// (free-flowing traffic) cost zero.
func TrafficCostCents(durationSec, staticDurationSec int64, hourlyRateCents int64) int64 {
	delta := durationSec - staticDurationSec
	if delta <= 0 || hourlyRateCents <= 0 {
		return 0
	}
	return roundCents(float64(delta) / 3600.0 * float64(hourlyRateCents))
}

func roundCents(v float64) int64 {
	return int64(math.Round(v))
}
