package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"automate-me/app/internal/catalog"
	"automate-me/app/internal/engine"
	"automate-me/app/internal/profile"
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

// GetProfile answers what the app knows about the person and what their
// record is worth at the current rate.
func (d Deps) GetProfile(ctx context.Context, userID string) (profileOut, error) {
	sum, err := profile.Get(ctx, d.Store, userID)
	if err != nil {
		return profileOut{}, err
	}
	return profileView(sum, "This is what every number in the product is priced from."), nil
}

// SetProfile records what the user just told us — the price of their hour,
// where they live and work — and re-prices everything already tracked.
func (d Deps) SetProfile(ctx context.Context, userID string, in profile.Input) (profileOut, error) {
	sum, err := profile.Apply(ctx, d.Store, userID, in)
	if err != nil {
		return profileOut{}, err
	}
	note := "Saved. Everything already tracked was re-priced at this rate by the Value Engine."
	if sum.CostDeltaCents != 0 {
		note += fmt.Sprintf(" The same routine is now worth %s/month %s than before.",
			brl(abs64(sum.CostDeltaCents)), moreOrLess(sum.CostDeltaCents))
	}
	return profileView(sum, note), nil
}

type profileOut struct {
	Name           string  `json:"name,omitempty"`
	HourlyRate     string  `json:"hourly_rate"`
	RateBasis      string  `json:"rate_basis,omitempty"`
	MonthlyIncome  string  `json:"monthly_income,omitempty"`
	HoursPerWeek   float64 `json:"hours_per_week,omitempty"`
	HomeAddress    string  `json:"home_address,omitempty"`
	WorkAddress    string  `json:"work_address,omitempty"`
	WorkSetup      string  `json:"work_setup,omitempty"`
	Onboarded      bool    `json:"onboarded"`
	Tasks          int     `json:"tasks_tracked"`
	HoursPerMonth  float64 `json:"hours_per_month"`
	CostOfInaction string  `json:"cost_of_inaction_per_month"`
	Note           string  `json:"note"`
}

func profileView(sum profile.Summary, note string) profileOut {
	u := sum.User
	out := profileOut{
		Name: u.Name, HourlyRate: brl(u.HourlyRateCents) + "/h", RateBasis: u.RateBasis,
		HoursPerWeek: u.HoursPerWeek, HomeAddress: u.HomeAddress, WorkAddress: u.WorkAddress,
		WorkSetup: u.WorkSetup, Onboarded: u.Onboarded, Tasks: sum.Tasks,
		HoursPerMonth: sum.HoursPerMonth, CostOfInaction: brl(sum.CostOfInaction), Note: note,
	}
	if u.MonthlyIncomeCents > 0 {
		out.MonthlyIncome = brl(u.MonthlyIncomeCents) + "/month"
	}
	return out
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func moreOrLess(v int64) string {
	if v < 0 {
		return "less"
	}
	return "more"
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
		// A voice model that never listed the proposals will guess an id. Say
		// what actually exists so it can retry instead of apologising.
		resolved, alt := d.resolveProposal(ctx, userID, in.ProposalID)
		if alt != "" {
			return approveOut{}, fmt.Errorf("no proposal %q. %s", in.ProposalID, alt)
		}
		p = resolved
	}
	if p.UserID != userID {
		return approveOut{}, fmt.Errorf("proposal belongs to another user")
	}
	p.Status = store.ProposalApproved
	if err := d.Store.PutProposal(ctx, p); err != nil {
		return approveOut{}, err
	}
	out := approveOut{Status: "approved", Recipe: p.RecipeID}
	var productID string
	for _, r := range catalog.Seed() {
		if r.ID != p.RecipeID {
			continue
		}
		out.Recipe = r.Title
		out.Executable = r.Class == catalog.ClassExecutable && r.ProductID != ""
		productID = r.ProductID
	}
	switch {
	case !out.Executable:
		out.Next = "Guided recipe, nothing to buy: give the user 2-4 concrete setup steps for this recipe right now. Do not mention a consent screen."
	default:
		// Try to complete it under the standing authorization the user signed.
		// The tool cannot sign anything: it asks the Trusted Surface, which
		// checks the envelope against the merchant-signed total and refuses on
		// its own terms.
		out.Next = "Purchase: tell the user to review and sign on the consent screen (the 'Review & sign' button); the agent cannot sign payment mandates."
		if d.Trusted == nil {
			break
		}
		res, err := d.Trusted.ExecuteAutonomousPurchase(ctx, userID, p.ID, productID, 1)
		switch {
		case err != nil:
			// The rail failed. The approval stands; fall back to consent.
			slog.Error("autonomous purchase failed", "user", userID, "proposal", p.ID, "err", err)
		case res.Completed:
			out.Purchased = true
			out.PurchaseTotal = brl(res.Checkout.Total.Amount)
			out.MandateRef = res.MandateRecordID
			out.Next = "Bought. Tell the user you completed the purchase yourself under the spending authorization they signed, name the amount, and say the signed receipt is in the ledger. Do not mention a consent screen — there was none."
		case res.NeedsConsent && res.Checkout.Total.Amount > 0:
			out.Next = "Above the authorized amount (" + brl(res.Checkout.Total.Amount) + "): tell the user this one is over the limit they set, so it needs their signature on the consent screen ('Review & sign')."
		}
	}
	return out, nil
}

// resolveProposal is the fallback when an id does not exist: match loosely on
// the recipe, and if that is ambiguous or empty, return a message naming the
// real options.
func (d Deps) resolveProposal(ctx context.Context, userID, want string) (store.Proposal, string) {
	all, err := d.Store.ListProposals(ctx, userID)
	if err != nil || len(all) == 0 {
		return store.Proposal{}, "The user has no proposals yet — call propose_automations first."
	}
	needle := strings.ToLower(strings.NewReplacer("prop-", "", "_", " ", "-", " ").Replace(want))
	var hits []store.Proposal
	for _, p := range all {
		hay := strings.ToLower(strings.ReplaceAll(p.RecipeID, "-", " "))
		if needle != "" && (strings.Contains(needle, hay) || strings.Contains(hay, needle)) {
			hits = append(hits, p)
		}
	}
	if len(hits) == 1 {
		return hits[0], ""
	}
	options := make([]string, 0, len(all))
	for _, p := range all {
		options = append(options, fmt.Sprintf("%s (%s)", p.ID, p.RecipeID))
	}
	return store.Proposal{}, "Call propose_automations first, or use one of: " + strings.Join(options, ", ")
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
	sched, err := d.Briefing.Schedule(ctx, d.Events, day, u.HomeAddress)
	if err != nil {
		return briefingOut{Note: "The calendar could not be read: " + err.Error()}, nil
	}
	cards := d.Briefing.Build(ctx, u.ID, u.HourlyRateCents, sched.Events)
	for _, c := range cards {
		if err := d.Store.PutBriefingCard(ctx, c); err != nil {
			return briefingOut{}, err
		}
	}
	note := sched.Note + " Numbers are measured (Routes/Weather/GeoSampa), not estimated."
	if len(cards) > 0 {
		note += " Offer to write the departure blocks to the calendar."
	} else {
		note += " Say plainly that no trip is planned today instead of inventing one."
	}
	return briefingOut{Day: d.Briefing.DayKey(day), Cards: d.rows(cards), Note: note}, nil
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

// Consultation is the graph's answer to a spoken question: the text it
// produced and which specialists it went through to get there.
type Consultation struct {
	Answer   string   `json:"answer"`
	Handled  []string `json:"handled_by"`
	Model    string   `json:"reasoned_with"`
	ToolsRun []string `json:"tools_run,omitempty"`
}

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
	tools := map[string]LiveTool{
		"consult_specialist": {
			Declaration: map[string]any{
				"name":        "consult_specialist",
				"description": "Hand a question to the Automate.me specialist graph — the Routine Analyst, the Automation Advisor and the Day Planner, reasoning on Gemini 3.5 Flash. Use it for anything that needs judgement rather than a lookup: what to automate and why, how to set a recipe up, comparing options, explaining a number. Returns their answer for you to say out loud in your own words.",
				"parameters": obj(map[string]any{
					"question": prop("string", "The user's question, in their own words, with any context from the conversation they would expect you to carry over"),
				}, "question"),
			},
			Invoke: func(ctx context.Context, uid string, a json.RawMessage) (any, error) {
				in, err := decode[consultIn](a)
				if err != nil {
					return nil, err
				}
				if d.Consult == nil {
					return nil, fmt.Errorf("the specialist graph is not available on this server")
				}
				if strings.TrimSpace(in.Question) == "" {
					return nil, fmt.Errorf("question is required")
				}
				return d.Consult(ctx, uid, in.Question)
			},
		},
		"find_products": {
			Declaration: map[string]any{
				"name":        "find_products",
				"description": "Search the live web for something the user wants to buy and come back with real options: product, store, current price and a link. Use it whenever they ask what to buy, what something costs, or where to get it — you have no product knowledge of your own and prices change. Read the options out loud; the links appear on their screen.",
				"parameters": obj(map[string]any{
					"need": prop("string", "What they want, in their words, with the constraints they gave: budget, brand, size, whether it is for a routine you already priced"),
				}, "need"),
			},
			Invoke: func(ctx context.Context, uid string, a json.RawMessage) (any, error) {
				in, err := decode[findProductsIn](a)
				if err != nil {
					return nil, err
				}
				if d.Consult == nil {
					return nil, fmt.Errorf("the specialist graph is not available on this server")
				}
				if strings.TrimSpace(in.Need) == "" {
					return nil, fmt.Errorf("need is required")
				}
				// Straight to the scout: it is the only agent with Google Search,
				// and naming it keeps the orchestrator from answering from memory.
				return d.Consult(ctx, uid, "Ask product_scout to search the web and find products for this, with current prices and links: "+in.Need)
			},
		},
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
		"get_profile": {
			Declaration: map[string]any{
				"name":        "get_profile",
				"description": "What the app knows about the user: the price of their hour, how that price was set, where they live and work, and what their tracked routine costs per month at that rate.",
				"parameters":  obj(map[string]any{}),
			},
			Invoke: func(ctx context.Context, uid string, _ json.RawMessage) (any, error) { return d.GetProfile(ctx, uid) },
		},
		"set_profile": {
			Declaration: map[string]any{
				"name":        "set_profile",
				"description": "Record what the user just told you about themselves and re-price everything at the new rate. Price the hour either directly (hourly_rate_reais) or from what they earn (monthly_income_reais with hours_per_week) — never both. Only send the fields they actually gave you.",
				"parameters": obj(map[string]any{
					"name":                 map[string]any{"type": "string", "description": "what to call them"},
					"hourly_rate_reais":    map[string]any{"type": "number", "description": "what one hour of their time is worth, in reais"},
					"monthly_income_reais": map[string]any{"type": "number", "description": "what they earn in a month, in reais"},
					"hours_per_week":       map[string]any{"type": "number", "description": "hours they work in a week — required with monthly_income_reais"},
					"home_address":         map[string]any{"type": "string", "description": "where the day starts; the briefing routes from here"},
					"work_address":         map[string]any{"type": "string", "description": "where they work, when they go somewhere"},
					"work_setup":           map[string]any{"type": "string", "enum": []string{"remote", "hybrid", "onsite"}},
				}),
			},
			Invoke: func(ctx context.Context, uid string, a json.RawMessage) (any, error) {
				in, err := decode[setProfileIn](a)
				if err != nil {
					return nil, err
				}
				return d.SetProfile(ctx, uid, in.toInput())
			},
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
	// Without a graph runner the delegation tool would only ever fail; do not
	// offer the model something it cannot use.
	if d.Consult == nil {
		delete(tools, "consult_specialist")
		delete(tools, "find_products")
	}
	return tools
}

type consultIn struct {
	Question string `json:"question"`
}

type findProductsIn struct {
	Need string `json:"need"`
}

// LiveToolOrder fixes the order the declarations are sent in, so the model
// sees the read-only tools before the ones that change something.
var LiveToolOrder = []string{
	"consult_specialist", "find_products", "get_profile", "get_life_pnl", "get_daily_briefing", "add_routine_task",
	"set_profile", "propose_automations", "plan_my_day", "approve_proposal", "write_departure_blocks",
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
- Never invent an identifier. Before approving anything, call propose_automations so you are holding a real proposal_id.
- You cannot browse and you know nothing about products, prices or stock. Anything to buy — "where do I get one", "how much is it", "which model", a link — goes to find_products, which searches the live web through the Product Scout. Read the options out loud: product, store, price. Say the links are on their screen; never spell a URL out loud, and never invent one.
- You are the voice, not the brain. Anything that needs judgement — what to automate and why, how to set something up, comparing options, explaining a number, or a question you are unsure how to answer — goes to consult_specialist, which reasons on Gemini 3.5 Flash across the Routine Analyst, the Automation Advisor and the Day Planner. Say their answer in your own words, out loud and short. Use the direct tools only for a plain lookup or to carry out something the user just asked for.

How to speak: this is a conversation, not a report. Short sentences. Lead with the number that matters. No markdown, no bullet lists, no reading identifiers out loud. Under 60 words unless they ask for detail. Match the user's language.`
