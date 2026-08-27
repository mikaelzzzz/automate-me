// Package agents builds the Automate.me agent graph (design §2): a ModeChat
// orchestrator with specialist sub-agents. Tools wrap the deterministic
// modules — the LLM converses and routes; engine/catalog/store compute.
package agents

import (
	"fmt"
	"sort"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"

	"automate-me/app/internal/catalog"
	"automate-me/app/internal/engine"
	"automate-me/app/internal/store"
)

// Deps carries the deterministic modules the tools operate on.
type Deps struct {
	Store store.Store
	// UserID resolves the acting user. Demo scope: fixed demo user; multi-user
	// auth swaps this for a session-state lookup.
	UserID func(agent.Context) string
}

// brl formats integer centavos as "R$3,366.08" (en-US grouping, matching the
// SPA). Money never touches floats.
func brl(cents int64) string {
	neg := cents < 0
	if neg {
		cents = -cents
	}
	whole := fmt.Sprintf("%d", cents/100)
	var b []byte
	for i, c := range []byte(whole) {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b = append(b, ',')
		}
		b = append(b, c)
	}
	sign := ""
	if neg {
		sign = "-"
	}
	return fmt.Sprintf("%sR$%s.%02d", sign, b, cents%100)
}

func (d Deps) userRate(ctx agent.Context) (store.User, error) {
	return d.Store.GetUser(ctx, d.UserID(ctx))
}

// --- tools -----------------------------------------------------------------

type addTaskIn struct {
	TaskID        string  `json:"task_id,omitempty" jsonschema:"Existing task_id from get_life_pnl to update instead of creating a duplicate; empty to create"`
	Name          string  `json:"name" jsonschema:"Short task name, e.g. 'Washing dishes after dinner'"`
	Minutes       int     `json:"minutes_per_occurrence" jsonschema:"Estimated minutes per occurrence, confirmed with the user"`
	TimesPerMonth float64 `json:"times_per_month" jsonschema:"Occurrences per month (daily=30, weekly=4.33)"`
	Source        string  `json:"source" jsonschema:"Where it came from: interview, photo or calendar"`
}
type addTaskOut struct {
	TaskID              string `json:"task_id"`
	Updated             bool   `json:"updated_existing"`
	CostOfInactionMonth string `json:"cost_of_inaction_per_month"`
	Note                string `json:"note"`
}

func (d Deps) addTask() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "add_routine_task",
		Description: "Persist a routine task the user confirmed (name, minutes, frequency). Upserts: pass task_id (from get_life_pnl) to update an existing task; a task with the same name is updated, never duplicated. Returns the monthly Cost of Inaction computed by the deterministic Value Engine.",
	}, func(ctx agent.Context, in addTaskIn) (addTaskOut, error) {
		if in.Name == "" || in.Minutes <= 0 || in.TimesPerMonth <= 0 {
			return addTaskOut{}, fmt.Errorf("name, positive minutes and times_per_month are required")
		}
		u, err := d.userRate(ctx)
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
		return addTaskOut{
			TaskID:              t.ID,
			Updated:             updated,
			CostOfInactionMonth: brl(cost),
			Note:                note,
		}, nil
	})
}

type pnlOut struct {
	Tasks []pnlRow `json:"tasks"`
	Total pnlTotal `json:"total"`
}
type pnlRow struct {
	TaskID     string  `json:"task_id"`
	Name       string  `json:"name"`
	Minutes    int     `json:"minutes_per_occurrence"`
	PerMonth   float64 `json:"times_per_month"`
	HoursMonth float64 `json:"hours_per_month"`
	CostMonth  string  `json:"cost_per_month"`
}
type pnlTotal struct {
	HoursMonth float64 `json:"hours_per_month"`
	CostMonth  string  `json:"cost_per_month"`
	HourlyRate string  `json:"hourly_rate"`
}

func (d Deps) lifePNL() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "get_life_pnl",
		Description: "The user's Life P&L: every routine task with its monthly hours and Cost of Inaction, plus totals.",
	}, func(ctx agent.Context, _ struct{}) (pnlOut, error) {
		u, err := d.userRate(ctx)
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
	})
}

type proposeIn struct {
	TaskID string `json:"task_id" jsonschema:"Task to find automations for; empty means all tasks"`
}
type proposalRow struct {
	ProposalID     string `json:"proposal_id"`
	Recipe         string `json:"recipe"`
	Class          string `json:"class"`
	MonthlySavings string `json:"monthly_savings"`
	NetMonthly     string `json:"net_monthly"`
	PaybackMonths  string `json:"payback_months"`
	Executable     bool   `json:"agent_can_execute"`

	netCents int64   // sort keys, not serialised
	payback  float64 //
}
type proposeOut struct {
	Proposals []proposalRow `json:"proposals"`
	Note      string        `json:"note"`
}

func (d Deps) proposeAutomations() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "propose_automations",
		Description: "Match the user's tasks against the automation catalog, rank by payback (deterministic engine) and persist proposals. Negative-net automations are never proposed.",
	}, func(ctx agent.Context, in proposeIn) (proposeOut, error) {
		u, err := d.userRate(ctx)
		if err != nil {
			return proposeOut{}, err
		}
		tasks, err := d.Store.ListTasks(ctx, u.ID)
		if err != nil {
			return proposeOut{}, err
		}
		recipes := catalog.Seed()
		out := proposeOut{Note: "Payback computed deterministically. Executable proposals need explicit user approval before any action."}
		for _, t := range tasks {
			if in.TaskID != "" && t.ID != in.TaskID {
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
					UserID: u.ID, TaskID: t.ID, RecipeID: r.ID,
					MonthlySavingsCents: ev.MonthlySavingsCents,
					NetMonthlyCents:     ev.NetMonthlyCents,
					PaybackMonths:       ev.PaybackMonths,
					Status:              store.ProposalProposed,
				}
				if err := d.Store.PutProposal(ctx, p); err != nil {
					return proposeOut{}, err
				}
				out.Proposals = append(out.Proposals, proposalRow{
					ProposalID: p.ID, Recipe: r.Title, Class: string(r.Class),
					MonthlySavings: brl(ev.MonthlySavingsCents), NetMonthly: brl(ev.NetMonthlyCents),
					PaybackMonths: paybackText(ev.PaybackMonths),
					Executable:    r.Class == catalog.ClassExecutable && r.ProductID != "",
					netCents:      ev.NetMonthlyCents,
					payback:       ev.PaybackMonths,
				})
			}
		}
		// Rank: biggest net monthly recovery first; ties → faster payback.
		sort.SliceStable(out.Proposals, func(i, j int) bool {
			a, b := out.Proposals[i], out.Proposals[j]
			if a.netCents != b.netCents {
				return a.netCents > b.netCents
			}
			return a.payback < b.payback
		})
		return out, nil
	})
}

type approveIn struct {
	ProposalID string `json:"proposal_id" jsonschema:"The proposal the user wants to approve"`
}
type approveOut struct {
	Status     string `json:"status"`
	Recipe     string `json:"recipe"`
	Executable bool   `json:"agent_can_execute"`
	Next       string `json:"next"`
}

func (d Deps) approveProposal() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:                "approve_proposal",
		Description:         "Mark a proposal as approved by the user. Purchases still require the consent screen (Trusted Surface) before any money action.",
		RequireConfirmation: true,
	}, func(ctx agent.Context, in approveIn) (approveOut, error) {
		p, err := d.Store.GetProposal(ctx, in.ProposalID)
		if err != nil {
			return approveOut{}, err
		}
		if p.UserID != d.UserID(ctx) {
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
	})
}

// --- graph -----------------------------------------------------------------

// style is appended to every instruction: replies land in a 380px chat panel.
const style = " Formatting: plain conversational text for a narrow chat panel. Short paragraphs; '-' bullets when listing; **bold** only the key number. No markdown tables, no headings, no LaTeX, no emoji. Stay under 120 words unless the user asks for detail."

// New builds the root orchestrator (must be ModeChat — runner requirement).
func New(llm model.LLM, d Deps) (agent.Agent, error) {
	addTask, err := d.addTask()
	if err != nil {
		return nil, err
	}
	pnl, err := d.lifePNL()
	if err != nil {
		return nil, err
	}
	propose, err := d.proposeAutomations()
	if err != nil {
		return nil, err
	}
	approve, err := d.approveProposal()
	if err != nil {
		return nil, err
	}

	analyst, err := llmagent.New(llmagent.Config{
		Name:        "routine_analyst",
		Description: "Interviews the user about their routine, estimates task durations from benchmarks, confirms numbers, and maintains the Life P&L.",
		Model:       llm,
		Instruction: "You capture the user's routine. First call get_life_pnl to see what is already tracked. For each task the user describes: if it is the same activity as an existing task (even worded differently, e.g. 'washing dishes' vs 'washing dishes after dinner'), update that task by passing its task_id to add_routine_task instead of creating a duplicate; ask when unsure. Estimate duration from general benchmarks (hand-washing dishes 40-60 min/day, supermarket run 60-90 min/week), ASK the user to confirm or adjust, then call add_routine_task with confirmed numbers. Never invent money figures — the tools return them. Use get_life_pnl to show the picture. Speak the user's language; keep money in BRL." + style,
		Tools:       []tool.Tool{addTask, pnl},
	})
	if err != nil {
		return nil, err
	}

	advisor, err := llmagent.New(llmagent.Config{
		Name:        "automation_advisor",
		Description: "Matches routine tasks to automation recipes, presents payback rankings, and records user approvals.",
		Model:       llm,
		Instruction: "You recommend automations. Call propose_automations to compute ranked proposals (the engine already filtered bad deals). Present the top 3 with payback in plain words ('pays for itself in about 2 months'; when there is no upfront cost say 'immediately', never '0 months'); mention that more exist. When the user wants one, call approve_proposal — purchases then go through the consent screen (the 'Let the agent buy it' button on the dashboard); you never handle payments yourself." + style,
		Tools:       []tool.Tool{propose, approve},
	})
	if err != nil {
		return nil, err
	}

	return llmagent.New(llmagent.Config{
		Name:        "automate_me",
		Description: "Automate.me orchestrator: finds where the user's life leaks time, prices it, and coordinates automation.",
		Model:       llm,
		Instruction: "You are Automate.me. Goal: find where the user leaks time, price it in BRL, and automate the worst leaks. Delegate routine capture to routine_analyst and recommendations to automation_advisor. Be concise and concrete; lead with numbers the tools return. Everything monetary comes from tools, never from you." + style,
		SubAgents:   []agent.Agent{analyst, advisor},
	})
}

func sanitize(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			out = append(out, r)
		case r >= 'A' && r <= 'Z':
			out = append(out, r+('a'-'A'))
		case r == ' ' && len(out) > 0 && out[len(out)-1] != '-':
			out = append(out, '-')
		}
		if len(out) >= 24 {
			break
		}
	}
	return string(out)
}

// upsertTarget picks the task to write: an explicit task_id wins, then a task
// whose normalised name matches, else a fresh task with a name-derived id.
func upsertTarget(existing []store.Task, taskID, name string) (store.Task, bool) {
	norm := sanitize(name)
	for _, t := range existing {
		if taskID != "" && t.ID == taskID {
			return t, true
		}
	}
	for _, t := range existing {
		if sanitize(t.Name) == norm {
			return t, true
		}
	}
	id := "t-" + norm
	for n := 2; ; n++ {
		clash := false
		for _, t := range existing {
			if t.ID == id {
				clash = true
				break
			}
		}
		if !clash {
			return store.Task{ID: id}, false
		}
		id = fmt.Sprintf("t-%s-%d", norm, n)
	}
}

// paybackText phrases payback for the LLM so free recipes read as
// "immediate" rather than "0 months".
func paybackText(months float64) string {
	if months <= 0 {
		return "immediate (no upfront cost)"
	}
	return fmt.Sprintf("%.1f months", months)
}

func defaultStr(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func round1(f float64) float64 {
	return float64(int(f*10+0.5)) / 10
}
