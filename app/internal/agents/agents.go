// Package agents builds the Automate.me agent graph (design §2): a ModeChat
// orchestrator with specialist sub-agents. Tools wrap the deterministic
// modules — the LLM converses and routes; engine/catalog/store compute.
package agents

import (
	"fmt"

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

func brl(cents int64) string {
	return fmt.Sprintf("R$%d.%02d", cents/100, cents%100)
}

func (d Deps) userRate(ctx agent.Context) (store.User, error) {
	return d.Store.GetUser(ctx, d.UserID(ctx))
}

// --- tools -----------------------------------------------------------------

type addTaskIn struct {
	Name          string  `json:"name" jsonschema:"Short task name, e.g. 'Washing dishes after dinner'"`
	Minutes       int     `json:"minutes_per_occurrence" jsonschema:"Estimated minutes per occurrence, confirmed with the user"`
	TimesPerMonth float64 `json:"times_per_month" jsonschema:"Occurrences per month (daily=30, weekly=4.33)"`
	Source        string  `json:"source" jsonschema:"Where it came from: interview, photo or calendar"`
}
type addTaskOut struct {
	TaskID              string `json:"task_id"`
	CostOfInactionMonth string `json:"cost_of_inaction_per_month"`
	Note                string `json:"note"`
}

func (d Deps) addTask() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "add_routine_task",
		Description: "Persist a routine task the user confirmed (name, minutes, frequency). Returns the monthly Cost of Inaction computed by the deterministic Value Engine.",
	}, func(ctx agent.Context, in addTaskIn) (addTaskOut, error) {
		if in.Name == "" || in.Minutes <= 0 || in.TimesPerMonth <= 0 {
			return addTaskOut{}, fmt.Errorf("name, positive minutes and times_per_month are required")
		}
		u, err := d.userRate(ctx)
		if err != nil {
			return addTaskOut{}, err
		}
		t := store.Task{
			ID:         fmt.Sprintf("t-%x", len(in.Name)+in.Minutes) + "-" + sanitize(in.Name),
			Name:       in.Name,
			EstMinutes: in.Minutes,
			FreqPerMon: in.TimesPerMonth,
			Source:     defaultStr(in.Source, "interview"),
			Confirmed:  true,
		}
		if err := d.Store.PutTask(ctx, u.ID, t); err != nil {
			return addTaskOut{}, err
		}
		cost := engine.CostOfInactionCents(engine.Task{Name: t.Name, EstMinutes: t.EstMinutes, FreqPerMonth: t.FreqPerMon}, u.HourlyRateCents)
		return addTaskOut{
			TaskID:              t.ID,
			CostOfInactionMonth: brl(cost),
			Note:                "Value computed deterministically; never estimate money yourself.",
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
					PaybackMonths: fmt.Sprintf("%.1f", ev.PaybackMonths),
					Executable:    r.Class == catalog.ClassExecutable,
				})
			}
		}
		return out, nil
	})
}

type approveIn struct {
	ProposalID string `json:"proposal_id" jsonschema:"The proposal the user wants to approve"`
}
type approveOut struct {
	Status string `json:"status"`
	Next   string `json:"next"`
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
		return approveOut{
			Status: "approved",
			Next:   "Tell the user to review and confirm on the consent screen; the agent cannot sign payment mandates.",
		}, nil
	})
}

// --- graph -----------------------------------------------------------------

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
		Instruction: "You capture the user's routine. For each task: estimate duration from general benchmarks (hand-washing dishes 40-60 min/day, supermarket run 60-90 min/week), ASK the user to confirm or adjust, then call add_routine_task with confirmed numbers. Never invent money figures — the tools return them. Use get_life_pnl to show the picture. Speak the user's language; keep money in BRL.",
		Tools:       []tool.Tool{addTask, pnl},
	})
	if err != nil {
		return nil, err
	}

	advisor, err := llmagent.New(llmagent.Config{
		Name:        "automation_advisor",
		Description: "Matches routine tasks to automation recipes, presents payback rankings, and records user approvals.",
		Model:       llm,
		Instruction: "You recommend automations. Call propose_automations to compute ranked proposals (the engine already filtered bad deals). Present top options with payback in plain words ('pays for itself in about 2 months'). When the user wants one, call approve_proposal — purchases then go through the consent screen; you never handle payments yourself.",
		Tools:       []tool.Tool{propose, approve},
	})
	if err != nil {
		return nil, err
	}

	return llmagent.New(llmagent.Config{
		Name:        "automate_me",
		Description: "Automate.me orchestrator: finds where the user's life leaks time, prices it, and coordinates automation.",
		Model:       llm,
		Instruction: "You are Automate.me. Goal: find where the user leaks time, price it in BRL, and automate the worst leaks. Delegate routine capture to routine_analyst and recommendations to automation_advisor. Be concise and concrete; lead with numbers the tools return. Everything monetary comes from tools, never from you.",
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

func defaultStr(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func round1(f float64) float64 {
	return float64(int(f*10+0.5)) / 10
}
