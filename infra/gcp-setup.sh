#!/usr/bin/env bash
# One-time GCP bootstrap for the hackathon project. Idempotent: safe to re-run.
#
#   GCP_PROJECT=automate-me-hack ./infra/gcp-setup.sh
#
# Never targets the gcloud default project: every call passes --project.
set -euo pipefail

PROJECT_ID="${GCP_PROJECT:-automate-me-hack}"
PROJECT_NAME="${GCP_PROJECT_NAME:-Automate-me Hackathon}"
ORG_ID="${ORG_ID:-995455311519}"                      # flowmika.com
BILLING_ACCOUNT="${BILLING_ACCOUNT:-01AB35-4D0433-156DEF}"
REGION="${REGION:-us-central1}"
AR_REPO="${AR_REPO:-automate-me}"
RUN_SA_NAME="${RUN_SA_NAME:-automate-me-run}"
SECRET_NAME="${SECRET_NAME:-google-api-key}"
BUDGET="${BUDGET:-500BRL}"                          # must match the billing account currency

log() { printf '\n\033[1;34m==> %s\033[0m\n' "$*"; }
# retry <n> <cmd...> — newly created service accounts take a few seconds to
# become visible to IAM; bindings fail with "does not exist" until then.
retry() {
  local n=$1; shift
  local i
  for ((i = 1; i <= n; i++)); do
    "$@" && return 0
    [[ $i -lt $n ]] && { echo "  retry $i/$n in 5s…"; sleep 5; }
  done
  return 1
}

if [[ "$PROJECT_ID" == "ecosistema-karol-prod" ]]; then
  echo "refusing to touch ecosistema-karol-prod (production of another product)" >&2
  exit 1
fi

# ---------------------------------------------------------------- project ---
log "project $PROJECT_ID"
if gcloud projects describe "$PROJECT_ID" --format='value(projectId)' >/dev/null 2>&1; then
  echo "exists"
else
  if ! gcloud projects create "$PROJECT_ID" --name="$PROJECT_NAME" --organization="$ORG_ID"; then
    cat >&2 <<MSG
project creation failed. Project IDs are global; if "$PROJECT_ID" is taken,
re-run with another id, e.g.:  GCP_PROJECT=automate-me-hack-2026 $0
MSG
    exit 1
  fi
fi
PROJECT_NUMBER="$(gcloud projects describe "$PROJECT_ID" --format='value(projectNumber)')"

log "billing → $BILLING_ACCOUNT"
CURRENT_BILLING="$(gcloud billing projects describe "$PROJECT_ID" --format='value(billingAccountName)' 2>/dev/null || true)"
if [[ "$CURRENT_BILLING" == "billingAccounts/$BILLING_ACCOUNT" ]]; then
  echo "already linked"
else
  gcloud billing projects link "$PROJECT_ID" --billing-account="$BILLING_ACCOUNT" >/dev/null
fi

# ------------------------------------------------------------------- APIs ---
log "APIs"
gcloud services enable --project="$PROJECT_ID" \
  run.googleapis.com \
  cloudbuild.googleapis.com \
  artifactregistry.googleapis.com \
  secretmanager.googleapis.com \
  iam.googleapis.com \
  cloudresourcemanager.googleapis.com \
  compute.googleapis.com \
  logging.googleapis.com \
  cloudtrace.googleapis.com \
  aiplatform.googleapis.com \
  billingbudgets.googleapis.com \
  routes.googleapis.com \
  weather.googleapis.com \
  cloudscheduler.googleapis.com

# ------------------------------------------------------ public access ---
# The flowmika.com org enforces Domain Restricted Sharing
# (iam.allowedPolicyMemberDomains), which rejects allUsers on Cloud Run.
# Override it for THIS project only so the demo dashboard can be public.
# Needs roles/orgpolicy.policyAdmin on the org. Reversible:
#   gcloud org-policies delete iam.allowedPolicyMemberDomains --project=$PROJECT_ID
log "org policy: allow public members on $PROJECT_ID only"
gcloud services enable orgpolicy.googleapis.com --project="$PROJECT_ID" >/dev/null
POLICY_FILE="$(mktemp)"
cat >"$POLICY_FILE" <<POLICY
name: projects/$PROJECT_ID/policies/iam.allowedPolicyMemberDomains
spec:
  rules:
    - allowAll: true
POLICY
gcloud org-policies set-policy "$POLICY_FILE" --project="$PROJECT_ID" >/dev/null \
  || echo "could not set project-level org policy (need orgpolicy.policyAdmin); public deploy will fail on allUsers"
rm -f "$POLICY_FILE"

# ------------------------------------------------------- Artifact Registry ---
log "Artifact Registry $AR_REPO ($REGION)"
gcloud artifacts repositories describe "$AR_REPO" --project="$PROJECT_ID" --location="$REGION" >/dev/null 2>&1 \
  || gcloud artifacts repositories create "$AR_REPO" --project="$PROJECT_ID" --location="$REGION" \
       --repository-format=docker --description="Automate.me service images"

# ----------------------------------------------------- runtime identity ---
RUN_SA="$RUN_SA_NAME@$PROJECT_ID.iam.gserviceaccount.com"
log "runtime service account $RUN_SA"
gcloud iam service-accounts describe "$RUN_SA" --project="$PROJECT_ID" >/dev/null 2>&1 \
  || gcloud iam service-accounts create "$RUN_SA_NAME" --project="$PROJECT_ID" \
       --display-name="Automate.me Cloud Run runtime"
for role in roles/logging.logWriter roles/cloudtrace.agent roles/aiplatform.user roles/datastore.user; do
  retry 8 gcloud projects add-iam-policy-binding "$PROJECT_ID" \
    --member="serviceAccount:$RUN_SA" --role="$role" --condition=None --quiet >/dev/null
done

# ------------------------------------------------------ Cloud Build identity ---
# New projects run Cloud Build as the Compute Engine default SA; org policy may
# strip its automatic Editor grant, so grant what `gcloud builds submit` needs.
BUILD_SA="$PROJECT_NUMBER-compute@developer.gserviceaccount.com"
log "Cloud Build identity $BUILD_SA"
for role in roles/cloudbuild.builds.builder roles/artifactregistry.writer roles/logging.logWriter roles/storage.objectAdmin; do
  retry 8 gcloud projects add-iam-policy-binding "$PROJECT_ID" \
    --member="serviceAccount:$BUILD_SA" --role="$role" --condition=None --quiet >/dev/null
done

# ------------------------------------------------------------- Gemini key ---
# AI Studio key lives in Secret Manager; Cloud Run mounts it as GOOGLE_API_KEY.
# Source of the value: $GOOGLE_API_KEY, else app/.env.
log "secret $SECRET_NAME"
if [[ -z "${GOOGLE_API_KEY:-}" && -f app/.env ]]; then
  GOOGLE_API_KEY="$(sed -n 's/^GOOGLE_API_KEY=//p' app/.env | head -1 | tr -d "\"'")"
fi
gcloud secrets describe "$SECRET_NAME" --project="$PROJECT_ID" >/dev/null 2>&1 \
  || gcloud secrets create "$SECRET_NAME" --project="$PROJECT_ID" --replication-policy=automatic
if [[ -n "${GOOGLE_API_KEY:-}" ]]; then
  LATEST="$(gcloud secrets versions access latest --secret="$SECRET_NAME" --project="$PROJECT_ID" 2>/dev/null || true)"
  if [[ "$LATEST" == "$GOOGLE_API_KEY" ]]; then
    echo "latest version already holds this key"
  else
    printf '%s' "$GOOGLE_API_KEY" | gcloud secrets versions add "$SECRET_NAME" --project="$PROJECT_ID" --data-file=- >/dev/null
    echo "new version added"
  fi
else
  echo "no GOOGLE_API_KEY in env or app/.env — add one later with:"
  echo "  printf '%s' \"\$KEY\" | gcloud secrets versions add $SECRET_NAME --project=$PROJECT_ID --data-file=-"
fi
retry 8 gcloud secrets add-iam-policy-binding "$SECRET_NAME" --project="$PROJECT_ID" \
  --member="serviceAccount:$RUN_SA" --role=roles/secretmanager.secretAccessor --quiet >/dev/null

# ------------------------------------------------- Maps Platform key ---------
# Routes + Weather power the Daily Briefing. The key is restricted to those two
# APIs and stored in Secret Manager; MAPS_API_KEY in app/.env wins if present.
MAPS_SECRET="${MAPS_SECRET:-maps-api-key}"
log "secret $MAPS_SECRET (Routes + Weather)"
if [[ -z "${MAPS_API_KEY:-}" && -f app/.env ]]; then
  MAPS_API_KEY="$(sed -n 's/^MAPS_API_KEY=//p' app/.env | head -1 | tr -d "\"'")"
fi
if [[ -z "${MAPS_API_KEY:-}" ]]; then
  KEY_NAME="$(gcloud services api-keys list --project="$PROJECT_ID" \
    --filter="displayName:automate-me maps" --format='value(name)' 2>/dev/null | head -1)"
  if [[ -z "$KEY_NAME" ]]; then
    echo "creating a restricted Maps key…"
    KEY_NAME="$(gcloud services api-keys create --project="$PROJECT_ID" \
      --display-name="automate-me maps (routes+weather)" \
      --api-target=service=routes.googleapis.com \
      --api-target=service=weather.googleapis.com \
      --format='value(response.name)' 2>/dev/null | head -1)"
    # `create` returns the operation; resolve the key resource either way.
    [[ -z "$KEY_NAME" ]] && KEY_NAME="$(gcloud services api-keys list --project="$PROJECT_ID" \
      --filter="displayName:automate-me maps" --format='value(name)' | head -1)"
  fi
  if [[ -n "$KEY_NAME" ]]; then
    MAPS_API_KEY="$(gcloud services api-keys get-key-string "$KEY_NAME" --format='value(keyString)' 2>/dev/null)"
  fi
fi
gcloud secrets describe "$MAPS_SECRET" --project="$PROJECT_ID" >/dev/null 2>&1 \
  || gcloud secrets create "$MAPS_SECRET" --project="$PROJECT_ID" --replication-policy=automatic
if [[ -n "${MAPS_API_KEY:-}" ]]; then
  LATEST_MAPS="$(gcloud secrets versions access latest --secret="$MAPS_SECRET" --project="$PROJECT_ID" 2>/dev/null || true)"
  if [[ "$LATEST_MAPS" == "$MAPS_API_KEY" ]]; then
    echo "latest version already holds this key"
  else
    printf '%s' "$MAPS_API_KEY" | gcloud secrets versions add "$MAPS_SECRET" --project="$PROJECT_ID" --data-file=- >/dev/null
    echo "new version added"
  fi
else
  echo "no Maps key available — the Daily Briefing will report itself unavailable"
fi
retry 8 gcloud secrets add-iam-policy-binding "$MAPS_SECRET" --project="$PROJECT_ID" \
  --member="serviceAccount:$RUN_SA" --role=roles/secretmanager.secretAccessor --quiet >/dev/null

# --------------------------------------------- Daily Briefing schedule -------
# 06:00 America/Sao_Paulo: the briefing is ready before the user asks.
log "scheduler job briefing-daily"
APP_URL="https://automate-me-$PROJECT_NUMBER.$REGION.run.app"
if gcloud scheduler jobs describe briefing-daily --project="$PROJECT_ID" --location="$REGION" >/dev/null 2>&1; then
  echo "exists"
else
  gcloud scheduler jobs create http briefing-daily --project="$PROJECT_ID" --location="$REGION" \
    --schedule="0 6 * * *" --time-zone="America/Sao_Paulo" \
    --uri="$APP_URL/app/api/briefing/run" --http-method=POST --attempt-deadline=120s \
    --description="Automate.me Daily Briefing: plan the day before the user asks" >/dev/null \
    || echo "scheduler job not created (non-fatal); the UI's 'Plan my day' button still works"
fi

# ------------------------------------------------------------- budget alert ---
# Budget calls bill their API quota to the gcloud *default* project, where the
# API may be disabled and gcloud would prompt; pin the quota project and never
# prompt. Non-fatal either way.
log "budget alert ($BUDGET)"
if CLOUDSDK_CORE_PROJECT="$PROJECT_ID" gcloud billing budgets list --billing-account="$BILLING_ACCOUNT" \
     --format='value(displayName)' --quiet 2>/dev/null | grep -qx "automate-me-hack"; then
  echo "exists"
else
  CLOUDSDK_CORE_PROJECT="$PROJECT_ID" gcloud billing budgets create --billing-account="$BILLING_ACCOUNT" \
    --display-name="automate-me-hack" --budget-amount="$BUDGET" \
    --filter-projects="projects/$PROJECT_NUMBER" \
    --threshold-rule=percent=0.5 --threshold-rule=percent=0.9 --threshold-rule=percent=1.0 \
    --quiet >/dev/null 2>&1 \
    || echo "budget creation failed (non-fatal); create one in the console: Billing → Budgets & alerts"
fi

# ------------------------------------------------------------- Firestore ---
# ADK sessions live here (internal/fsession), so a conversation survives the
# revision that hosted it. nam5 is the US multi-region; a database can never
# change location, so this one is created once and kept.
log "Firestore (agent sessions)"
gcloud services enable firestore.googleapis.com --project="$PROJECT_ID" --quiet
gcloud firestore databases describe --database='(default)' --project="$PROJECT_ID" >/dev/null 2>&1 \
  || gcloud firestore databases create --database='(default)' --project="$PROJECT_ID" \
       --location="${FIRESTORE_LOCATION:-nam5}" --type=firestore-native --quiet

# ----------------------------------------------------------- Memory Bank ---
# One Agent Engine instance holds the agent's long-term memory (facts about the
# user, scoped per user_id). No code is deployed to it — Memory Bank is used on
# its own, from Cloud Run, through internal/memorybank.
log "Agent Engine (Memory Bank)"
AE_API="https://$REGION-aiplatform.googleapis.com/v1beta1/projects/$PROJECT_ID/locations/$REGION/reasoningEngines"
AE_TOKEN="$(gcloud auth print-access-token)"
MEMORY_ENGINE="$(curl -sf "$AE_API" -H "Authorization: Bearer $AE_TOKEN" \
  | python3 -c 'import json,sys; d=json.load(sys.stdin); print(next((e["name"].rsplit("/",1)[1] for e in d.get("reasoningEngines",[]) if e.get("displayName")=="automate-me-memory"), ""))' 2>/dev/null || true)"
if [[ -z "$MEMORY_ENGINE" ]]; then
  curl -sf -X POST "$AE_API" -H "Authorization: Bearer $AE_TOKEN" -H "Content-Type: application/json" \
    -d '{"displayName":"automate-me-memory","description":"Memory Bank for Automate.me: what the agent remembers about each user"}' >/dev/null
  # Creation is a long-running operation; the instance shows up in seconds.
  for _ in $(seq 1 20); do
    sleep 5
    MEMORY_ENGINE="$(curl -sf "$AE_API" -H "Authorization: Bearer $AE_TOKEN" \
      | python3 -c 'import json,sys; d=json.load(sys.stdin); print(next((e["name"].rsplit("/",1)[1] for e in d.get("reasoningEngines",[]) if e.get("displayName")=="automate-me-memory"), ""))' 2>/dev/null || true)"
    [[ -n "$MEMORY_ENGINE" ]] && break
  done
fi
# Memory Bank generates and embeds facts as its own service agent, which needs
# to call the embedding and extraction models.
retry 8 gcloud projects add-iam-policy-binding "$PROJECT_ID" \
  --member="serviceAccount:service-$PROJECT_NUMBER@gcp-sa-aiplatform-re.iam.gserviceaccount.com" \
  --role=roles/aiplatform.user --condition=None --quiet >/dev/null

cat <<MSG

ready: $PROJECT_ID (number $PROJECT_NUMBER, region $REGION)
memory: agent engine ${MEMORY_ENGINE:-<creation pending — re-run this script>}
next:  GCP_PROJECT=$PROJECT_ID REGION=$REGION MEMORY_ENGINE=$MEMORY_ENGINE ./infra/deploy.sh
MSG
