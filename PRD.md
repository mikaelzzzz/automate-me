# Automate.me — Product Requirements Document

**Version:** 1.0 · **Date:** 2026-08-14 · **Status:** Approved for implementation planning
**Hackathon:** Google Cloud "All Things Agentic" — Track 1: The Taskmaster
**Submission window:** Aug 3 – Aug 31, 2026, 5:00 PM PT ([official rules](https://allthingsagentichackathon.devpost.com/rules))

---

## 1. One-liner

> **Your agent finds where your life leaks time, prices the leak in money, and buys the fix — autonomously.**

Automate.me is an autonomous multi-agent system (ADK Go + Gemini 3.5) that turns a person's declared routine into a ranked, money-denominated list of automation opportunities — then **executes** the top ones on the user's behalf: buying appliances via the AP2 payment protocol, optimizing commutes with Google Maps, writing departure blocks and batching into Google Calendar, and proving the value recovered week after week.

## 2. Problem & Opportunity

Verified market research (Aug 2026) shows the market is split into three halves nobody has stitched together:

1. **Pricing time** exists only as static toy calculators (TimeWorth, Omni) — no memory, no real data, no action.
2. **Measuring time** is mature (RescueTime, Toggl, Reclaim) — but none attach a monetary dimension to personal life, and none suggest or execute automation.
3. **Recommending + executing automation** is mature only in enterprise process mining (UiPath, Celonis — five/six-figure contracts), or exists as unmeasured consumer concierges (Duckbill $49–350/mo; Yohana by Panasonic **shut down Sep 2025** for failing to prove measurable value).

Reference signals:
- Zapier, owner of the world's largest automation graph, only reports **time** saved (fixed 2-min/task guess, Enterprise plan only) — never money.
- Microsoft is the only company that productized hours→dollars (Copilot Dashboard, default $72/h) — but it is org-aggregate and recommends nothing.

**White space:** close the full consumer loop — capture routine → price it → rank by payback → execute → prove value. No step is novel; the product is the stitching. The defensible core is payback ranking fed by declared behavior, wired to an executor.

## 3. Target users

| Persona | Description | Mode |
|---|---|---|
| **Primary — Busy individual (Brazil-aware)** | Working adult drowning in domestic + personal-admin chores; may commute in a flood-prone city (São Paulo). Wants time back, responds to money framing. | Personal |
| **Secondary — Small team / business owner** | Team spends hours on manual work; owner wants a report of what to automate and what it costs to do nothing. | Teams |

## 4. Core loop ("Proof of Time")

1. **Declare** — user tells the agent their routine by voice (push-to-talk), text, or **photo** (handwritten to-do list, paper calendar, pile of boletos, school note, pantry).
2. **Estimate** — AI estimates duration & frequency per task from general-knowledge benchmarks (e.g., hand-washing dishes ≈ 40–60 min/day), asks the user to confirm/adjust when uncertain. Every number is user-editable.
3. **Price** — the deterministic Value Engine computes the **Cost of Inaction**: `time × frequency × personal hourly rate = R$/month lost by NOT automating` — per task and total. Hourly rate is declared or derived (buyback rate: annual income ÷ 2,000 ÷ 4).
4. **Rank** — automations from the catalog are ranked by **payback**: `payback_months = upfront_cost ÷ (monthly_time_value_recovered − monthly_running_cost)`. Automations with no upfront cost (subscriptions, services) rank by **net monthly savings** instead; negative-net automations are never proposed. Exact formulas live in the deterministic Value Engine with table-driven tests.
5. **Execute** — with explicit human approval, the agent acts: AP2 purchase, Calendar writes, Maps-optimized plans, Gmail drafts, Teams report.
6. **Prove** — the Savings Ledger accumulates: *"You bought back 22 h = R$1,100 this month."* (Yohana's lesson: without measurable proof, retention dies.)
7. **Follow through** — the Plan Guardian agent watches the plan in action: automatic signals + a weekly 2-question check-in verify that what was planned is actually happening, the ledger splits **projected vs confirmed** savings, and a Progress Report with updated charts shows implemented automations, adherence, and drift nudges.

## 5. Features

### 5.1 Must have (MVP)

Priority order is the listing order: **F1–F6 are the demoable core; F7–F10 degrade gracefully if the schedule slips.**

- **F1 — Conversational onboarding** (voice push-to-talk + text + photo ingestion). Audio goes directly into Gemini as multimodal input (no separate STT); spoken replies via Cloud Text-to-Speech. Photo of a handwritten list/paper calendar → structured tasks (Gemini vision) → routed to the Routine Analyst.
- **F2 — Value Engine** (deterministic Go, not an LLM): Cost of Inaction + payback ranking. Money math is never hallucinated.
- **F3 — Dashboard "Life P&L"** (Crextio Warm Minimal design system): KPI pills (h/month leaking, R$/month leaking, R$ recovered), routines table (est. time, R$/month, suggested automation, payback, status badge), and **attention-grabbing charts** (see §8).
- **F4 — Automation Catalog** — recipes stored as Firestore data over the **7 canonical agent capabilities** (`vision`, `calendar_write`, `maps_routes`, `weather_flood`, `gmail_draft`, `ap2_purchase`, `report_gen` — single enum shared with design §4). See §5.2.
- **F5 — Daily Briefing (spotlight feature)** — the agent scans the day's calendar events, fans out one route sub-agent per appointment, computes the calmest route and **optimal departure time** (Routes API with future `departureTime`), monetizes congestion (**`duration − staticDuration` × hourly rate = "today's traffic cost you R$23"** — a real measured number, straight into the Value Engine), checks **weather → clothing suggestion** (Weather API, GA, full Brazil coverage), and raises **flood alerts** for the route (São Paulo focus) from two layers: Weather API `publicAlerts` (`FLOOD`/`FLASH_FLOOD` events with severity + polygon) and a static GeoSampa historic flood-point layer intersected with the route ("your route crosses 3 points with flooding history"). Writes "Leave at 8:12 → Downtown meeting" blocks into Calendar and pushes everything to a Briefing screen — unprompted.
- **F6 — AP2 autonomous purchase** — real AP2 **v0.2** flow (Checkout Mandate + Payment Mandate, ECDSA-signed JWTs) against a merchant agent we build as a separate A2A service. Payment settlement is **simulated** and labeled as such. Non-agentic Trusted Surface collects consent (per AP2 threat model).
- **F7 — Calendar Watcher** — scheduled background scan (Cloud Scheduler) detects new recurring tasks in Google Calendar, refreshes the routine profile, and pushes new proposals. Calendar is a **secondary** source; declared routine is primary.
- **F8 — Teams mode** — team task list in (text/spreadsheet/photo of a whiteboard) + average team hourly cost → **Automation Opportunities Report**: ranked tasks, suggested automation per task, projected annual savings; shareable report page.
- **F9 — Savings Ledger** — cumulative proof of hours/R$ bought back, with AP2 receipts (verifiable JWTs) attached. Every entry is flagged **projected or confirmed** (see F10).
- **F10 — Plan Guardian** — every approved proposal becomes an **Action Plan** (expected savings + expected signals). A dedicated agent (Cloud Scheduler: daily light pass, weekly full pass) verifies the plan in action: collects automatic signals (Calendar blocks kept/moved/deleted, AP2 delivery completed, briefing blocks accepted, recipes executed), detects drift (planned-but-not-executed → chat nudge: *"the dishwasher arrived 3 days ago and you still logged 40 min of dishes yesterday — what's blocking?"*), runs a weekly check-in of at most 2 conversational questions, promotes ledger entries from projected to **confirmed**, and generates a **Progress Report** with updated charts (evolution curve with projected × confirmed bands, before/after), implemented-automation list, adherence %, and next-best actions — reusing the Teams report page; optionally drafts an email with the report link. No new capabilities required. If the schedule slips, the check-in degrades to signals-only.

### 5.2 Executable catalog recipes (MVP — 8 recipes, every one mapped to a timeline day)

All recipes run on the **7 canonical agent capabilities** (single enum, defined in design §4): `vision`, `calendar_write`, `maps_routes`, `weather_flood`, `gmail_draft`, `ap2_purchase`, `report_gen`.

| Recipe | Agent action | Built in days |
|---|---|---|
| Commute audit | Routes API compares car/transit/bike + departure windows; "your commute costs R$X/month"; writes optimal-departure block | 9–10 |
| Dishwasher purchase (demo hero) | Full AP2 v0.2 purchase + delivery event in Calendar | 6–8 |
| Calendar batching | Consolidates scattered recurring chores into weekly blocks | 3–5 |
| Delegation drafts | Ready-to-send message/ad for hiring cleaner/help | 3–5 |
| Boleto pile | Photo of boletos → extracts value/due date/47-digit line, schedules payment-eve reminders in Calendar (settlement is roadmap — bill-pay does not fit a catalog-checkout merchant) | 3–5 |
| School note to calendar | Photo of paper note / WhatsApp print → family calendar events + drafted reply (30-second demo) | 3–5 |
| Leave-on-time | Per-appointment traffic-predicted departure blocks; proposes video call with the R$ cost of the trip when travel isn't worth it | 9–10 |
| Teams report | Generates the shareable Automation Opportunities Report | 11–12 |

### 5.3 Advised recipes (payback cards; catalog data, no new code)

Robot vacuum / cleaning service · grocery delivery subscription · laundry service · auto-pay migration list · email filters · recurring-item subscriptions · gov.br digital alternatives to queues · Farmácia Popular (free meds check) · SNE 40% traffic-fine discount · "Is your car worth it?" total-cost vs ride-hailing comparison · virtual-assistant delegation with ready job description · forgotten money ritual (Banco Central SVR, Nota Fiscal Paulista credits) · Zapier-style workflow suggestions (Teams).

### 5.4 Roadmap (post-hackathon; listed in submission to show vision)

Open Finance monitor (ghost subscriptions, fees — via Pluggy/Belvo) · WhatsApp agent channel · automatic NFS-e issuing for freelancers · health-plan reimbursement end-to-end · self-answering inbox (Gmail read scope).

Cut from MVP scope after adversarial spec review (each needs bespoke orchestration no timeline day covers): gas canister prediction+purchase · weekly menu from pantry photo · anti no-show nightly confirmations · errand-loop waypoint optimization · boleto AP2 settlement.

## 6. Non-functional requirements

- **Language/stack:** Go 1.26.5, ADK Go v2.2.0, `gemini-3.5-flash` everywhere (meets the "Gemini 3.5 or newer" rule; `gemini-3-pro`/`gemini-3.5-pro` do not exist — verified).
- **Go quality gates (CI-enforced):** `gofmt`, `go vet`, `go fix` modernizers, `golangci-lint`, and **`go test -race` on every test run**. Concurrency uses `errgroup` + context propagation; no shared mutable state without synchronization (Daily Briefing fan-out is the hot spot).
- **Money math is deterministic:** Value Engine is plain Go with table-driven unit tests; LLMs converse, the engine calculates.
- **AP2 security posture:** non-agentic Trusted Surface (no LLM in the consent/signing path), ECDSA P-256 (spec forbids Ed25519 for mandate JWTs), exact `vct` string matching, mandate↔checkout hash binding, full audit trail in Firestore.
- **Demo resilience:** seeded demo mode + kill switch — if live OAuth fails, fall back to seed data. The 4-minute video must be uncut and show the backend running on Google Cloud.
- **Degradation:** external API failure degrades gracefully (Briefing without weather still shows route; catalog still ranks without Maps).

## 7. UX & design

- **Design system:** "Crextio Warm Minimal" (tokens in `docs/design/design-system.json`) — warm canvas `#F6F5F0`, sun accent `#F8D973`, pill-first geometry, quiet depth. Landing communicates both modes: "For you / For your team". UI language: **English**.
- **Charts must grab attention** (design-psychology principles):
  - **Before/After** — per-routine and total hours/R$ with vs. without automation (anchoring: annual cost shown before monthly).
  - **Evolution curve (hero chart)** — cumulative time & money bought back, week over week, from the Savings Ledger (goal gradient; loss-aversion framing for the "leaking" state).
- Persistent chat drawer (text + push-to-talk) on every screen.
- Screens: Landing · Onboarding · Life P&L Dashboard · Daily Briefing · Proposal + Consent Surface · Savings Ledger · Teams Report.

## 8. Hackathon fit & submission mapping

| Judging criterion | Weight | Our answer |
|---|---|---|
| Innovation & Operational Utility | 40% | Agent removes real friction **autonomously**: buys, schedules, plans routes, alerts floods — with money-denominated proof |
| Architectural Discipline & Tech Stack | 30% | Decoupled A2A agent network, deterministic money engine, non-agentic trusted surface, typed error handling, state in Firestore, secrets in Secret Manager |
| Demo & Production Readiness | 30% | Uncut 4-min demo, clean architecture diagram, reproducible README spin-up, Cloud Run console + logs on screen |

- **Required stack:** Gemini 3.5 ✓ (gemini-3.5-flash) · ADK ✓ (ADK Go v2) · Google Cloud ✓ (Cloud Run, Firestore, Secret Manager, Cloud Scheduler, Maps Platform, Cloud TTS, Cloud Logging).
- **Deliverables:** public repo + README spin-up instructions · architecture diagram · ~4-min demo video proving Google Cloud backend · hosted URL (encouraged).
- **Bonus points:** build-in-public blog post (explicitly stating it was built for this hackathon) + social post with **#AllThingsAgenticHackathon**.
- **Demo script (~230s of content in a ~4-min uncut take — 10s transition slack):** photo of handwritten list → structured tasks (30s) · push-to-talk "I wash dishes an hour a day" → dashboard updates live (30s) · Life P&L with before/after chart (30s) · Daily Briefing with flood alert + departure block (30s) · dishwasher proposal → consent → **AP2 mandates visualized** → receipt + delivery on Calendar (75s) · Teams report flash (10s) · Savings Ledger (projected × confirmed bands) + **Plan Guardian status ("2 of 3 automations active, 1 drifting → nudge sent")** + Cloud Run console/logs (25s). Ledger's evolution curve shows seeded history **labeled "simulated weeks"**, plus at least one real entry generated live during the take.
- **Action item (user):** claim the $150 GCP credits form before Aug 28, 12:00 PT.

## 9. Success metrics

**Definition — "autonomous action":** agent-initiated and agent-executed; purchases are additionally gated by explicit consent on the Trusted Surface, as the AP2 spec requires. Consent-gated purchase still counts as autonomous.

- Working end-to-end loop on Google Cloud by Aug 26 (buffer before Aug 31 deadline).
- Demo shows ≥3 autonomous actions (purchase, calendar write, briefing) without mid-demo failure.
- Value Engine unit-test coverage of all payback formulas; `-race` clean.
- Submission complete with both bonus-point actions published.

## 10. Timeline (17 days)

| Days | Deliverable |
|---|---|
| 1–2 | Scaffold Go+React, CI (all quality gates), Firestore, **Firestore-backed session.Service (go/no-go end of day 2 → fallback: single-instance Cloud Run, tradeoff documented)**, skeleton **deployed to Cloud Run day one** |
| 3–5 | Value Engine + Routine Analyst (interview + photo) + dashboard + **voice I/O (push-to-talk audio into Gemini, Cloud TTS reply)** + vision recipes (boleto, school note) + batching/delegation |
| 6–8 | AP2 v0.2 (mandates, merchant A2A service, trusted surface) |
| 9–10 | Daily Briefing (Maps/Weather/GeoSampa) + Calendar Watcher + commute recipes |
| 11–12 | Teams report, Savings Ledger, **Plan Guardian (action plans, signals, check-in, Progress Report)**, Crextio polish |
| 13–14 | Demo video, architecture diagram, README, blog + social post |
| 15–17 | Buffer + early submission |

## 11. Risks & mitigations

| Risk | Mitigation |
|---|---|
| AP2 v0.2 hand-implementation stalls | Spec is deterministic/testable; scope = happy path + 3 failure paths; official Go samples exist as fallback reference (v0.1 nomenclature — do not copy blindly) |
| August is dry season in SP — live flood alerts likely empty during demo | Primary narrative uses the GeoSampa **historic risk** layer (always has data, no disclaimer needed); optional replay mode with a real rainy day (2026-01-28 snapshot, 8 flooded points) as complement |
| adk-go pins legacy a2a-go v0.3.x | Talk A2A only through `adka2a`'s surface; no direct dependency on a2a-go v2.x in the main app |
| 8 executable recipes tempt scope creep | Advised recipes are pure data; every executable recipe is mapped to a timeline day (§5.2); demo commits to hero set (dishwasher, school note or boletos, briefing); cut order if slipping: Teams UI → commute audit → batching |
| Live-demo failure | Seed mode + kill switch; record video against seeded environment |

## 12. Out of scope (hackathon)

Real payment settlement / PSP sandbox · WhatsApp channel · Open Finance connections · NFS-e issuing · Gmail read scope (drafts only) · native mobile apps · multi-tenant auth beyond Google sign-in.

---

*Technical design: see `docs/superpowers/specs/2026-08-14-automate-me-design.md`. Design tokens: `docs/design/design-system.json`.*
