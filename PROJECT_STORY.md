## Inspiration

I live in São Paulo. Twenty-two million people, and a commute that is never the same trip twice.

Two things run my week here, and they are not the same thing. The first is **time** — where it
goes, and what it is worth. The second is **safety**: this is a city where a single afternoon of
rain closes streets that were fine an hour earlier, and where "leave at 6" and "leave at 6:20"
can be a flooded avenue apart.

So I delegate. My agenda, my work blocks, my personal errands — I hand them to an assistant and
let it hold the shape of my day. But every tool I tried held only half the problem.

Then I went looking at what already existed, and found the market split into three halves nobody
had stitched together:

- **Pricing time** exists only as static toy calculators. No memory, no real data, no action.
- **Measuring time** is mature — RescueTime, Toggl, Reclaim — but none of them attach a *monetary*
  dimension to personal life, and none of them suggest or execute an automation.
- **Recommending and executing** automation is mature only in enterprise process mining, on
  five- and six-figure contracts.

The signals were sharp. Zapier owns the largest automation graph on earth and still reports only
**time** saved — a flat two-minute guess per task, Enterprise plan only. Never money. Microsoft is
the one company that productized hours into dollars, and it is org-aggregate and recommends
nothing. And **Yohana, backed by Panasonic, shut down in September 2025** — a well-funded human
concierge that could not prove it saved anyone anything measurable.

That is the whole thesis. An assistant that cannot show you what it bought back is a subscription
you eventually cancel. I wanted the loop closed: **find the leak, price it in reais, fix it, and
prove it.**

## What it does

The loop is **Proof of Time**: capture → price → rank → execute → prove.

**Capture.** You tell the agent your routine in chat or by voice, or you photograph a handwritten
list, a pile of boletos, a school note. Gemini reads the pixels; the analyst confirms the numbers
with you before anything is saved.

**Price.** A deterministic Go engine computes the *Cost of Inaction* — what a routine costs you
every month by not being automated:

$$\text{CoI}_{\text{month}} = \sum_{i} m_i \times f_i \times r$$

where \\( m_i \\) is minutes per occurrence, \\( f_i \\) is occurrences per month, and \\( r \\) is
your hourly rate. For the demo profile — five declared routines at R$50/hour — that is
**R$3,366.08 a month**, 67.3 hours.

**Rank.** Routines are matched against a 25-recipe catalog and ranked by payback. The dishwasher
recipe recovers **R$1,375 a month** against a R$3,000 appliance:

$$\text{payback} = \frac{3000}{1375} \approx 2.18 \text{ months}$$

Negative-net automations are never proposed at all.

**Execute — and, under a signed envelope, without being asked.** You sign one **Spending
Authorization** up front: a standing delegation carrying a per-purchase cap, a merchant allowlist
and a hard expiry. Under it the agent completes purchases with nobody watching — it buys the R$350
grocery basket by itself and tells you afterwards. The R$3,000 dishwasher is above the cap, so it
stops and asks for a signature.

**Prove.** The Savings Ledger accumulates hours and reais bought back, with the signed AP2 receipt
attached to every purchase.

And the São Paulo half — the reason this is not just a productivity toy. Every morning the agent
builds a **Daily Briefing**: when to leave for each appointment, what the traffic is costing you
in reais, what to wear, and **whether your route crosses somewhere that floods.**

## How I built it

A multi-agent system on **ADK Go v2.2.0** with **Gemini 3.5 Flash**, deployed on **Google Cloud
Run**.

A root `automate_me` orchestrator routes to specialist sub-agents — `routine_analyst`,
`automation_advisor`, `day_planner` — over function tools that wrap deterministic Go modules. The
LLM converses and routes; the engine computes. The **Talk** tab is a live
[Gemini Live API](https://ai.google.dev/gemini-api/docs/live) session: `gemini-3.1-flash-live` is
the front of house, and every judgement it makes is handed to the 3.5 graph through
`consult_specialist`, because the only conversational Live model should not also be the brain.

**Payments run on AP2 v0.2 over A2A v1.0.1.** A separate merchant agent — its own private Cloud
Run service, invokable only by the app's service account — signs Checkout JWTs. The app signs
closed Checkout and Payment Mandates with an ECDSA P-256 key, and the merchant returns verifiable
receipts. Settlement is simulated and labelled as such everywhere. **The protocol is not
simulated.**

The **Daily Briefing** calls the Google Maps **Routes API** twice per appointment, then prices the
congestion it finds:

$$c = (t_{\text{traffic}} - t_{\text{static}}) \times r$$

Flood risk comes from two layers: live public weather alerts whose polygon your route crosses, and
**192 flooding occurrences logged by São Paulo's Civil Defense** (GeoSampa), matched within 150
metres of the route polyline. August is dry season here, so the historic layer is what keeps the
feature honest when there is nothing live to show.

**Google Cloud:** Cloud Run (two services) · Cloud Build · Artifact Registry · Secret Manager ·
Cloud Scheduler (the 06:00 briefing) · Firestore (agent sessions) · Vertex AI Memory Bank ·
Google Maps Platform · Cloud Logging · Cloud Trace.

## What I learned

**Autonomy and safety were not the trade-off I assumed.** The obvious way to let an agent buy
without asking is to loosen the rule that a human authorizes payments. The right way turned out to
be the opposite: keep that rule absolute, and move *when* the human applies it. A standing,
user-signed authorization with a cap, an allowlist and an expiry gives the agent real autonomy
while the signing surface stays exactly as non-agentic as it was before. The agent still cannot
sign, reach the key, mint an authorization, or widen one. What moved is that the human now decides
once, over a bounded envelope, instead of once per purchase.

**A cap is only as trustworthy as the number it is checked against.** The constraint is evaluated
against the total the *merchant signed*, after the Checkout JWT is verified and before the first
signature — never against a price my own code computed. A cap enforced against a local number
would be a cap an attacker could move. And because the check runs before anything is signed, a
refusal signs nothing and leaves no trace on the rail.

**Pricing traffic needs two Routes calls, not one.** How long the trip takes depends on when you
leave, and when you should leave depends on how long it takes. The first call exists only to pick
the departure time for the second.

**Keeping money out of the model is what makes the product credible.** Every figure comes from the
deterministic engine. A model that hallucinates your savings destroys the only thing this product
sells, so the model never gets to produce one.

**The AP2 threat model forces architecture, not policy.** The signing key lives in a package no LLM
code path can reach. Making the trusted surface non-agentic *by construction* was far easier to
reason about than trying to constrain a model with prompting.

## Challenges I ran into

**An org policy that forbade a public service.** My Google Cloud organization enforces Domain
Restricted Sharing, so binding `allUsers` on Cloud Run was rejected outright. Worse, `gcloud run
deploy --allow-unauthenticated` only *warns* when that binding fails — it exits zero and leaves you
with a private service you believe is public. The fix was a project-scoped policy override, plus a
deploy script that asserts the IAM binding explicitly and exits non-zero if the service is still
private. Failing loudly beat failing politely.

**A matcher that silently matched nothing.** The catalog pairs routines to recipes by substring.
The seeded routine "Supermarket run" matched no recipe at all, because the grocery recipe's
triggers were `mercado`, `grocery`, `supermercado` and `compras` — and none of those appear inside
the English word *supermarket*. The recipe existed, the rail could buy the product, and the
proposal was simply never generated. Nothing errored. It taught me to be suspicious of the quiet
paths, not the loud ones.

**A claim the code did not back.** One recipe was classed as *executable* with a report-generation
capability, but nothing ever executed it. I cut it rather than deferring it. A catalog that
promises what it cannot do is worse than a smaller catalog.

**Two agents, one of them private.** Cloud Run URLs are computable from the project number before
the first deploy, which is what let the app be configured with the merchant's address in the very
deploy that creates it — and let the merchant stay authenticated-only from the start.

## What's next

The **Plan Guardian**: every approved automation becomes a plan with expected savings and expected
signals, and the Guardian watches for drift — the dishwasher arrived, but you still logged forty
minutes of dishes — then promotes ledger entries from *projected* to *confirmed*. Proving the
value is the whole thesis, and right now the ledger only projects it.

After that: a Calendar Watcher that learns routines from your real calendar instead of asking, and
Open Finance so the cost side is measured rather than estimated.
