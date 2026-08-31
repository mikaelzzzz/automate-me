# Devpost Submission Checklist — All Things Agentic

**Audited:** 2026-08-31 · **Deadline:** 2026-08-31 17:00 PT
**Verified against:** commit `759c1e3`, live Cloud Run deployment.

Legend: ✅ done and verified · ⚠️ partial / needs an action · ❌ blocker.

---

## 1. Built during the submission period with the required stack

| Requirement | Status | Evidence |
|---|---|---|
| Built in window (Aug 3 – Aug 31, 2026) | ✅ | First commit `45ea6a5` — **2026-08-14 18:53 -03**. All 24 commits fall inside the window. |
| **Gemini 3.5 or newer** | ✅ | `gemini-3.5-flash` pinned in `app/cmd/server/main.go:116` and `merchant/cmd/server/main.go:64`, default in `app/.env.example:24`. |
| **A Google agent framework** | ✅ | **ADK Go v2.2.0** (`google.golang.org/adk/v2` in `app/go.mod` and `merchant/go.mod`) **and** the **GenAI SDK** (`google.golang.org/genai v1.68.0`). Two of the four accepted frameworks. |
| **At least one Google Cloud service** | ✅ | Cloud Run, Cloud Build, Artifact Registry, Secret Manager, Cloud Scheduler, Maps Platform (Routes + Weather), Cloud Logging, Cloud Trace. Provisioned by `infra/gcp-setup.sh`, deployed by `infra/deploy.sh`. |

Agent graph actually in the code (`app/internal/agents/agents.go`): root `automate_me`
orchestrator over `routine_analyst`, `automation_advisor`, `day_planner`, with 7 function
tools (`add_routine_task`, `get_life_pnl`, `propose_automations`, `approve_proposal`,
`plan_my_day`, `get_daily_briefing`, `write_departure_blocks`).

---

## 2. One category selected

✅ **Track 1 — The Taskmaster.** Declared in `README.md:5`, `PRD.md`, `DESIGN_SPEC.md`.
Action: select the same one on the Devpost form. Do not tick a second.

---

## 3. Teammates added and invitations accepted

⚠️ **Cannot be verified from the repo.** If solo, nothing to do. If not, invitations must be
*accepted* before the deadline — a pending invite does not count.

---

## 4. Demo video — public, under 4 min, agent working + proof the backend runs on Google Cloud

❌ **Not recorded.** The only hard deliverable still missing. See the script in §12.

Two things the video *must* contain to satisfy the rule:
1. The agent working (chat → proposal → consent → AP2 receipt, or the Daily Briefing).
2. **Proof the backend runs on Google Cloud** — show the `*.run.app` URL in the address bar,
   plus one of: the Cloud Run console page for `automate-me`, or a terminal running
   `gcloud run services logs read automate-me --project=automate-me-hack --region=us-central1`.

Upload public to YouTube (not "unlisted" if the form asks for public).

---

## 5. Code repo linked, access granted if private

❌ **BLOCKER — there is no git remote.** `git remote -v` is empty; the repo exists only on
this machine. Nothing is pushed anywhere, so there is no URL to submit.

Fix (choose public — it removes the access-grant requirement entirely):

```bash
gh repo create automate-me --public --source=. --remote=origin --push
```

If it must be private, then after creating it, grant read access to **both**:
- `testing@devpost.com`
- `cloudhackathons@google.com`

Before pushing, note that `app/web/dist/` and `node_modules/` are gitignored — good — and
`.env` is gitignored while `.env.example` is kept. No secrets were found in tracked files.

---

## 6. Architecture diagram uploaded + spin-up instructions in the README

| Item | Status | Evidence |
|---|---|---|
| Spin-up instructions | ✅ | `README.md` §Spin-up (line 140): prerequisites, local run, end-to-end verification, `infra/deploy.sh` for Google Cloud. |
| Architecture diagram in the README | ✅ | Two Mermaid diagrams (`README.md:42` agent graph, `README.md:113`). |
| Diagram **uploadable as an image** | ✅ | `docs/design/architecture.png` and `docs/design/ap2-flow.png` already exist — upload `architecture.png` to the Devpost gallery. Mermaid inside a README does **not** count as an uploaded diagram. |

---

## 7. Hosted project URL

✅ **Live and responding.** `https://automate-me-288504867090.us-central1.run.app`

Verified 2026-08-31:

| Endpoint | Result |
|---|---|
| `/` | HTTP 200 (0.42 s) |
| `/app/api/pnl` | HTTP 200 |
| `/app/api/ledger` | HTTP 200 |
| `/app/api/proposals` | HTTP 200, returns the dishwasher proposal (R$ 1,375/mo, 2.18-month payback) |

No login wall, so no test credentials are needed on the form. Say exactly that in the
testing instructions — judges otherwise assume they are missing something.

⚠️ Cloud Run scales to zero. Cold start was 0.42 s here, which is fine, but hit the URL once
right before submitting so the first judge does not eat the cold start.

---

## 8. Which Google SDK, and the project start date

✅ Both answerable from evidence:

- **SDK / framework:** Agent Development Kit for Go (ADK Go) **v2.2.0**, together with the
  Google GenAI SDK for Go **v1.68.0**. Also uses the A2A Go SDK v2.4.0 for agent-to-agent.
- **Start date:** **2026-08-14** (first commit `45ea6a5`, 18:53 -03).

---

## 9. Text description — features, technologies, data sources, learnings

⚠️ **Not written as a submission text.** The raw material is all in `README.md` and `PRD.md`,
but the Devpost description field is separate and must be pasted in. The four required
angles, with what to say:

**Features.** Capture a routine by chat or photo → deterministic Value Engine prices the Cost
of Inaction (`minutes × times/month × hourly rate`, never LLM-generated) → 26-recipe catalog
ranked by payback → execution with explicit consent (AP2 purchase, Calendar departure blocks,
Maps-optimized Daily Briefing) → Savings Ledger with signed receipts.

**Technologies.** Go 1.26, ADK Go v2.2.0, Gemini 3.5 Flash, React 19 + Vite + Tailwind 4,
Cloud Run, Cloud Build, Artifact Registry, Secret Manager, Cloud Scheduler, Cloud Logging,
Cloud Trace. Protocols: AP2 v0.2 (ECDSA P-256 signed mandates) and A2A v1.0.1.

**Data sources.** Google Maps Platform **Routes API** (`routes.googleapis.com`) and
**Weather API** (`weather.googleapis.com` — hourly forecast and public alerts), plus
**GeoSampa** — 192 flooding occurrences logged by São Paulo's Civil Defense, matched within
150 m of the route polyline. Name GeoSampa explicitly; it is the one non-Google source and
judges reward a real public dataset.

**What you learned.** Strongest honest angles from the build: pricing traffic requires calling
Routes twice (departure time changes the answer, so the first call only exists to pick the
time for the second); the AP2 threat model forces the signing key into a package no LLM code
path can reach; and the reason a money figure never comes from the model.

---

## 10. Pre-existing / third-party code disclosure

⚠️ **Not written, and there is no LICENSE file.** Nothing in the repo is copied code, but the
form asks for a positive statement. Suggested answer:

> All application code was written during the submission period, starting 2026-08-14. No
> pre-existing codebase was reused. Third-party dependencies are all open-source libraries
> consumed unmodified via Go modules and npm: ADK Go v2.2.0, Google GenAI SDK for Go v1.68.0,
> the A2A Go SDK v2.4.0, `golang-jwt/jwt/v5`, `gorilla/mux`, OpenTelemetry, React 19, Vite,
> and Tailwind CSS 4. The AP2 v0.2 and A2A v1.0.1 protocol implementations in `ap2core/` and
> `merchant/` are our own, written against the public specifications.

Optional, 30 seconds: add a `LICENSE` (Apache-2.0 or MIT) at the repo root before pushing.

---

## 11. Optional items

| Item | Status | Note |
|---|---|---|
| Startup Excellence prize | ⚠️ Your call | Requires opting in **plus** an incorporated organization name and a corporate email. Do not opt in without both — it is required to win, not to enter. |
| Public write-up / social post | ❌ Not done | Bonus points. One LinkedIn or X post with `#AllThingsAgenticHackathon`, linking the live URL. ~10 minutes. |
| Extra Google models (Gemma, Veo, Lyria) | ❌ Not integrated | Bonus points only. **Do not start this now** — too little time, and a half-wired model is worse than none. |

---

## 12. Video script — 3 min, built from what already works

Recorded per the "pro tips": no intro, already logged in, all load time cut.

| Time | Screen | Say |
|---|---|---|
| 0:00–0:15 | App already open, agent panel streaming a proposal card | "This is my agent. It found R$ 1,375 a month leaking out of my dishwashing routine, and it is about to buy the fix." Show the number on screen first — do not explain before showing. |
| 0:15–0:50 | Chat: paste (never type) a routine, or drop the photo of a handwritten list | The analyst confirms the minutes with you. On-screen text: "no LLM ever produces a money figure — a deterministic Go engine does." |
| 0:50–1:30 | Proposal → approve → consent screen → AP2 receipt in the Ledger | "Approval is mine, not the agent's. The mandate is signed with a P-256 key that no model code path can reach." Show the signed receipt. Keep the "payments are simulated" label visible — honesty scores. |
| 1:30–2:15 | Daily Briefing | "Routes API is called twice, because congestion depends on when you leave. Flood risk comes from 192 occurrences logged by São Paulo's Civil Defense." Show the departure block landing in Calendar. |
| 2:15–2:45 | **Google Cloud proof** — address bar showing `automate-me-288504867090.us-central1.run.app`, then the Cloud Run console page, then `gcloud run services logs read` in a terminal | "The backend is Cloud Run. Two services, ADK Go v2 and Gemini 3.5 Flash, agent-to-agent over A2A." |
| 2:45–3:00 | Architecture diagram (`docs/design/architecture.png`) | One line on the loop: capture → price → rank → execute → prove. |

Record in short clips so one bad take does not cost the whole video.

---

## Do this in order

1. **Push the repo.** `gh repo create automate-me --public --source=. --remote=origin --push` — nothing can be submitted without a repo URL. *(§5)*
2. **Warm the live URL** and confirm it still returns 200. *(§7)*
3. **Record the video** per §12, upload public to YouTube. *(§4)*
4. **Paste the description** from §9 and the disclosure from §10 into the form. *(§9, §10)*
5. **Upload** `docs/design/architecture.png` to the gallery. *(§6)*
6. **Select Track 1 — Taskmaster**, answer SDK = ADK Go v2.2.0, start date = 2026-08-14. *(§2, §8)*
7. Confirm teammate invitations are accepted. *(§3)*
8. If there is time left: LICENSE file, social post. *(§10, §11)*
