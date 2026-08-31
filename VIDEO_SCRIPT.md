# Demo video script — Automate.me

**Target:** 2:55 · **Limit:** 4:00 · **Narration:** ~390 words at ~150 wpm.
**Language:** English (project decision for product and demo).
**Rule being satisfied:** the agent working, *plus* proof the backend runs on Google Cloud.
**Judging weights:** 40% autonomy · 30% architecture · 30% demo.

Edit the hook freely — alternatives at the bottom. Everything else is timed to fit.

---

## 0:00–0:10 · Hook

**Screen.** App already open on the Ledger. The R$350 receipt is landing. No intro, no title card, no logo.

> "My agent just spent my money. I never approved this purchase — and that is exactly what I built."

The number lands on screen *before* the sentence ends. Never explain before showing.

---

## 0:10–0:45 · The autonomous purchase

This is where the autonomy score lives. Give it room.

**Screen.** Proposals → **Grocery delivery (R$350)** → Approve → the purchase completes on its own. No modal. Signed receipt drops into the Ledger.

> "One click, and it's done. No confirmation screen, because I signed a spending authorization once: a standing delegation with a cap of one thousand reais per purchase, a merchant allowlist, and a hard expiry. Under that envelope, the agent transacts on its own."

**On-screen text:** `AP2 v0.2 · ECDSA P-256 signed mandate · payments simulated, protocol real`

Keep the "simulated" label visible. Honesty scores; a judge who discovers it themselves discounts everything else.

---

## 0:45–1:30 · The refusal

The strongest 45 seconds in the video. Do not rush it.

**Screen.** Approve **the dishwasher (R$3,000)**. The agent stops. The consent screen opens. Show the literal reason.

> "Now the dishwasher. Three thousand reais — above my limit. So it refuses to proceed, and asks me."

*(beat — let the reason breathe on screen)*

> "Nothing was signed by that refusal. The check runs against the total the merchant signed, after the checkout is verified and before the first signature. A cap enforced against a price my own code computed would be a cap an attacker could move."

> "And here is the part that did not change: the agent cannot sign. It cannot reach the key, mint an authorization, or widen one. It asks the trusted surface, and the trusted surface says no."

**On-screen text:** `unresolved_constraint: 300000 BRL above authorized 100000 BRL`

**Screen.** You confirm by hand → the purchase completes → signed receipt in the Ledger.

> "My signature outranks the envelope. Now it buys."

---

## 1:30–1:50 · Where the number comes from

**Screen.** Life P&L. R$1,375/month, 2.18-month payback.

> "Every number here comes from a deterministic Go engine: minutes, times per month, my hourly rate. No language model ever produces a money figure. A model that hallucinates your savings destroys the only thing this product sells."

---

## 1:50–2:20 · Real data

**Screen.** Daily Briefing running.

> "The daily briefing calls the Google Maps Routes API twice — because how long the trip takes depends on when you leave, and when you should leave depends on how long it takes. Congestion is then priced in reais. Flood risk comes from a hundred and ninety-two flooding occurrences logged by São Paulo's Civil Defense, matched within a hundred and fifty metres of my route."

---

## 2:20–2:45 · Google Cloud proof

**Required by the rules. Not decoration — cutting this can disqualify the entry.**

**Screen — three fast cuts:**
1. Address bar: `automate-me-288504867090.us-central1.run.app`
2. Cloud Run console showing both services
3. Terminal: `gcloud run services logs read automate-me --project=automate-me-hack --region=us-central1`

> "All of this runs on Google Cloud Run. Two services: the agent graph, and a separate merchant agent that is private — only my app's service account can invoke it. ADK Go v2, Gemini 3.5 Flash, agent-to-agent over A2A."

---

## 2:45–2:55 · Close

**Screen.** `docs/design/architecture.png`

> "Capture your routine. Price it. Rank it. Execute it. Prove it. That's Automate.me."

---

## Hook alternatives

What makes a hook work here: **promise a paradox the video then resolves.** Do not announce the product.

| | Hook | Why it works |
|---|---|---|
| **A** *(current)* | "My agent just spent my money. I never approved this purchase — and that is exactly what I built." | Immediate tension. "Exactly what I built" forces the question: why? |
| **B** | "This agent buys things without asking me. It also cannot sign a single payment. Both are true." | Pure paradox — hard not to want the explanation. Best of the four if you land the pause. |
| **C** | "Watch it buy my groceries. Now watch it refuse to buy a dishwasher — with the same permission." | Promises the whole structure in six seconds. Safer, less elegant. |
| **D** | "Yohana shut down because it could not prove it saved you anything. Here is my agent buying back four hours this week, with receipts." | Anchors in the market. Strong for a business judge, weak for a technical one. |

Whichever you pick, the only hard requirement is that **the result is on screen within the first ten seconds.**

---

## Recording rules

- Already logged in. Zero setup, zero loading.
- **Never type live.** Paste, or cut to the finished result.
- Short independent clips, so one bad take does not cost the whole video.
- Cut every pause, "um", and dead moment. Jump cuts.
- Speed slow stretches 1.1–1.2×.
- Team story and inspiration go in the written description, never in the video.

### Reset the demo before recording

The store is in memory, so once you approve something it stays approved on that instance.

```bash
GCP_PROJECT=automate-me-hack ONLY=app ./infra/deploy.sh
curl -s https://automate-me-288504867090.us-central1.run.app/app/api/proposals \
  | grep -o '"status":"proposed"' | wc -l     # expect both purchases back
```

Rehearse as much as you like, reset once more, then record.

**Check before the first take:** the cold start must land on the P&L, not the onboarding screen.

```bash
curl -s https://automate-me-288504867090.us-central1.run.app/app/api/profile
```

`"onboarded": true` means you are clear. If it still says `false`, you land on setup on
every reset — which the hackathon's own guidance says never to film.

---

## Cut first if you run long

In this order: the close (2:45), then real data (1:50), then where the number comes from (1:30).
**Never cut the refusal or the Google Cloud proof** — the first is the autonomy score, the
second is a rule.

## Not in this cut

- **Voice.** The Talk tab is a real Gemini Live session delegating to the 3.5 graph. It is a
  genuine differentiator, but it costs 30+ seconds to show convincingly and it competes with
  the refusal. Mention it in the written description instead.
- **Onboarding.** The app asks what an hour of your life is worth, once. Good feature, wrong
  place — filming setup is explicitly against the guidance. Written description.
