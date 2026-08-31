// Package agents builds the Automate.me agent graph (design §2): a ModeChat
// orchestrator with specialist sub-agents. Tools wrap the deterministic
// modules — the LLM converses and routes; engine/catalog/store compute.
package agents

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
	"google.golang.org/adk/v2/tool/loadmemorytool"
	"google.golang.org/adk/v2/tool/preloadmemorytool"
	"google.golang.org/genai"

	"automate-me/app/internal/briefing"
	"automate-me/app/internal/memorybank"
	"automate-me/app/internal/profile"
	"automate-me/app/internal/store"
	"automate-me/app/internal/trusted"
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
	// Memory is Vertex AI Memory Bank when configured: what the agent knows
	// about this person from earlier conversations. Nil disables recall and
	// the memory tools.
	Memory *memorybank.Service
	// Sessions is the session store the graph runs on. The after-turn
	// callback needs it: a callback context exposes the session's identity
	// but not the session itself.
	Sessions session.Service
	// Consult runs the agent graph (Gemini 3.5 Flash) for the voice session.
	// Set after the graph is built, since it closes over the runner.
	Consult func(ctx context.Context, userID, question string) (Consultation, error)
	// Trusted is the non-agentic Trusted Surface. The graph holds it only to
	// *attempt* a purchase under a standing authorization the user signed —
	// it cannot sign, mint or widen one. Nil disables autonomous buying, and
	// every purchase then goes through the consent screen.
	Trusted *trusted.Surface
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

// setProfileIn is the model-facing shape: money in reais, because that is how
// a person says it out loud. Conversion to centavos happens here, once, before
// anything deterministic sees it.
type setProfileIn struct {
	Name               string  `json:"name,omitempty"`
	HourlyRateReais    float64 `json:"hourly_rate_reais,omitempty"`
	MonthlyIncomeReais float64 `json:"monthly_income_reais,omitempty"`
	HoursPerWeek       float64 `json:"hours_per_week,omitempty"`
	HomeAddress        string  `json:"home_address,omitempty"`
	WorkAddress        string  `json:"work_address,omitempty"`
	WorkSetup          string  `json:"work_setup,omitempty"`
}

func (in setProfileIn) toInput() profile.Input {
	return profile.Input{
		Name:               in.Name,
		HourlyRateCents:    reaisToCents(in.HourlyRateReais),
		MonthlyIncomeCents: reaisToCents(in.MonthlyIncomeReais),
		HoursPerWeek:       in.HoursPerWeek,
		HomeAddress:        in.HomeAddress,
		WorkAddress:        in.WorkAddress,
		WorkSetup:          in.WorkSetup,
	}
}

func reaisToCents(v float64) int64 {
	if v <= 0 {
		return 0
	}
	return int64(v*100 + 0.5)
}

func (d Deps) getProfile() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "get_profile",
		Description: "What the app knows about the user: the price of their hour, how that price was set, where they live and work, and what their tracked routine costs per month at that rate. Call it before asking them anything you may already know.",
	}, func(ctx agent.Context, _ struct{}) (profileOut, error) {
		return d.GetProfile(ctx, d.UserID(ctx))
	})
}

func (d Deps) setProfile() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "set_profile",
		Description: "Record what the user told you about themselves and re-price everything already tracked. Price the hour either directly (hourly_rate_reais) or from what they earn (monthly_income_reais with hours_per_week) — never both. Send only the fields they actually gave you.",
	}, func(ctx agent.Context, in setProfileIn) (profileOut, error) {
		return d.SetProfile(ctx, d.UserID(ctx), in.toInput())
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
	// Purchased is true when a standing Spending Authorization covered this
	// one and the agent completed the AP2 dance without a consent screen.
	Purchased bool `json:"purchased_autonomously,omitempty"`
	// PurchaseTotal is what was actually charged, formatted for speech.
	PurchaseTotal string `json:"purchase_total,omitempty"`
	// MandateRef is the audit-trail record of an autonomous purchase.
	MandateRef string `json:"mandate_ref,omitempty"`
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
	getProf, err := d.getProfile()
	if err != nil {
		return nil, err
	}
	setProf, err := d.setProfile()
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
		Instruction: "You capture the user's routine and who they are. The price of their hour is what every number in the product is computed from: if get_profile shows it was never set, ask for it before pricing anything — either what an hour of their time is worth, or what they earn in a month and how many hours a week they work, and call set_profile. Same for where they start their day, when the conversation is about commuting. Photos: when the user sends an image (handwritten to-do list, paper calendar, pile of boletos, school note, whiteboard), read every item on it, turn each into a routine task with your best estimate of minutes and times per month, list them in one short message for confirmation, and after the user confirms save each with add_routine_task (source: photo). First call get_life_pnl to see what is already tracked. For each task the user describes: if it is the same activity as an existing task (even worded differently, e.g. 'washing dishes' vs 'washing dishes after dinner'), update that task by passing its task_id to add_routine_task instead of creating a duplicate; ask when unsure. Estimate duration from general benchmarks (hand-washing dishes 40-60 min/day, supermarket run 60-90 min/week), ASK the user to confirm or adjust, then call add_routine_task with confirmed numbers. Never invent money figures — the tools return them. Use get_life_pnl to show the picture. Speak the user's language; keep money in BRL." + style,
		Tools:       []tool.Tool{addTask, pnl, getProf, setProf},
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

	// Memory: preload_memory puts what we already know about this person in
	// front of the model on every request; load_memory lets it go looking.
	// The after-run callback hands the finished turn to Memory Bank, which
	// decides which facts are durable.
	var memTools []tool.Tool
	var afterRun []agent.AfterAgentCallback
	// The orchestrator carries the profile tools itself. A person states their
	// rate or their address in the middle of any conversation, and routing that
	// to a sub-agent first is how the fact gets lost: it once answered "I have
	// updated your hourly rate" without ever calling the tool.
	rootTools := []tool.Tool{getProf, setProf}
	instruction := "You are Automate.me. Goal: find where the user leaks time, price it in BRL, and automate the worst leaks. Delegate routine capture (text or photos of lists, calendars, boletos, notes) to routine_analyst, recommendations to automation_advisor, and anything about today's/tomorrow's schedule, commute, departure times, traffic, weather or floods to day_planner. Be concise and concrete; lead with numbers the tools return. Everything monetary comes from tools, never from you. When the user tells you what their hour is worth, what they earn, how many hours they work, where they live or where they work — call set_profile yourself, in that same turn, and quote back what the tool returned. Never say you saved, updated or remembered something unless a tool call did it."
	if d.Memory != nil {
		memTools = []tool.Tool{preloadmemorytool.New(), loadmemorytool.New()}
		afterRun = []agent.AfterAgentCallback{d.rememberTurn}
		instruction += " You remember this person between conversations: use what you already know about their routine, constraints and preferences instead of asking again, and say what you remember out loud when it changes your answer. Never invent a memory."
	}

	return llmagent.New(llmagent.Config{
		Name:                "automate_me",
		Description:         "Automate.me orchestrator: finds where the user's life leaks time, prices it, and coordinates automation.",
		Model:               llm,
		Instruction:         instruction + style,
		SubAgents:           []agent.Agent{analyst, advisor, planner},
		Tools:               append(rootTools, memTools...),
		AfterAgentCallbacks: afterRun,
	})
}

// rememberTurn hands the conversation so far to Memory Bank, which decides
// which facts are durable. It runs after every turn — the service consolidates
// rather than duplicating — detached from the request, so remembering never
// delays the answer and a failure never costs the user their reply.
//
// A callback context refuses Session() and Memory(); it does give the identity
// triple, so the session is fetched from the store instead.
func (d Deps) rememberTurn(ctx agent.Context) (*genai.Content, error) {
	if d.Memory == nil || d.Sessions == nil {
		return nil, nil
	}
	app, user, id := ctx.AppName(), ctx.UserID(), ctx.SessionID()
	bg := context.WithoutCancel(ctx)
	go func() {
		bg, cancel := context.WithTimeout(bg, 30*time.Second)
		defer cancel()
		res, err := d.Sessions.Get(bg, &session.GetRequest{AppName: app, UserID: user, SessionID: id})
		if err != nil {
			slog.Warn("memory: could not read the session to remember it", "session", id, "err", err)
			return
		}
		if err := d.Memory.AddSessionToMemory(bg, res.Session); err != nil {
			slog.Warn("memory: could not store this turn", "session", id, "err", err)
			return
		}
		slog.Info("memory: turn handed to Memory Bank", "session", id, "user", user)
	}()
	return nil, nil
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
