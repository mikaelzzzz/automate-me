# CONTEXT.md — deltas por sessão

## 2. ATUALIZACAO 2026-08-31 — GCP, deploy, Briefing, redesign Command Rail e voz Live API

**O que foi feito:**
- **Infra GCP do zero** (`infra/gcp-setup.sh` + `deploy.sh` + `cloudbuild.yaml`): projeto `automate-me-hack` (nº 288504867090, us-central1), billing, 14 APIs, Artifact Registry, SA `automate-me-run`, Secret Manager (`google-api-key`, `maps-api-key`), budget R$500, job Scheduler `briefing-daily` 06:00 BRT. Idempotente — re-rodar diz "already/exists".
- **Deploy funcionando**: `merchant-agent` privado (só a SA do app tem `run.invoker`; app autentica com ID token via `MERCHANT_AUTH=idtoken`) + `automate-me` público. Ambos `--max-instances=1` (estado em memória). Hoje na rev **00007**.
- **Daily Briefing (F5)** em `app/internal/briefing/`: Routes API com `departureTime` futuro em duas passadas, trânsito precificado `(duration − staticDuration) × rate`, Weather hourly + publicAlerts, e camada **GeoSampa** (192 pontos de alagamento da Defesa Civil, embutidos) casada a ≤150 m da polyline. Endpoints `/app/api/briefing[/run|/{id}/block]`, sub-agente `day_planner`.
- **Ingestão de foto (F1)**: composer anexa imagem (≤1280px JPEG → `inlineData`), analyst lê lista manuscrita/boletos e salva as tarefas. Testado: 7 itens extraídos de uma lista.
- **Redesign "Command Rail"** (wireframe 1a do Claude Design): rail de capacidades à esquerda, feed "Agent activity" derivado de estado real à direita, **tabela de payback** como espinha do P&L, tela Proposals, consent split com a cadeia AP2. Paleta da marca (teal `#13353F`, dourado `#BC9A75`), Fraunces nos números.
- **Voz em tempo real (Gemini Live API)**: aba **Talk**, waveform em canvas alimentada por `AnalyserNode` nos dois lados, transcrição, barge-in. Browser fala direto com a Live API via **token efêmero de uso único** (chave nunca vai pro browser); function calls voltam pra `POST /app/api/live/tool`.
- **`internal/agents/core.go`**: corpos das tools extraídos dos closures do ADK → grafo e voz executam a mesma função. `internal/proposer` idem para o matching catálogo↔engine (usado pelo tool e pelo seed).
- **Regra do hackathon (Gemini 3.5+) resolvida**: não existe modelo Live conversacional 3.5 (só `3.5-transcribe-live` e `3.5-live-translate`, ambos especializados — verificado na API de modelos). Solução: tool **`consult_specialist`** entrega qualquer julgamento ao grafo ADK em `gemini-3.5-flash`, que roteia pros sub-agentes e devolve a resposta pra voz falar. UI mostra a cadeia.
- **README + diagramas** (arquitetura e sequência AP2, mermaid + PNG em `docs/design/`).

**Deploys:** `automate-me-00007-4xg` e `merchant-agent-00007-tcs`. App público: https://automate-me-288504867090.us-central1.run.app · merchant privado (403 sem ID token). Repo público: https://github.com/mikaelzzzz/automate-me (main = `ffce118`).

**Gotchas:**
- Cota de billing era 5/5 projetos → `corretorautomatico` (dormente) foi desvinculado. `corretorzapsignmikael` também dormente se precisar de outra vaga.
- Org flowmika.com tem Domain Restricted Sharing → `allUsers` era rejeitado e `gcloud run deploy --allow-unauthenticated` só **avisa**. `gcp-setup.sh` põe override `allowAll` **só neste projeto** (propaga em ~2 min); `deploy.sh` faz o binding explícito e fatal.
- Budget precisa ser na moeda da conta (**BRL**), não USD.
- `go.mod` usa `replace ../ap2core` → Dockerfiles buildam com **contexto na raiz** do repo, nunca `--source app`.
- mermaid: `initialize()` antes de qualquer `await`, senão o auto-run pinta com tema default; e esperar `document.fonts.ready` senão os títulos cortam.
- Porta 8081 é do Expo do Ecosistema-Karol nesta máquina → merchant local em `PORT=8082/8083`.
- Sessão paralela trabalhou em `.claude/worktrees/hackathon-checklist` (branch `worktree-hackathon-checklist`), produziu LICENSE + SUBMISSION_ANSWERS.md + SUBMISSION_CHECKLIST.md. `.claude/worktrees/` está no .gitignore.

**Pendências:**
1. **Conectar a agenda real** (próximo passo): `CALENDAR_ID` é lido em `app/cmd/server/main.go:76` e `internal/briefing/gcal.go` está pronto (ADC + `CalendarEventsScope`), mas **nada disso está ligado** — falta habilitar `calendar-json.googleapis.com`, compartilhar um Google Calendar com `automate-me-run@automate-me-hack.iam.gserviceaccount.com` ("Fazer alterações em eventos") e passar `CALENDAR_ID` no `--set-env-vars` do `infra/deploy.sh`. Hoje os blocos saem como `simulated`.
2. Compromissos do briefing são **seed** (`briefing.DemoAppointments`) — ler eventos reais da agenda é o passo seguinte ao item 1.
3. Store é memória (1 instância por serviço). Firestore `session.Service` continua roadmap.
4. Calendar Watcher (F7), Teams report (F8), Plan Guardian (F10) não implementados — telas Teams/Guardian dizem o que farão.
5. Vídeo 4min + submissão Devpost (respostas prontas em `SUBMISSION_ANSWERS.md`).
6. Custo: 2 Cloud Run com `min-instances=1` ≈ R$2-3/dia. Depois do hackathon: `MIN_INSTANCES=0 make deploy`.

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
