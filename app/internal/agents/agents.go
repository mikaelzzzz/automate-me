// Package agents builds the Automate.me agent graph (design §2): a ModeChat
// orchestrator with specialist sub-agents. Tools wrap the deterministic
// modules — the LLM converses and routes; engine/catalog/store compute.
package agents

import (
	"context"
	"fmt"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"

	"automate-me/app/internal/briefing"
	"automate-me/app/internal/store"
)

// Deps carries the deterministic modules the tools operate on.
type Deps struct {
	Store store.Store
	// UserID resolves the acting user. Demo scope: fixed demo user; multi-user
	// auth swaps this for a session-state lookup.
	UserID func(agent.Context) string
	// Briefing is nil without MAPS_API_KEY; the day planner then says so.
	Briefing *briefing.Builder
	Blocks   briefing.BlockWriter
	// Events is where the day's appointments come from: the connected Google
	// Calendar, or the seeded São Paulo day when none is.
	Events briefing.EventSource
	// Consult runs the agent graph (Gemini 3.5 Flash) for the voice session.
	// Set after the graph is built, since it closes over the runner.
	Consult func(ctx context.Context, userID, question string) (Consultation, error)
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
		return d.AddTask(ctx, d.UserID(ctx), in)
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
		return d.LifePNL(ctx, d.UserID(ctx))
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
		Description: "Match the user's tasks against the automation catalog, rank by net monthly savings (deterministic engine) and persist proposals. Negative-net automations are never proposed.",
	}, func(ctx agent.Context, in proposeIn) (proposeOut, error) {
		return d.Propose(ctx, d.UserID(ctx), in)
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
		return d.Approve(ctx, d.UserID(ctx), in)
	})
}

// --- day planner tools -------------------------------------------------------

type briefingRow struct {
	CardID      string `json:"card_id"`
	Event       string `json:"event"`
	EventAt     string `json:"event_at"`
	LeaveAt     string `json:"leave_at"`
	Route       string `json:"route"`
	TrafficMin  int    `json:"traffic_minutes"`
	TrafficCost string `json:"traffic_cost"`
	Weather     string `json:"weather"`
	Clothing    string `json:"clothing"`
	FloodRisk   string `json:"flood_risk"`
	FloodDetail string `json:"flood_detail"`
	Alert       string `json:"alert,omitempty"`
	Alternative string `json:"alternative,omitempty"`
	Calendar    string `json:"calendar_block"`
}
type briefingOut struct {
	Day   string        `json:"day"`
	Cards []briefingRow `json:"cards"`
	Note  string        `json:"note"`
}

func (d Deps) rows(cards []store.BriefingCard) []briefingRow {
	out := make([]briefingRow, 0, len(cards))
	for _, c := range cards {
		loc := c.EventStart.Location()
		row := briefingRow{
			CardID: c.ID, Event: c.EventSummary,
			EventAt: c.EventStart.In(loc).Format("15:04"), LeaveAt: c.DepartureTime.In(loc).Format("15:04"),
			Route: c.RouteSummary, TrafficMin: c.TrafficMinutes, TrafficCost: brl(c.TrafficCents),
			Weather: c.Weather, Clothing: c.Clothing, FloodRisk: c.FloodRisk, FloodDetail: c.FloodDetail,
			Alert: c.AlertHeadline, Alternative: c.AlternativeNote, Calendar: "not written",
		}
		if c.CalendarBlockID != "" {
			row.Calendar = "written (" + c.CalendarBlockMode + ")"
		}
		out = append(out, row)
	}
	return out
}

func (d Deps) planMyDay() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "plan_my_day",
		Description: "Build today's Daily Briefing: one route worker per appointment (Routes API with future departure time), traffic priced at the user's hourly rate, hourly weather at departure, flood risk from live alerts and GeoSampa history. Returns the cards; call get_daily_briefing if it was already built.",
	}, func(ctx agent.Context, _ struct{}) (briefingOut, error) {
		return d.PlanMyDay(ctx, d.UserID(ctx))
	})
}

func (d Deps) getBriefing() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "get_daily_briefing",
		Description: "Read the Daily Briefing already built for the day being planned (today before 08:00, else tomorrow).",
	}, func(ctx agent.Context, _ struct{}) (briefingOut, error) {
		return d.GetBriefing(ctx, d.UserID(ctx))
	})
}

type blocksIn struct {
	CardIDs []string `json:"card_ids" jsonschema:"card_id values from the briefing to write 'Leave at' blocks for; empty means all"`
}
type blocksOut struct {
	Written []string `json:"written"`
	Mode    string   `json:"mode"`
	Note    string   `json:"note"`
}

func (d Deps) writeBlocks() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:                "write_departure_blocks",
		Description:         "Write 'Leave at HH:MM → event' blocks to the user's calendar for the briefed appointments. Requires the user's confirmation.",
		RequireConfirmation: true,
	}, func(ctx agent.Context, in blocksIn) (blocksOut, error) {
		return d.WriteBlocks(ctx, d.UserID(ctx), in)
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
	plan, err := d.planMyDay()
	if err != nil {
		return nil, err
	}
	getBrief, err := d.getBriefing()
	if err != nil {
		return nil, err
	}
	blocks, err := d.writeBlocks()
	if err != nil {
		return nil, err
	}

	analyst, err := llmagent.New(llmagent.Config{
		Name:        "routine_analyst",
		Description: "Interviews the user about their routine, estimates task durations from benchmarks, confirms numbers, and maintains the Life P&L.",
		Model:       llm,
		Instruction: "You capture the user's routine. Photos: when the user sends an image (handwritten to-do list, paper calendar, pile of boletos, school note, whiteboard), read every item on it, turn each into a routine task with your best estimate of minutes and times per month, list them in one short message for confirmation, and after the user confirms save each with add_routine_task (source: photo). First call get_life_pnl to see what is already tracked. For each task the user describes: if it is the same activity as an existing task (even worded differently, e.g. 'washing dishes' vs 'washing dishes after dinner'), update that task by passing its task_id to add_routine_task instead of creating a duplicate; ask when unsure. Estimate duration from general benchmarks (hand-washing dishes 40-60 min/day, supermarket run 60-90 min/week), ASK the user to confirm or adjust, then call add_routine_task with confirmed numbers. Never invent money figures — the tools return them. Use get_life_pnl to show the picture. Speak the user's language; keep money in BRL." + style,
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

	planner, err := llmagent.New(llmagent.Config{
		Name:        "day_planner",
		Description: "Builds and narrates the Daily Briefing: when to leave for each appointment, what traffic costs, weather and what to wear, flood risk on the route, and writes departure blocks to the calendar.",
		Model:       llm,
		Instruction: "You plan the user's day. Call get_daily_briefing first; if nothing is built, call plan_my_day. Brief each appointment in one line: leave-at time, route minutes, traffic cost in BRL, weather and what to wear, flood risk (say plainly when a route crosses points with flooding history). Lead with the biggest risk of the day. Every number comes from the tool. Then offer to write the departure blocks to the calendar; on yes, call write_departure_blocks." + style,
		Tools:       []tool.Tool{getBrief, plan, blocks},
	})
	if err != nil {
		return nil, err
	}

	return llmagent.New(llmagent.Config{
		Name:        "automate_me",
		Description: "Automate.me orchestrator: finds where the user's life leaks time, prices it, and coordinates automation.",
		Model:       llm,
		Instruction: "You are Automate.me. Goal: find where the user leaks time, price it in BRL, and automate the worst leaks. Delegate routine capture (text or photos of lists, calendars, boletos, notes) to routine_analyst, recommendations to automation_advisor, and anything about today's/tomorrow's schedule, commute, departure times, traffic, weather or floods to day_planner. Be concise and concrete; lead with numbers the tools return. Everything monetary comes from tools, never from you." + style,
		SubAgents:   []agent.Agent{analyst, advisor, planner},
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
