# DESIGN_SPEC.md — Automate.me

> Primary references: **`PRD.md`** (product, features F1–F10, judging map, timeline) and
> **`docs/superpowers/specs/2026-08-14-automate-me-design.md`** (architecture, packages, data model, AP2/A2A).
> This file is the contract summary the dev workflow keys on; on any conflict, the design doc wins.

## Overview

Automate.me is an autonomous multi-agent Taskmaster (Google Cloud "All Things Agentic" hackathon, Track 1) built on **ADK Go v2** with **`gemini-3.5-flash`** (pinned — hackathon requires Gemini 3.5+; do NOT swap models). A user declares their routine by voice (push-to-talk), text, or photo (handwritten lists, boletos, school notes); the system estimates time per task, computes the deterministic **Cost of Inaction** (R$/month lost by not automating), ranks catalog automations by payback, and — with consent — **executes** them: AP2 v0.2 purchase against a merchant agent over A2A, Calendar writes, Maps-optimized daily briefings with flood alerts (São Paulo), Gmail drafts, Teams automation reports. A **Plan Guardian** agent then verifies the plan in action (signals + weekly check-in), splits the Savings Ledger into projected × confirmed, and generates Progress Reports.

Two Cloud Run services: `automate-me` (agent graph + React SPA + REST/SSE API) and `merchant-agent` (A2A server implementing AP2 v0.2 merchant + simulated Credential Provider/Processor). Firestore holds all state; Secret Manager holds ECDSA P-256 keys; Cloud Scheduler drives watcher/briefing/guardian passes.

## Example use cases

1. Photo of a handwritten chore list → structured tasks with time estimates → dashboard shows "R$900/month leaking".
2. Voice: "I wash dishes about an hour a day" → task added, Cost of Inaction updates live → dishwasher proposal, payback 2.1 months → consent → AP2 checkout/payment mandates → receipt + delivery on Calendar.
3. Morning briefing: 3 calendar events with addresses → per-event departure blocks ("Leave at 8:12"), clothing suggestion from weather, flood-risk alert with alternative route; `duration − staticDuration` × hourly rate = "today's traffic cost you R$23".
4. Teams mode: paste team task list + hourly cost → shareable Automation Opportunities Report.
5. Guardian: dishwasher delivered but user still logs dishwashing → drift nudge; weekly check-in confirms savings → ledger entry flips projected→confirmed.

## Tools required (7 canonical capabilities)

`vision` (Gemini multimodal) · `calendar_write` (Google Calendar API, OAuth `calendar.events`) · `maps_routes` (Routes API, existing key) · `weather_flood` (Weather API `forecast/hours` + `publicAlerts` FLOOD events; static GeoSampa GeoJSON layer) · `gmail_draft` (OAuth `gmail.compose`) · `ap2_purchase` (A2A → merchant, AP2 v0.2) · `report_gen`.

## Constraints & safety rules

- Money math ONLY in the deterministic Value Engine (`internal/engine`, pure Go, table-driven tests). LLMs never compute R$.
- Mandate signing ONLY in the non-agentic Trusted Surface after explicit user click; ECDSA P-256 (never Ed25519); exact `vct` match; checkout-hash binding. No LLM-adjacent code path can sign.
- Payment settlement is SIMULATED and labeled as such everywhere.
- A2A only through adk-go's `adka2a` surface (both services); never import a2a-go v2.x.
- Purchases, Gmail drafts, and Calendar writes require prior user approval of the proposal (human-in-the-loop); drafts are never sent automatically.
- Quality gates on every CI run: `gofmt`, `go vet`, `go fix`, `golangci-lint`, `go test -race ./...`.
- `DEMO_MODE=seed` must always boot a fully working demo without external OAuth.

## Success criteria

- End-to-end loop live on Cloud Run by Aug 26; uncut 4-min demo shows ≥3 autonomous actions (defined in PRD §9).
- Value Engine + AP2 mandate tests green with `-race`; A2A+AP2 integration happy path + 3 failure paths.
- Submission complete (repo, README spin-up, architecture diagram, video with GCP proof) + both bonus actions.

## Edge cases

Illegible photo → ask, never guess silently · uncertain time estimate → confirm with user · negative-payback automation → never proposed · merchant timeout → idempotent retry by checkout ID · tampered/mismatched mandate → abort + audit entry · Weather/Maps outage → briefing degrades to route-only/card-omitted · empty flood day (dry season) → GeoSampa historic layer narrative · OAuth failure live → seed-mode kill switch · Guardian slip past day-12 gate → degraded demo closing segment (PRD §8).
