# CONTEXT.md — deltas por sessão

## 1. ATUALIZACAO 2026-08-16 — Fundação completa: specs, AP2 ponta a ponta, dashboard Crextio

**O que foi feito:**
- Brainstorm completo → PRD.md + design doc (`docs/superpowers/specs/2026-08-14-automate-me-design.md`), 2 rodadas de revisão adversarial por subagente, 17 findings resolvidas. Produto: Automate.me, hackathon "All Things Agentic" (Track 1 Taskmaster, deadline submissão 31/08 17:00 PT).
- Pesquisa verificada em fonte primária: AP2 v0.2 (2 closed mandates, ECDSA, vct exatos), ADK Go v2.2.0 GA, A2A v1.0.1, regras/julgamento do hackathon (40/30/30), Weather API GA cobre BR com alertas FLOOD, Flood Forecasting API descartada, GeoSampa como camada histórica. Transcrições em `docs/research/` (ap2-v02-schema.md com test vectors verificados; adk-go-v2-cheatsheet.md compilado contra a tag).
- Workspace Go 1.26.5: módulos `app/`, `merchant/`, `ap2core/` (crypto compartilhada). Tudo `go test -race` verde.
- `ap2core`: Checkout JWT, closed mandates (mandate.checkout.1/payment.1), recibos Success/Error, JWK P-256, alg-confusion rejeitado. Desvio documentado: receipt `reference` = hash da folha JWS (maneira do SDK oficial, não da prosa da spec).
- `merchant`: domínio AP2 (checkout com JWK pinado, verificação bilateral, recibo SEMPRE, idempotência, liquidação simulada) + trilho HTTP determinístico `/ap2/*` + superfície A2A opcional (card + search_catalog via adka2a/v2). JWS nunca passa por LLM (MUST #2 da spec).
- `app`: Value Engine determinístico (centavos int64), catálogo 27 receitas como dados (enum 7 capacidades), store Memory+seed demo, grafo ADK (orquestrador ModeChat + routine_analyst + automation_advisor, approve com tool confirmation), adkrest em `/api`, API dashboard em `/app/api`, Trusted Surface não-agentic executando a dança AP2 completa no consent.
- SPA `app/web`: dashboard Crextio Warm Minimal (âncora anual Fraunces, KPI pills, antes/depois, curva projetado×confirmado com paleta validada #A07C12/#2C5FA8, consent modal com os 4 artefatos AP2 decodificados, chat drawer com protocolo adk_request_confirmation, ledger). Verificado por screenshot Playwright.
- Smoke test ponta a ponta pela rede: P&L → approve → consent → mandates → recibos assinados → ledger com mandate_ref. FUNCIONA.
- CI: gofmt, vet, go fix sem drift, golangci-lint, test -race, build web (3 módulos na matriz).

**Deploys:** nenhum (bloqueado em decisão de projeto GCP).

**Gotchas:**
- `GOFLAGS=-mod=mod` global do usuário foi removido (`go env -u GOFLAGS`) — conflitava com workspace mode.
- `go mod tidy` remove deps não importadas; adk volta quando código importar.
- adk-go: root agent DEVE ser ModeChat; `SaveInputBlobsAsArtifacts:true` esconde imagem do modelo (deixar false pra vision); pacotes A2A usar `/v2`; session state prefixos `app:/user:/temp:`; helpers de state são internal/ (copiar ~30 linhas).
- Rodar local: `make run-merchant` (:8081) + `make run-app` (:8080 com WEB_DIST=web/dist, DEMO_MODE=seed). Chat exige GOOGLE_API_KEY no `app/.env`.
- gcloud ativo aponta pra `ecosistema-karol-prod` (produção da Karol) — NÃO deployar hackathon lá.

**Pendências:**
1. USUÁRIO: criar `app/.env` com GOOGLE_API_KEY (aistudio.google.com/apikey) → testar chat dos agentes (não testado com LLM real ainda).
2. USUÁRIO: decidir projeto GCP dedicado (proposta: `automate-me-hack`) → deploy 2 Cloud Run + Firestore + Secret Manager.
3. USUÁRIO: formulário US$150 créditos GCP até 28/08 12:00 PT (link no PRD §8).
4. Ingestão de foto (lista manuscrita → tarefas; boleto; bilhete escolar) — receitas vision D3-5.
5. Voz push-to-talk (áudio direto no Gemini multimodal) + resposta Cloud TTS.
6. Daily Briefing (Routes departureTime futuro, duration−staticDuration monetizado, Weather forecast+publicAlerts pt-BR, GeoSampa GeoJSON estático) + Calendar Watcher (OAuth: calendar.events + gmail.compose, task cedo).
7. Firestore session.Service (contrato de 8 pontos no cheat sheet §5.2; sessiontestsuite dá conformidade grátis) — fallback documentado: Cloud Run 1 instância.
8. Teams report, Savings Ledger real, Plan Guardian (gate dia 12), landing page.
9. D13-17: vídeo 4min (roteiro PRD §8, 230s + folga), diagrama, README spin-up, blog + post #AllThingsAgenticHackathon, submissão Devpost antecipada.
