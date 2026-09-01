# CONTEXT.md — deltas por sessão

## 4. ATUALIZACAO 2026-08-31 (tarde/noite) — agenda real, Firestore, Memory Bank, onboarding, Product Scout, login

Sessao de design/produto, rodando junto com a sessao de submissao (`automate-me-52`).
Coordenacao por SendMessage; rebase em cima do trabalho dela, nunca force-push.

**O que foi feito:**
- **Agenda real lida, nao so escrita** (`internal/briefing/gcal.go`): `GoogleCalendar` virou
  `EventSource` + `BlockWriter`. `CALENDAR_ID` aceita lista separada por virgula (1a = onde
  escreve). `classify()` separa o dia em `trip` / `remote` / `no_place` / `ignored` — ignora
  all-day, out-of-office, recusados e **os blocos que o proprio app escreveu** (senao briefa
  viagem pra viagem). Origem encadeia do compromisso anterior (<4h), senao endereco do perfil /
  `HOME_ADDRESS`. Link colado sem `https://` conta como remoto.
- **Agenda no front-end**: `GET /app/api/agenda` devolve o dia inteiro com o motivo de cada linha;
  painel "Your day" acima dos cards, com badge da fonte (live Google Calendar vs seeded day).
  Read-only, nao gasta chamada da Routes API.
- **Sessoes ADK no Firestore** (`internal/fsession`): `session.Service` completo (partial nunca
  persiste, `app:`/`user:`/`temp:` por prefixo, `temp:` descartado, merge com prefixos de volta).
  Estado como JSON text; id do evento = contador zero-padded (le em ordem sem indice composto).
  Suite `sessiontestsuite` do proprio ADK **verde contra o Firestore real**. `FIRESTORE_PROJECT` liga.
- **Memory Bank** (`internal/memorybank`): Vertex AI Agent Engine Memory Bank por `user_id` via
  `DirectContentsSource` (o client do ADK alimenta de sessao hospedada no Agent Engine; as nossas
  estao no Firestore). Chat: `preload_memory` + `load_memory` + callback pos-turno. Voz: recall
  injetado na system instruction no inicio da chamada, transcript enviado no fim. `MEMORY_ENGINE`
  liga; engine `8839411031563304960`.
- **Onboarding + perfil** (`internal/profile`): taxa digitada ou derivada da renda
  (`renda / (horas por semana * 52/12)`), casa/trabalho/modelo de trabalho. `GET/PUT
  /app/api/profile` + tools `get_profile`/`set_profile` no orquestrador e na voz. Trocar a taxa
  **re-roda o proposer**. Tela de setup em 3 passos + barra de taxa no topo do P&L. Seed ja nasce
  `onboarded` pra demo cair direto no dashboard.
- **Product Scout**: 4o sub-agente, `gemini-3.5-flash` com `geminitool.GoogleSearch{}` como **unica**
  tool. Tool `find_products` pra voz. Link chega por 3 caminhos (markdown, URL solta, resposta
  inteira renderizada em markdown).
- **Chamada de voz persiste** (`internal/calls`): transcript no Firestore, `GET/DELETE
  /app/api/live/transcript`, restaurado ao reabrir a aba. Ao encerrar, o grafo le o transcript e
  chama `add_routine_task` pras rotinas faladas (instruido a nao salvar nada se so houve pergunta).
- **Login**: basic auth no app (`APP_PASSWORD` do secret `app-password`, `APP_USER=reviewer`),
  comparacao em tempo constante, `/health` aberto. Job `briefing-daily` atualizado com o header
  Authorization, senao o briefing das 06:00 quebrava em silencio.
- `docs/design/architecture.png` refeito + `scripts/render-diagrams.py` (Playwright, paleta da
  marca). `PROJECT_STORY.md` atualizado (arquivo agora fora do git, so em disco).

**Deploys:** `automate-me-00022-fnr` (app) e `merchant-agent-00008-ctw`.

**Gotchas:**
- Firestore recusa **leitura depois de escrita** na mesma transacao (pegou no teste de app-state) e
  exige indice composto pra ordem **descendente** por `__name__` — `limitToLast` e essa ordem por
  baixo e ainda por cima nao streama. Ascendente + corte em Go nao precisa de indice nenhum.
- Google Search nativo **nao mistura** com function declarations, e o ADK da `transfer_to_agent` a
  todo sub-agente → o Scout precisa de `tool_config.include_server_side_tool_invocations`.
- Callback context do ADK **recusa** `Session()` e `Memory()` — o callback pos-turno rele a sessao
  do store pelo trio (app, user, session) e roda destacado.
- `go.sum` precisa das entradas de `aiplatform/iampb`: o workspace resolvia pelo `go.work.sum` e o
  Docker nao tem workspace (`GOWORK=off go mod tidy`).
- Grounding metadata nao chegou nos eventos da sessao (`Consultation.Sources` volta vazio) — os
  links vem do texto da resposta.
- O orquestrador chegou a dizer "I have updated your hourly rate" **sem chamar tool nenhuma**: ele
  recebia a mensagem mas nao tinha as tools de perfil. Carrega as duas agora.

**Pendencias:**
1. **Agenda real ainda nao ligada em producao**: falta o USUARIO compartilhar os calendarios com
   `automate-me-run@automate-me-hack.iam.gserviceaccount.com` ("Fazer alteracoes em eventos");
   depois `CALENDAR_ID=... HOME_ADDRESS=... ONLY=app ./infra/deploy.sh`. Achado que muda a
   prioridade: a agenda real tem **46 compromissos em 7 dias e zero enderecos de rua** (21 links de
   Zoom/Meet, o resto sem local) → o Briefing nao tem o que dizer; o valor esta no Calendar Watcher
   (extrair rotinas recorrentes), nao em rota.
2. Store da aplicacao (tasks/proposals/ledger) continua em memoria; so sessao e chamadas foram pro
   Firestore.
3. Duas taxas (hora de trabalho vs hora pessoal): hoje o leak usa uma taxa so, sem janela de
   expediente — `minutos x frequencia x taxa`, sem teto de 8h nem de 24h.
4. Custo agora inclui Firestore e Memory Bank alem dos 2 Cloud Run com `min-instances=1`.
   Encerrado o hackathon: `MIN_INSTANCES=0 make deploy`.

## 3. ATUALIZACAO 2026-08-31 — Spending Authorization (AP2) e preparo da submissao Devpost

Sessao paralela em worktree (`.claude/worktrees/hackathon-checklist`), rodando junto com a sessao
de design (`automate-me-d8`). Coordenacao por SendMessage; nenhuma colisao de arquivo.

**O que foi feito:**
- **Repo publicado**: https://github.com/mikaelzzzz/automate-me (publico). Antes disso nao existia
  remote nenhum — era o bloqueador #1 da submissao. Scan de segredos rodado na working tree E no
  historico completo antes de publicar: limpo. Publico dispensa dar acesso a `testing@devpost.com`
  e `cloudhackathons@google.com`.
- **Spending Authorization (AP2)** — o agente compra sozinho sob envelope assinado pelo usuario:
  - `ap2core/authorization.go`: `SignSpendingAuthorization` / `VerifySpendingAuthorization` /
    `Permits` (teto por compra, allowlist de merchant, expiracao dura, moeda exata sem conversao).
    Namespace `automate.spending_authorization.1` — **nao e artefato AP2**, documentado como
    extensao (AP2 v0.2 define exatamente 4 vct e esse nao e nenhum).
  - `app/internal/trusted/`: caminhos consentido e autonomo compartilham uma `execute` com `gate`
    opcional. O gate roda **depois** do Checkout JWT verificado e **antes** da primeira assinatura,
    contra o total **assinado pelo merchant** (nunca preco calculado local). Recusa nao assina nada
    e nao deixa registro. Consentimento explicito vence o envelope.
  - `app/internal/httpapi/authority.go`: GET/POST/DELETE `/app/api/trusted/authority`. Conceder e
    acao do Trusted Surface, nunca tool de agente.
  - `approve` (HTTP e tool) tenta a compra autonoma e devolve `autonomous{purchased,needs_consent,
    reason,total_cents,mandate_record_id}` **ao lado** dos campos que a SPA ja lia.
  - Demo semeia envelope de R$1.000 (`DEMO_SPENDING_CAP_CENTS` sobrescreve). Fora de demo, nada
    autorizado ate o usuario conceder.
- **catalog**: `grocery-delivery` virou `ClassExecutable` (merchant vende, o rail compra — classificar
  como conselho era a inconsistencia). Bug real corrigido junto: faltava o trigger `supermarket`, e
  o matcher e `strings.Contains`, entao a tarefa semeada "Supermarket run" nao casava com receita
  nenhuma — receita existia, produto existia, proposta nunca nascia, **nada dava erro**.
- **`teams-report` removido** (pedido do usuario): era `ClassExecutable` com `CapReportGen` mas nada
  o executava. `CapReportGen` saiu junto. Catalogo agora 25 receitas (8 executable, 8 advised, 9 roadmap).
- **UI**: header do Talk diz `Live Voice · reasoning gemini-3.5-flash` em vez de expor
  `gemini-3.1-flash-live-preview` — o nome ao lado do 3.5 fazia parecer violacao da regra "Gemini 3.5+".
  Escondido o rotulo, **nao** o fato: README e PROJECT_STORY explicam por que o Live e 3.1.

**Deploys:** varios (`ONLY=app`). Ultimo verificado por mim: rev 00011 (feature) → depois a sessao
de design levou ate rev 00022 (basic auth). Verificado ao vivo em producao: mercado R$350 compra
sozinho (`mnd-chk_1`), lava-loucas R$3.000 devolve `unresolved_constraint` e fica `approved`,
consentimento manual compra. Checkout recusado nao virou MandateRecord.

**Gotchas:**
- **Store e em memoria** — aprovar algo persiste na instancia. Reset antes de gravar/demonstrar:
  `GCP_PROJECT=automate-me-hack ONLY=app ./infra/deploy.sh`. Ensaio de voz tambem suja o P&L
  (chamada encerrada grava rotinas de verdade).
- Portas 8080/8081/8082/8083/8090 ocupadas nesta maquina. Local: merchant 8191, app 8192+.
- Docs de submissao (`PROJECT_STORY.md`, `SUBMISSION_ANSWERS.md`, `SUBMISSION_CHECKLIST.md`,
  `VIDEO_SCRIPT.md`) foram **removidos do git** a pedido do usuario e gitignorados. Continuam em
  disco. Ainda existem no historico — limpar exige force-push, deixado pra depois do prazo.
- Senha do reviewer nunca foi escrita em arquivo do repo (repo publico). So no form do Devpost.

**Pendencias:**
- Vídeo demo nao gravado ate o fechamento desta sessao. Roteiro pronto em `VIDEO_SCRIPT.md`.
- Submissao Devpost nao confirmada.
- Post social (#AllThingsAgenticHackathon) — bonus, nao feito.
- Plan Guardian segue pos-hackathon: o Ledger so projeta, nunca confirma.

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
