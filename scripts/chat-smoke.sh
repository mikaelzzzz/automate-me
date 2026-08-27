#!/usr/bin/env bash
# Drive the agent graph through adkrest exactly like the SPA does:
# list-apps → create session → /api/run per message. Prints agent text,
# tool calls and pending confirmations so a transcript can be reviewed.
#
#   ./scripts/chat-smoke.sh                                  # local, default script
#   BASE=https://automate-me-<n>.us-central1.run.app ./scripts/chat-smoke.sh
#   ./scripts/chat-smoke.sh "I wash dishes an hour a day" "what should I automate first?"
set -euo pipefail
BASE="${BASE:-http://localhost:8080}"
USER_ID="${USER_ID:-demo}"
command -v jq >/dev/null || { echo "jq required (brew install jq)" >&2; exit 1; }

if [[ $# -gt 0 ]]; then
  MESSAGES=("$@")
else
  MESSAGES=(
    "Hi. I wash the dishes by hand for about an hour every day, and I spend around 90 minutes a week at the supermarket."
    "Yes, those numbers are right."
    "Show me my Life P&L."
    "What should I automate first?"
    "Approve the dishwasher one."
  )
fi

APP="$(curl -sf "$BASE/api/list-apps" | jq -r '.[0]')"
[[ -n "$APP" && "$APP" != null ]] || { echo "no agent app at $BASE/api/list-apps (GOOGLE_API_KEY missing?)" >&2; exit 1; }
SESSION="$(curl -sf -X POST -H 'Content-Type: application/json' -d '{}' "$BASE/api/apps/$APP/users/$USER_ID/sessions" | jq -r .id)"
echo "app=$APP session=$SESSION base=$BASE"

run() { # $1 = newMessage JSON
  curl -sf -X POST "$BASE/api/run" -H 'Content-Type: application/json' \
    -d "$(jq -cn --arg app "$APP" --arg u "$USER_ID" --arg s "$SESSION" --argjson m "$1" \
      '{appName:$app,userId:$u,sessionId:$s,newMessage:$m,streaming:false}')"
}

# Print each event: author, text, tool calls/responses, confirmation requests.
show() {
  jq -r '
    .[] | select(.partial != true and .author != "user") as $e
    | ($e.content.parts // [])[]
    | if .text then "  [\($e.author)] \(.text)"
      elif .functionCall then
        (if .functionCall.name == "adk_request_confirmation"
         then "  [\($e.author)] ?? CONFIRM id=\(.functionCall.id) → \(.functionCall.args.originalFunctionCall.name) \(.functionCall.args.originalFunctionCall.args // {} | tojson)"
         else "  [\($e.author)] → tool \(.functionCall.name)(\(.functionCall.args // {} | tojson))" end)
      elif .functionResponse then "  [\($e.author)] ← \(.functionResponse.name): \(.functionResponse.response | tojson | .[0:200])"
      else empty end'
}

for msg in "${MESSAGES[@]}"; do
  echo; echo "> $msg"
  t0=$(date +%s)
  out="$(run "$(jq -cn --arg t "$msg" '{role:"user",parts:[{text:$t}]}')")"
  echo "$out" | show
  echo "  ($(( $(date +%s) - t0 ))s)"
  # Auto-approve a pending confirmation so the flow reaches approve_proposal.
  cid="$(echo "$out" | jq -r '[.[] | (.content.parts // [])[] | select(.functionCall.name=="adk_request_confirmation") | .functionCall.id] | first // empty')"
  if [[ -n "$cid" ]]; then
    echo "> (confirm $cid)"
    run "$(jq -cn --arg id "$cid" '{role:"user",parts:[{functionResponse:{id:$id,name:"adk_request_confirmation",response:{confirmed:true}}}]}')" | show
  fi
done

echo; echo "dashboard state after chat:"
curl -sf "$BASE/app/api/pnl" | jq -c '{tasks: (.tasks|length), total_hours_month, total_cents_month}'
curl -sf "$BASE/app/api/proposals" | jq -c '[.[] | {id, status, payback_months}]'
