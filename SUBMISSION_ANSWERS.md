# Devpost form — answers to paste

Every field below is filled from verified repo evidence. Copy verbatim; edit only where
marked **[CHECK]**. Companion to `SUBMISSION_CHECKLIST.md`.

---

## Category

**Track 1 — The Taskmaster.** (Select this one only.)

---

## Which Google SDK did you use?

Agent Development Kit for Go (**ADK Go v2.2.0**, `google.golang.org/adk/v2`), together with the
**Google GenAI SDK for Go v1.68.0** (`google.golang.org/genai`). Agent-to-agent communication
uses the **A2A Go SDK v2.4.0**.

## Date you started the project

**2026-08-14** — first commit `45ea6a5`, 2026-08-14 18:53 -03.

## Model

**Gemini 3.5 Flash** (`gemini-3.5-flash`), pinned in both services.

## Hosted project URL

https://automate-me-288504867090.us-central1.run.app

## Testing instructions

No login and no credentials required — the app boots with seeded demo data
(`DEMO_MODE=seed`), so judges land straight on a working dashboard.

Suggested path:
1. Open the URL. The dashboard shows the Life P&L and the Savings Ledger.
2. Open **Proposals** — the dishwasher automation is ranked at R$ 1,375/month recovered,
   2.18-month payback.
3. Approve it. The consent screen appears; approving signs an AP2 mandate and the signed
   receipt is attached in the Ledger. **Payments are simulated and labelled as such — the AP2
   protocol itself runs for real (ECDSA P-256 signed mandates, verifiable receipts).**
4. Open the **Daily Briefing** and run it, for live Google Maps Routes + Weather data.

Backend runs on Google Cloud Run in `us-central1`. The merchant agent
(`merchant-agent`) is a second, private Cloud Run service, invokable only by the app's
service account.

---

## Text description

### What it does

**Automate.me finds where your life leaks time, prices the leak in money, and buys the fix.**

The loop is *Proof of Time*: capture → price → rank → execute → prove.

1. **Capture.** You tell the agent your routine in chat, or photograph a handwritten list, a
   pile of boletos, a school note. Gemini reads the pixels; the analyst confirms the numbers
   with you before saving anything.
2. **Price.** A deterministic Go Value Engine computes the *Cost of Inaction*:
   `minutes × times per month × your hourly rate`. **No LLM ever produces a money figure.**
3. **Rank.** Routines are matched against a 26-recipe catalog (8 executable, 9 advised, 9
   roadmap) and ranked by payback. Negative-net automations are never proposed.
4. **Execute.** With explicit approval the agent acts: buys a dishwasher over the AP2 payment
   protocol against a separate merchant agent, plans the day from live traffic, writes
   departure blocks to Calendar.
5. **Prove.** The Savings Ledger accumulates hours and R$ bought back, with signed AP2
   receipts attached to each purchase.

The gap it fills: pricing time exists only as static toy calculators; measuring time is mature
but never attaches money to personal life; executing automation is mature only in enterprise
process mining. Zapier reports time saved but never money. Yohana by Panasonic shut down in
Sep 2025 for failing to prove measurable value. Automate.me closes the whole loop and proves
the value in currency.

### How we built it

A multi-agent system on **ADK Go v2.2.0** with **Gemini 3.5 Flash**. A root `automate_me`
orchestrator routes to three sub-agents — `routine_analyst`, `automation_advisor`,
`day_planner` — over seven function tools (`add_routine_task`, `get_life_pnl`,
`propose_automations`, `approve_proposal`, `plan_my_day`, `get_daily_briefing`,
`write_departure_blocks`).

The frontend is React 19 + Vite + Tailwind 4. The agent panel streams over SSE, so you watch
the graph work: which sub-agent took the question, which tool is running, what it returned.
Tool results become cards you can act on without leaving the conversation.

**Technologies:** Go 1.26 · ADK Go v2.2.0 · Google GenAI SDK for Go v1.68.0 · Gemini 3.5 Flash ·
React 19 · Vite · Tailwind CSS 4.
**Google Cloud:** Cloud Run (two services) · Cloud Build · Artifact Registry · Secret Manager ·
Cloud Scheduler · Google Maps Platform (Routes + Weather) · Cloud Logging · Cloud Trace.
**Protocols:** AP2 v0.2 (agent payments, ECDSA P-256 signed mandates) · A2A v1.0.1
(agent-to-agent).

### Data sources

- **Google Maps Platform Routes API** (`routes.googleapis.com`) — live traffic-aware routing,
  used to price congestion.
- **Google Maps Platform Weather API** (`weather.googleapis.com`) — hourly forecast at the
  computed departure time, and live public weather alerts whose polygon the route crosses.
- **GeoSampa — São Paulo Civil Defense.** 192 logged flooding occurrences, matched within 150 m
  of the route polyline. August is dry season in São Paulo, so this historic layer is what
  keeps the flood-risk feature honest when there are no live alerts to show.
- User-declared routine data, entered by chat or extracted from a photograph by Gemini.

### What we learned

**Traffic pricing needs two Routes calls, not one.** Travel time depends on when you leave, and
when you should leave depends on travel time. The first call exists only to pick the departure
time for the second. Congestion is then priced deterministically as
`(duration − staticDuration) × hourly rate`.

**The AP2 threat model forces architecture, not just policy.** The user's P-256 signing key
lives in a `trusted` package that no LLM code path can reach, and a mandate is only signed
after the consent endpoint is called from the UI. Making the trusted surface non-agentic by
construction was easier to reason about than trying to constrain a model with prompting.

**Keeping money out of the model is what makes the product credible.** Every R$ figure comes
from a deterministic Go engine. An LLM that hallucinates a savings number destroys the one
thing the product sells, so the model never gets to produce one.

**Cloud Run's deterministic URLs solve the agent-to-agent bootstrap.** Both service URLs are
computable from the project number before the first deploy, so the app can be configured with
the merchant's address in the same deploy that creates it.

### Challenges

Deploying a public Cloud Run service under an organization policy that forbids `allUsers`
required a project-scoped policy override. Cloud Run's
`--allow-unauthenticated` only warns rather than failing when the binding is rejected, so the
deploy script asserts the IAM binding explicitly and exits non-zero if the service is still
private.

---

## Pre-existing / third-party code disclosure

All application code was written during the submission period, starting 2026-08-14. No
pre-existing codebase was reused.

Third-party dependencies are open-source libraries consumed unmodified via Go modules and npm:
ADK Go v2.2.0, the Google GenAI SDK for Go v1.68.0, the A2A Go SDK v2.4.0,
`golang-jwt/jwt/v5`, `gorilla/mux`, `gorilla/websocket`, OpenTelemetry, `google.golang.org/api`,
React 19, Vite, and Tailwind CSS 4.

The AP2 v0.2 and A2A v1.0.1 protocol implementations in `ap2core/` and `merchant/` are our own,
written against the public specifications. The project is MIT licensed.

---

## Startup Excellence prize — **[CHECK]**

Opt in **only** if you have both an incorporated organization name and a corporate email
address. Both are required to win, not merely to enter. Leave unticked otherwise.

- Organization name: **[CHECK — fill or leave opted out]**
- Corporate email: **[CHECK — fill or leave opted out]**

---

## Gallery uploads

- `docs/design/architecture.png` — architecture diagram (required upload).
- `docs/design/ap2-flow.png` — AP2 payment flow (optional second image).

---

## Bonus (optional, after everything else is submitted)

Social post with `#AllThingsAgenticHackathon` on X and LinkedIn. Suggested text:

> Built Automate.me for the Google Cloud All Things Agentic hackathon: an agent that finds
> where your life leaks time, prices the leak in R$, and then buys the fix — signing a real
> AP2 mandate to do it. ADK Go v2 + Gemini 3.5 Flash on Cloud Run. No LLM ever produces a
> money figure; a deterministic engine does.
> https://automate-me-288504867090.us-central1.run.app
> #AllThingsAgenticHackathon
