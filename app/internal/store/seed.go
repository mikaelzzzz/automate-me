package store

import (
	"context"
	"fmt"
	"time"
)

// DemoUserID is the single user in DEMO_MODE=seed.
const DemoUserID = "demo"

// SeedDemo populates a store with the demo scenario: a busy São Paulo
// professional with a declared routine, one week of ledger history labeled
// "simulated weeks" in the UI (PRD §8), and a drifting action plan for the
// Guardian scene. now anchors relative dates so the demo is stable on any day.
func SeedDemo(ctx context.Context, s Store, now time.Time) error {
	u := User{ID: DemoUserID, Name: "Ana", Mode: "personal", HourlyRateCents: 50_00}
	if err := s.PutUser(ctx, u); err != nil {
		return err
	}

	tasks := []Task{
		{ID: "t-dishes", Name: "Washing dishes after dinner", EstMinutes: 60, FreqPerMon: 30, Source: "interview", Confirmed: true},
		{ID: "t-commute", Name: "Commute to the office", EstMinutes: 55, FreqPerMon: 22, Source: "interview", Confirmed: true},
		{ID: "t-groceries", Name: "Supermarket run", EstMinutes: 90, FreqPerMon: 4.33, Source: "photo", Confirmed: true},
		{ID: "t-bills", Name: "Paying boletos", EstMinutes: 30, FreqPerMon: 4, Source: "photo", Confirmed: true},
		{ID: "t-cleaning", Name: "House cleaning", EstMinutes: 120, FreqPerMon: 4.33, Source: "interview", Confirmed: true},
	}
	for _, t := range tasks {
		if err := s.PutTask(ctx, u.ID, t); err != nil {
			return err
		}
	}

	// four "simulated weeks" of ledger history + the current week
	for i := 4; i >= 1; i-- {
		week := startOfWeek(now.AddDate(0, 0, -7*i))
		entry := LedgerEntry{
			ID:             fmt.Sprintf("led-sim-%d", i),
			UserID:         u.ID,
			WeekStart:      week,
			RecipeID:       "calendar-batching",
			HoursRecovered: 2.5,
			BRLRecovered:   125_00,
			Confirmed:      i > 1, // oldest weeks confirmed, latest still projected
		}
		if err := s.PutLedgerEntry(ctx, entry); err != nil {
			return err
		}
	}

	// a drifting plan for the Guardian demo beat
	plan := ActionPlan{
		ID:              "plan-batching",
		UserID:          u.ID,
		ProposalID:      "prop-batching",
		ExpectedHours:   2.5,
		ExpectedCents:   125_00,
		ExpectedSignals: []SignalType{SignalCalendarBlock},
		Status:          PlanDrifting,
		AdherenceScore:  0.4,
		LastCheckin:     now.AddDate(0, 0, -8),
	}
	return s.PutActionPlan(ctx, plan)
}

func startOfWeek(t time.Time) time.Time {
	t = t.UTC().Truncate(24 * time.Hour)
	for t.Weekday() != time.Monday {
		t = t.AddDate(0, 0, -1)
	}
	return t
}
