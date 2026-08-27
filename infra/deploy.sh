#!/usr/bin/env bash
# Build both images on Cloud Build and roll them out to Cloud Run.
#
#   GCP_PROJECT=automate-me-hack ./infra/deploy.sh          # both services
#   ONLY=app ./infra/deploy.sh   |   ONLY=merchant ./infra/deploy.sh
#   SKIP_BUILD=1 ./infra/deploy.sh                          # redeploy last built tag
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
  log "deploy automate-me (public)"
  gcloud run deploy automate-me --project="$PROJECT_ID" --region="$REGION" \
    --image="$REPO/app:$TAG" \
    --service-account="$RUN_SA" \
    --allow-unauthenticated \
    --min-instances="$MIN_INSTANCES" --max-instances=1 \
    --memory=512Mi --cpu=1 --timeout=300 \
    --set-env-vars="MERCHANT_URL=$MERCHANT_URL,MERCHANT_AUTH=idtoken,DEMO_MODE=seed,GEMINI_MODEL=$GEMINI_MODEL" \
    --set-secrets="GOOGLE_API_KEY=$SECRET_NAME:latest" \
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
