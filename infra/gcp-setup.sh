#!/usr/bin/env bash
# One-time GCP bootstrap for the hackathon project. Idempotent: safe to re-run.
#
#   GCP_PROJECT=automate-me-hack ./infra/gcp-setup.sh
#
# Never targets the gcloud default project: every call passes --project.
set -euo pipefail

PROJECT_ID="${GCP_PROJECT:-automate-me-hack}"
PROJECT_NAME="${GCP_PROJECT_NAME:-Automate.me Hackathon}"
ORG_ID="${ORG_ID:-995455311519}"                      # flowmika.com
BILLING_ACCOUNT="${BILLING_ACCOUNT:-01AB35-4D0433-156DEF}"
REGION="${REGION:-us-central1}"
AR_REPO="${AR_REPO:-automate-me}"
RUN_SA_NAME="${RUN_SA_NAME:-automate-me-run}"
SECRET_NAME="${SECRET_NAME:-google-api-key}"
BUDGET_USD="${BUDGET_USD:-100}"

log() { printf '\n\033[1;34m==> %s\033[0m\n' "$*"; }

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
gcloud billing projects link "$PROJECT_ID" --billing-account="$BILLING_ACCOUNT" >/dev/null

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
  billingbudgets.googleapis.com

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
for role in roles/logging.logWriter roles/cloudtrace.agent roles/aiplatform.user; do
  gcloud projects add-iam-policy-binding "$PROJECT_ID" \
    --member="serviceAccount:$RUN_SA" --role="$role" --condition=None --quiet >/dev/null
done

# ------------------------------------------------------ Cloud Build identity ---
# New projects run Cloud Build as the Compute Engine default SA; org policy may
# strip its automatic Editor grant, so grant what `gcloud builds submit` needs.
BUILD_SA="$PROJECT_NUMBER-compute@developer.gserviceaccount.com"
log "Cloud Build identity $BUILD_SA"
for role in roles/cloudbuild.builds.builder roles/artifactregistry.writer roles/logging.logWriter roles/storage.objectAdmin; do
  gcloud projects add-iam-policy-binding "$PROJECT_ID" \
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
  printf '%s' "$GOOGLE_API_KEY" | gcloud secrets versions add "$SECRET_NAME" --project="$PROJECT_ID" --data-file=- >/dev/null
  echo "new version added"
else
  echo "no GOOGLE_API_KEY in env or app/.env — add one later with:"
  echo "  printf '%s' \"\$KEY\" | gcloud secrets versions add $SECRET_NAME --project=$PROJECT_ID --data-file=-"
fi
gcloud secrets add-iam-policy-binding "$SECRET_NAME" --project="$PROJECT_ID" \
  --member="serviceAccount:$RUN_SA" --role=roles/secretmanager.secretAccessor --quiet >/dev/null

# ------------------------------------------------------------- budget alert ---
log "budget alert (USD $BUDGET_USD)"
if gcloud billing budgets list --billing-account="$BILLING_ACCOUNT" --format='value(displayName)' 2>/dev/null \
   | grep -qx "automate-me-hack"; then
  echo "exists"
else
  gcloud billing budgets create --billing-account="$BILLING_ACCOUNT" --display-name="automate-me-hack" \
    --budget-amount="${BUDGET_USD}USD" --filter-projects="projects/$PROJECT_NUMBER" \
    --threshold-rule=percent=0.5 --threshold-rule=percent=0.9 --threshold-rule=percent=1.0 >/dev/null \
    || echo "budget creation failed (non-fatal); create one in the console"
fi

# Firestore (pendência 7 — session.Service). Uncomment once code uses it:
# gcloud services enable firestore.googleapis.com --project="$PROJECT_ID"
# gcloud firestore databases create --project="$PROJECT_ID" --location="$REGION" --type=firestore-native

cat <<MSG

ready: $PROJECT_ID (number $PROJECT_NUMBER, region $REGION)
next:  GCP_PROJECT=$PROJECT_ID REGION=$REGION ./infra/deploy.sh
MSG
