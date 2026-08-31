#!/usr/bin/env bash
# Build both images on Cloud Build and roll them out to Cloud Run.
#
#   GCP_PROJECT=automate-me-hack ./infra/deploy.sh          # both services
#   ONLY=app ./infra/deploy.sh   |   ONLY=merchant ./infra/deploy.sh
#   SKIP_BUILD=1 ./infra/deploy.sh                          # redeploy last built tag
#
# Long-term memory (Vertex AI Agent Engine Memory Bank):
#   MEMORY_ENGINE=<reasoning engine id> ./infra/deploy.sh
# infra/gcp-setup.sh creates the engine and prints its id; the value is carried
# over from the running service when not passed.
#
# Real calendar (Daily Briefing reads the day and writes departure blocks):
#   CALENDAR_ID=you@example.com,c_personal@group.calendar.google.com \
#   HOME_ADDRESS="Rua X 100, São Paulo" ./infra/deploy.sh
# The calendars must be shared with the runtime service account with
# "make changes to events". Both values are carried over from the running
# service when not passed, so a plain redeploy never silently unplugs them.
#
# Both services keep state in memory (store, idempotency, merchant signing key),
# so they run as exactly one instance. MIN_INSTANCES=0 after the hackathon.
set -euo pipefail

PROJECT_ID="${GCP_PROJECT:?set GCP_PROJECT (never ecosistema-karol-prod)}"
REGION="${REGION:-us-central1}"
AR_REPO="${AR_REPO:-automate-me}"
RUN_SA_NAME="${RUN_SA_NAME:-automate-me-run}"
SECRET_NAME="${SECRET_NAME:-google-api-key}"
MIN_INSTANCES="${MIN_INSTANCES:-1}"
ONLY="${ONLY:-}"
GEMINI_MODEL="${GEMINI_MODEL:-gemini-3.5-flash}"
CALENDAR_ID="${CALENDAR_ID:-}"
HOME_ADDRESS="${HOME_ADDRESS:-}"
MEMORY_ENGINE="${MEMORY_ENGINE:-}"

if [[ "$PROJECT_ID" == "ecosistema-karol-prod" ]]; then
  echo "refusing to deploy to ecosistema-karol-prod" >&2
  exit 1
fi

log() { printf '\n\033[1;34m==> %s\033[0m\n' "$*"; }

cd "$(dirname "$0")/.."
TAG="${TAG:-$(git rev-parse --short HEAD)$([[ -n "$(git status --porcelain)" ]] && echo -dirty || true)}"
REPO="$REGION-docker.pkg.dev/$PROJECT_ID/$AR_REPO"
PROJECT_NUMBER="$(gcloud projects describe "$PROJECT_ID" --format='value(projectNumber)')"
RUN_SA="$RUN_SA_NAME@$PROJECT_ID.iam.gserviceaccount.com"
# Cloud Run deterministic URLs — known before the first deploy, so the merchant
# can advertise itself and the app can pin it in one pass.
MERCHANT_URL="https://merchant-agent-$PROJECT_NUMBER.$REGION.run.app"
APP_URL="https://automate-me-$PROJECT_NUMBER.$REGION.run.app"

if [[ -z "${SKIP_BUILD:-}" ]]; then
  log "cloud build → $REPO/{app,merchant}:$TAG"
  gcloud builds submit --project="$PROJECT_ID" --region="$REGION" \
    --config=infra/cloudbuild.yaml --substitutions="_REPO=$REPO,_TAG=$TAG" .
fi

if [[ -z "$ONLY" || "$ONLY" == merchant ]]; then
  log "deploy merchant-agent (private)"
  gcloud run deploy merchant-agent --project="$PROJECT_ID" --region="$REGION" \
    --image="$REPO/merchant:$TAG" \
    --service-account="$RUN_SA" \
    --no-allow-unauthenticated \
    --min-instances="$MIN_INSTANCES" --max-instances=1 \
    --memory=512Mi --cpu=1 --timeout=120 \
    --set-env-vars="PUBLIC_URL=$MERCHANT_URL,GEMINI_MODEL=$GEMINI_MODEL" \
    --set-secrets="GOOGLE_API_KEY=$SECRET_NAME:latest" \
    --labels="app=automate-me,service=merchant" --quiet
  # only the app's identity may call the merchant rail
  gcloud run services add-iam-policy-binding merchant-agent --project="$PROJECT_ID" --region="$REGION" \
    --member="serviceAccount:$RUN_SA" --role=roles/run.invoker --quiet >/dev/null
fi

if [[ -z "$ONLY" || "$ONLY" == app ]]; then
  # Keep the calendar wiring across redeploys: --set-env-vars replaces the whole
  # set, so an unset CALENDAR_ID would silently drop the app back to seed data.
  current_env() {
    gcloud run services describe automate-me --project="$PROJECT_ID" --region="$REGION" \
      --format="value(spec.template.spec.containers[0].env.filter(\"name:$1\").extract(value).flatten())" 2>/dev/null || true
  }
  [[ -n "$CALENDAR_ID" ]] || CALENDAR_ID="$(current_env CALENDAR_ID)"
  [[ -n "$HOME_ADDRESS" ]] || HOME_ADDRESS="$(current_env HOME_ADDRESS)"
  [[ -n "$MEMORY_ENGINE" ]] || MEMORY_ENGINE="$(current_env MEMORY_ENGINE)"
  # ^@^ delimiter: addresses contain commas.
  # FIRESTORE_PROJECT switches ADK sessions from memory to Firestore;
  # MEMORY_ENGINE switches long-term memory on (Agent Engine Memory Bank).
  APP_ENV="^@^MERCHANT_URL=$MERCHANT_URL@MERCHANT_AUTH=idtoken@DEMO_MODE=seed@GEMINI_MODEL=$GEMINI_MODEL@FIRESTORE_PROJECT=${FIRESTORE_PROJECT:-$PROJECT_ID}"
  if [[ -n "$MEMORY_ENGINE" ]]; then
    APP_ENV="$APP_ENV@MEMORY_ENGINE=$MEMORY_ENGINE@MEMORY_PROJECT=$PROJECT_ID@MEMORY_LOCATION=$REGION"
    echo "memory: agent engine $MEMORY_ENGINE"
  else
    echo "memory: none — the agent starts every conversation as a stranger"
  fi
  if [[ -n "$CALENDAR_ID" ]]; then
    APP_ENV="$APP_ENV@CALENDAR_ID=$CALENDAR_ID"
    [[ -n "$HOME_ADDRESS" ]] && APP_ENV="$APP_ENV@HOME_ADDRESS=$HOME_ADDRESS"
    echo "calendar: $CALENDAR_ID${HOME_ADDRESS:+ (home: $HOME_ADDRESS)}"
  else
    echo "calendar: none — the briefing runs on seeded appointments"
  fi

  # APP_PASSWORD puts a browser credential prompt in front of everything but
  # /health. Judges get a user and a password; without the secret the app is
  # open, which is what local development wants.
  APP_SECRETS="GOOGLE_API_KEY=$SECRET_NAME:latest,MAPS_API_KEY=maps-api-key:latest"
  if gcloud secrets describe app-password --project="$PROJECT_ID" >/dev/null 2>&1; then
    APP_SECRETS="$APP_SECRETS,APP_PASSWORD=app-password:latest"
    APP_ENV="$APP_ENV@APP_USER=${APP_USER:-reviewer}"
    echo "auth: basic, user ${APP_USER:-reviewer} (secret app-password)"
  else
    echo "auth: none — the app is open to anyone with the URL"
  fi

  log "deploy automate-me (public)"
  gcloud run deploy automate-me --project="$PROJECT_ID" --region="$REGION" \
    --image="$REPO/app:$TAG" \
    --service-account="$RUN_SA" \
    --allow-unauthenticated \
    --min-instances="$MIN_INSTANCES" --max-instances=1 \
    --memory=512Mi --cpu=1 --timeout=300 \
    --set-env-vars="$APP_ENV" \
    --set-secrets="$APP_SECRETS" \
    --labels="app=automate-me,service=app" --quiet \
  || { echo "if this failed on allUsers: org policy iam.allowedPolicyMemberDomains blocks public services;" >&2
       echo "run infra/gcp-setup.sh (sets a project-level override) and redeploy." >&2; exit 1; }
  # `gcloud run deploy --allow-unauthenticated` only warns when the allUsers
  # binding is rejected; make the public binding explicit and fatal.
  gcloud run services add-iam-policy-binding automate-me --project="$PROJECT_ID" --region="$REGION" \
    --member=allUsers --role=roles/run.invoker --quiet >/dev/null
fi

log "smoke"
printf 'app      %s /health → ' "$APP_URL"; curl -s -o /dev/null -w '%{http_code}\n' "$APP_URL/health"
printf 'merchant %s /health → ' "$MERCHANT_URL"
curl -s -o /dev/null -w '%{http_code}\n' -H "Authorization: Bearer $(gcloud auth print-identity-token)" "$MERCHANT_URL/health"
printf 'merchant unauthenticated → '; curl -s -o /dev/null -w '%{http_code} (403 expected)\n' "$MERCHANT_URL/health"
echo
echo "dashboard: $APP_URL"
echo "logs:      gcloud run services logs read automate-me --project=$PROJECT_ID --region=$REGION --limit=50"
