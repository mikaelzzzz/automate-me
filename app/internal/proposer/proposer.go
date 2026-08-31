// Package proposer turns a user's routines into ranked automation proposals.
// One implementation, two callers: the agent's propose_automations tool and
// the demo seed — so what the demo shows is what the agent computes.
package proposer

import (
	"context"
	"sort"

	"automate-me/app/internal/catalog"
	"automate-me/app/internal/engine"
	"automate-me/app/internal/store"
)

// Result pairs a persisted proposal with the recipe it came from.
type Result struct {
	Proposal store.Proposal
	Recipe   catalog.Recipe
}

// Propose matches every task (or just taskID, when set) against the catalog,
// prices each match with the deterministic Value Engine, persists the
// proposable ones and returns them ranked: biggest net monthly recovery
// first, ties broken by faster payback.
//
// Existing proposals are never downgraded — a proposal the user already
// approved or executed keeps its status.
func Propose(ctx context.Context, st store.Store, userID, taskID string) ([]Result, error) {
	u, err := st.GetUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	tasks, err := st.ListTasks(ctx, userID)
	if err != nil {
		return nil, err
	}
	recipes := catalog.Seed()

	var out []Result
	for _, t := range tasks {
		if taskID != "" && t.ID != taskID {
			continue
		}
		for _, r := range catalog.Match(t.Name, recipes) {
			if r.Class == catalog.ClassRoadmap {
				continue
			}
			ev := engine.Evaluate(engine.Automation{
				Name:                r.ID,
				UpfrontCents:        r.Cost.UpfrontCents,
				MonthlyRunningCents: r.Cost.MonthlyRunningCents,
				MinutesSavedPerOcc:  min(r.Cost.MinutesSavedPerOcc, t.EstMinutes),
				FreqPerMonth:        t.FreqPerMon,
			}, u.HourlyRateCents)
			if !ev.Proposable {
				continue
			}
			p := store.Proposal{
				ID:     "prop-" + t.ID + "-" + r.ID,
				UserID: userID, TaskID: t.ID, RecipeID: r.ID,
				MonthlySavingsCents: ev.MonthlySavingsCents,
				NetMonthlyCents:     ev.NetMonthlyCents,
				PaybackMonths:       ev.PaybackMonths,
				Status:              store.ProposalProposed,
			}
			// keep whatever the user already decided about this proposal
			if existing, err := st.GetProposal(ctx, p.ID); err == nil {
				p.Status = existing.Status
				p.CreatedAt = existing.CreatedAt
			}
			if err := st.PutProposal(ctx, p); err != nil {
				return nil, err
			}
			out = append(out, Result{Proposal: p, Recipe: r})
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].Proposal, out[j].Proposal
		if a.NetMonthlyCents != b.NetMonthlyCents {
			return a.NetMonthlyCents > b.NetMonthlyCents
		}
		return a.PaybackMonths < b.PaybackMonths
	})
	return out, nil
}
