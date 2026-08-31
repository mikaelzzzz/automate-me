package agents

import (
	"context"
	"encoding/json"
	"fmt"

	"automate-me/app/internal/briefing"
	"automate-me/app/internal/catalog"
	"automate-me/app/internal/engine"
	"automate-me/app/internal/proposer"
	"automate-me/app/internal/store"
)

// The tool bodies live here as plain functions of (ctx, userID, input) so two
// front doors can share them without duplicating a line of money logic:
//
//   - the ADK graph, where each one is wrapped in a functiontool and driven by
//     Gemini over adkrest;
//   - the Live API voice session, where the browser talks straight to Gemini
//     and posts the model's function calls back to /app/api/live/tool.
//
// Same code, same Value Engine, same store. Only the transport differs.

func (d Deps) user(ctx context.Context, userID string) (store.User, error) {
	return d.Store.GetUser(ctx, userID)
}

// AddTask upserts a confirmed routine and prices it.
func (d Deps) AddTask(ctx context.Context, userID string, in addTaskIn) (addTaskOut, error) {
	if in.Name == "" || in.Minutes <= 0 || in.TimesPerMonth <= 0 {
		return addTaskOut{}, fmt.Errorf("name, positive minutes and times_per_month are required")
	}
	u, err := d.user(ctx, userID)
	if err != nil {
		return addTaskOut{}, err
	}
	existing, err := d.Store.ListTasks(ctx, u.ID)
	if err != nil {
		return addTaskOut{}, err
	}
	t, updated := upsertTarget(existing, in.TaskID, in.Name)
	t.Name = in.Name
	t.EstMinutes = in.Minutes
	t.FreqPerMon = in.TimesPerMonth
	t.Source = defaultStr(in.Source, defaultStr(t.Source, "interview"))
	t.Confirmed = true
	if err := d.Store.PutTask(ctx, u.ID, t); err != nil {
		return addTaskOut{}, err
	}
	cost := engine.CostOfInactionCents(engine.Task{Name: t.Name, EstMinutes: t.EstMinutes, FreqPerMonth: t.FreqPerMon}, u.HourlyRateCents)
	note := "Created. Value computed deterministically; never estimate money yourself."
	if updated {
		note = "Updated the existing task instead of duplicating it. Value computed deterministically; never estimate money yourself."
	}
	return addTaskOut{TaskID: t.ID, Updated: updated, CostOfInactionMonth: brl(cost), Note: note}, nil
}

// LifePNL is every routine with its monthly hours and Cost of Inaction.
func (d Deps) LifePNL(ctx context.Context, userID string) (pnlOut, error) {
	u, err := d.user(ctx, userID)
	if err != nil {
		return pnlOut{}, err
	}
	tasks, err := d.Store.ListTasks(ctx, u.ID)
	if err != nil {
		return pnlOut{}, err
	}
	out := pnlOut{}
	var totalMin float64
	var totalCents int64
	for _, t := range tasks {
		et := engine.Task{Name: t.Name, EstMinutes: t.EstMinutes, FreqPerMonth: t.FreqPerMon}
		cost := engine.CostOfInactionCents(et, u.HourlyRateCents)
		mins := engine.MinutesPerMonth(et)
		totalMin += mins
		totalCents += cost
		out.Tasks = append(out.Tasks, pnlRow{
			TaskID: t.ID, Name: t.Name, Minutes: t.EstMinutes, PerMonth: t.FreqPerMon,
			HoursMonth: round1(mins / 60), CostMonth: brl(cost),
		})
	}
	out.Total = pnlTotal{HoursMonth: round1(totalMin / 60), CostMonth: brl(totalCents), HourlyRate: brl(u.HourlyRateCents) + "/h"}
	return out, nil
}

// Propose ranks catalog matches for the user's routines.
func (d Deps) Propose(ctx context.Context, userID string, in proposeIn) (proposeOut, error) {
	results, err := proposer.Propose(ctx, d.Store, userID, in.TaskID)
	if err != nil {
		return proposeOut{}, err
	}
	out := proposeOut{Note: "Payback computed deterministically. Executable proposals need explicit user approval before any action."}
	for _, r := range results {
		out.Proposals = append(out.Proposals, proposalRow{
			ProposalID: r.Proposal.ID, Recipe: r.Recipe.Title, Class: string(r.Recipe.Class),
			MonthlySavings: brl(r.Proposal.MonthlySavingsCents), NetMonthly: brl(r.Proposal.NetMonthlyCents),
			PaybackMonths: paybackText(r.Proposal.PaybackMonths),
			Executable:    r.Recipe.Class == catalog.ClassExecutable && r.Recipe.ProductID != "",
		})
	}
	return out, nil
}

// Approve records the user's approval. It never moves money: a purchase still
// has to be signed on the Trusted Surface.
func (d Deps) Approve(ctx context.Context, userID string, in approveIn) (approveOut, error) {
	p, err := d.Store.GetProposal(ctx, in.ProposalID)
	if err != nil {
		return approveOut{}, err
	}
	if p.UserID != userID {
		return approveOut{}, fmt.Errorf("proposal belongs to another user")
	}
	p.Status = store.ProposalApproved
	if err := d.Store.PutProposal(ctx, p); err != nil {
		return approveOut{}, err
	}
	out := approveOut{Status: "approved", Recipe: p.RecipeID}
	for _, r := range catalog.Seed() {
		if r.ID != p.RecipeID {
			continue
		}
		out.Recipe = r.Title
		out.Executable = r.Class == catalog.ClassExecutable && r.ProductID != ""
	}
	if out.Executable {
		out.Next = "Purchase: tell the user to review and sign on the consent screen (the 'Review & sign' button); the agent cannot sign payment mandates."
	} else {
		out.Next = "Guided recipe, nothing to buy: give the user 2-4 concrete setup steps for this recipe right now. Do not mention a consent screen."
	}
	return out, nil
}

// PlanMyDay builds today's briefing.
func (d Deps) PlanMyDay(ctx context.Context, userID string) (briefingOut, error) {
	if d.Briefing == nil {
		return briefingOut{Note: "Maps Platform is not configured on this server (MAPS_API_KEY); tell the user the briefing is unavailable."}, nil
	}
	u, err := d.user(ctx, userID)
	if err != nil {
		return briefingOut{}, err
	}
	day := d.Briefing.DayFor(d.Briefing.Now())
	cards := d.Briefing.Build(ctx, u.ID, u.HourlyRateCents, briefing.DemoAppointments(day, d.Briefing.Loc))
	for _, c := range cards {
		if err := d.Store.PutBriefingCard(ctx, c); err != nil {
			return briefingOut{}, err
		}
	}
	return briefingOut{
		Day: d.Briefing.DayKey(day), Cards: d.rows(cards),
		Note: "Numbers are measured (Routes/Weather/GeoSampa), not estimated. Offer to write the departure blocks to the calendar.",
	}, nil
}

// GetBriefing reads the briefing already built for the day being planned.
func (d Deps) GetBriefing(ctx context.Context, userID string) (briefingOut, error) {
	if d.Briefing == nil {
		return briefingOut{Note: "Maps Platform is not configured on this server."}, nil
	}
	day := d.Briefing.DayFor(d.Briefing.Now())
	cards, err := d.Store.ListBriefing(ctx, userID, d.Briefing.DayKey(day))
	if err != nil {
		return briefingOut{}, err
	}
	if len(cards) == 0 {
		return briefingOut{Day: d.Briefing.DayKey(day), Note: "Nothing built yet — call plan_my_day."}, nil
	}
	return briefingOut{Day: d.Briefing.DayKey(day), Cards: d.rows(cards)}, nil
}

// WriteBlocks writes "Leave at HH:MM" blocks for the briefed appointments.
func (d Deps) WriteBlocks(ctx context.Context, userID string, in blocksIn) (blocksOut, error) {
	if d.Briefing == nil || d.Blocks == nil {
		return blocksOut{Note: "Calendar writing is not configured."}, nil
	}
	day := d.Briefing.DayKey(d.Briefing.DayFor(d.Briefing.Now()))
	cards, err := d.Store.ListBriefing(ctx, userID, day)
	if err != nil {
		return blocksOut{}, err
	}
	want := map[string]bool{}
	for _, id := range in.CardIDs {
		want[id] = true
	}
	out := blocksOut{}
	for _, c := range cards {
		if len(want) > 0 && !want[c.ID] {
			continue
		}
		id, mode, err := d.Blocks.WriteDepartureBlock(ctx, c)
		if err != nil {
			return blocksOut{}, err
		}
		c.CalendarBlockID, c.CalendarBlockMode = id, mode
		if err := d.Store.PutBriefingCard(ctx, c); err != nil {
			return blocksOut{}, err
		}
		out.Written = append(out.Written, c.EventSummary+" · leave "+c.DepartureTime.In(c.EventStart.Location()).Format("15:04"))
		out.Mode = mode
	}
	if out.Mode == "simulated" {
		out.Note = "No Google Calendar connected on this server: blocks recorded in-app and labelled simulated."
	}
	return out, nil
}

// --- live registry -----------------------------------------------------------

// LiveTool is one function the Live API voice session may call. Declaration is
// the JSON-Schema the model sees; Invoke runs the same body the ADK graph runs.
type LiveTool struct {
	Declaration map[string]any
	Invoke      func(ctx context.Context, userID string, args json.RawMessage) (any, error)
}

func decode[T any](args json.RawMessage) (T, error) {
	var in T
	if len(args) == 0 || string(args) == "null" {
		return in, nil
	}
	err := json.Unmarshal(args, &in)
	return in, err
}

func obj(props map[string]any, required ...string) map[string]any {
	m := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}

func prop(t, desc string) map[string]any { return map[string]any{"type": t, "description": desc} }

// LiveTools is the tool set handed to a Live session. It deliberately excludes
// anything that signs money: a voice command can approve a proposal, but the
// purchase itself still goes through the non-agentic Trusted Surface.
func (d Deps) LiveTools() map[string]LiveTool {
	return map[string]LiveTool{
		"get_life_pnl": {
			Declaration: map[string]any{
				"name":        "get_life_pnl",
				"description": "The user's Life P&L: every routine with its monthly hours and Cost of Inaction, plus totals. Call this before answering anything about where their time or money goes.",
				"parameters":  obj(map[string]any{}),
			},
			Invoke: func(ctx context.Context, uid string, _ json.RawMessage) (any, error) { return d.LifePNL(ctx, uid) },
		},
		"add_routine_task": {
			Declaration: map[string]any{
				"name":        "add_routine_task",
				"description": "Save a routine the user just described and confirmed, and get back its monthly Cost of Inaction. Upserts: pass task_id from get_life_pnl to update an existing routine instead of duplicating it.",
				"parameters": obj(map[string]any{
					"task_id":                prop("string", "Existing task_id from get_life_pnl to update; omit to create"),
					"name":                   prop("string", "Short routine name, e.g. 'Washing dishes after dinner'"),
					"minutes_per_occurrence": prop("integer", "Minutes each time it happens, confirmed with the user"),
					"times_per_month":        prop("number", "Occurrences per month (daily=30, weekly=4.33)"),
					"source":                 prop("string", "Where it came from: interview, photo or calendar"),
				}, "name", "minutes_per_occurrence", "times_per_month"),
			},
			Invoke: func(ctx context.Context, uid string, a json.RawMessage) (any, error) {
				in, err := decode[addTaskIn](a)
				if err != nil {
					return nil, err
				}
				return d.AddTask(ctx, uid, in)
			},
		},
		"propose_automations": {
			Declaration: map[string]any{
				"name":        "propose_automations",
				"description": "Match the user's routines against the automation catalog and rank them by net monthly savings. The deterministic Value Engine does the arithmetic.",
				"parameters":  obj(map[string]any{"task_id": prop("string", "Only this routine; omit for all")}),
			},
			Invoke: func(ctx context.Context, uid string, a json.RawMessage) (any, error) {
				in, err := decode[proposeIn](a)
				if err != nil {
					return nil, err
				}
				return d.Propose(ctx, uid, in)
			},
		},
		"approve_proposal": {
			Declaration: map[string]any{
				"name":        "approve_proposal",
				"description": "Record the user's spoken approval of a proposal. Ask them to confirm out loud first. This never moves money: a purchase still has to be signed by the user on the consent screen.",
				"parameters":  obj(map[string]any{"proposal_id": prop("string", "proposal_id from propose_automations")}, "proposal_id"),
			},
			Invoke: func(ctx context.Context, uid string, a json.RawMessage) (any, error) {
				in, err := decode[approveIn](a)
				if err != nil {
					return nil, err
				}
				return d.Approve(ctx, uid, in)
			},
		},
		"plan_my_day": {
			Declaration: map[string]any{
				"name":        "plan_my_day",
				"description": "Build the Daily Briefing: one route worker per appointment, departure times from live traffic, traffic priced at the user's hourly rate, weather at departure, and flood risk from live alerts plus São Paulo's logged flooding history.",
				"parameters":  obj(map[string]any{}),
			},
			Invoke: func(ctx context.Context, uid string, _ json.RawMessage) (any, error) { return d.PlanMyDay(ctx, uid) },
		},
		"get_daily_briefing": {
			Declaration: map[string]any{
				"name":        "get_daily_briefing",
				"description": "Read the briefing already built for the day being planned. Prefer this over plan_my_day when one already exists.",
				"parameters":  obj(map[string]any{}),
			},
			Invoke: func(ctx context.Context, uid string, _ json.RawMessage) (any, error) { return d.GetBriefing(ctx, uid) },
		},
		"write_departure_blocks": {
			Declaration: map[string]any{
				"name":        "write_departure_blocks",
				"description": "Write 'Leave at HH:MM' blocks to the user's calendar for the briefed appointments. Ask for a spoken yes first.",
				"parameters": obj(map[string]any{
					"card_ids": map[string]any{
						"type": "array", "items": map[string]any{"type": "string"},
						"description": "card_id values to write; omit for all",
					},
				}),
			},
			Invoke: func(ctx context.Context, uid string, a json.RawMessage) (any, error) {
				in, err := decode[blocksIn](a)
				if err != nil {
					return nil, err
				}
				return d.WriteBlocks(ctx, uid, in)
			},
		},
	}
}

// LiveToolOrder fixes the order the declarations are sent in, so the model
// sees the read-only tools before the ones that change something.
var LiveToolOrder = []string{
	"get_life_pnl", "get_daily_briefing", "add_routine_task",
	"propose_automations", "plan_my_day", "approve_proposal", "write_departure_blocks",
}

// LiveSystemInstruction is what the voice model is told. It mirrors the graph's
// division of labour, in one voice instead of four.
const LiveSystemInstruction = `You are Automate.me, speaking with the user out loud.

Your job: find where their life leaks time, price it in Brazilian reais, and automate the worst leaks.

Rules that do not bend:
- Every monetary or time figure comes from a tool. Never compute or estimate money yourself.
- Before saving a routine, say your estimate for how long it takes and how often, and wait for them to confirm or correct it.
- You may record an approval by voice, but you can never buy anything. Purchases are signed by the user on the consent screen. Say so plainly when a proposal is a purchase.
- Call get_life_pnl before you talk about their overall numbers, and get_daily_briefing before you talk about their day.

How to speak: this is a conversation, not a report. Short sentences. Lead with the number that matters. No markdown, no bullet lists, no reading identifiers out loud. Under 60 words unless they ask for detail. Match the user's language.`
