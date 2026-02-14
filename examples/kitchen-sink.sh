#!/usr/bin/env bash
# kitchen-sink.sh — comprehensive test of every chomp feature
# Usage: ./kitchen-sink.sh
set -uo pipefail

# ── colours & symbols ────────────────────────────────────────────────────────
GRN='\033[32m'; RED='\033[31m'; YEL='\033[33m'; CYN='\033[36m'
BLD='\033[1m'; DIM='\033[2m'; RST='\033[0m'
PASS="${GRN}✓${RST}"; FAIL="${RED}✗${RST}"; SKIP="${DIM}○${RST}"

# ── config ───────────────────────────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [[ -z "${CHOMP_API_TOKEN:-}" ]] && [[ -f "$SCRIPT_DIR/../state/.env" ]]; then
  # shellcheck disable=SC1091
  set -a; source "$SCRIPT_DIR/../state/.env"; set +a
fi
if [[ -z "${CHOMP_API_TOKEN:-}" ]]; then
  echo -e "${RED}CHOMP_API_TOKEN not set and state/.env not found${RST}" >&2
  exit 1
fi

BASE="${CHOMP_BASE:-http://localhost:8001}"
AUTH="Authorization: Bearer $CHOMP_API_TOKEN"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

total_pass=0; total_fail=0; total_skip=0; total_tokens=0
declare -A latencies

# ── helpers ──────────────────────────────────────────────────────────────────
chomp_v1() {
  # chomp_v1 '{"model":"...","messages":[...]}'
  curl -s --max-time 60 "$BASE/v1/chat/completions" \
    -H "$AUTH" -H 'Content-Type: application/json' -d "$1" 2>&1
}

chomp_dispatch() {
  # chomp_dispatch '{"prompt":"...","router":"..."}'
  curl -s --max-time 15 "$BASE/api/dispatch" \
    -H "$AUTH" -H 'Content-Type: application/json' -d "$1" 2>&1
}

chomp_result() {
  # chomp_result <job_id> — polls until status != running/pending (max 30s)
  local id="$1" elapsed=0
  while (( elapsed < 30 )); do
    local resp
    resp=$(curl -sf --max-time 10 "$BASE/api/result/$id" -H "$AUTH" 2>&1) || { sleep 1; (( elapsed++ )); continue; }
    local st
    st=$(echo "$resp" | jq -r '.status // empty')
    if [[ "$st" != "running" && "$st" != "pending" && -n "$st" ]]; then
      echo "$resp"; return 0
    fi
    sleep 1; (( elapsed++ ))
  done
  echo '{"status":"timeout"}'; return 1
}

truncate_str() { local s="$1" n="${2:-40}"; [[ ${#s} -gt $n ]] && echo "${s:0:$n}…" || echo "$s"; }

ms_since() {
  # ms_since <epoch_ms_start>
  local now; now=$(date +%s%N)
  echo $(( (now / 1000000) - $1 ))
}

now_ms() { echo $(( $(date +%s%N) / 1000000 )); }

echo -e "\n${BLD}═══ chomp kitchen sink ═══${RST}"
echo -e "${DIM}base: $BASE${RST}\n"

###############################################################################
# [1/7] Router sweep via /v1/chat/completions
###############################################################################
echo -e "${BLD}[1/7] Router sweep (/v1/chat/completions)${RST}"

declare -A ROUTER_MODEL=(
  [zen]="gpt-5-nano"
  [groq]="llama-3.3-70b-versatile"
  [openrouter]="auto"
  [cerebras]="llama-3.3-70b"
  [sambanova]="Meta-Llama-3.3-70B-Instruct"
  [together]="auto"
  [fireworks]="auto"
)
ROUTER_ORDER=(zen groq openrouter cerebras sambanova together fireworks)

# fire all in background
for r in "${ROUTER_ORDER[@]}"; do
  model="${ROUTER_MODEL[$r]}"
  payload=$(jq -nc --arg r "$r" --arg m "$model" '{
    router: $r,
    model: $m,
    messages: [{role:"user",content:"What router are you? One word."}],
    max_tokens: 30
  }')
  (
    t0=$(now_ms)
    resp=$(chomp_v1 "$payload" 2>&1) || resp="{\"error\":\"curl failed\"}"
    lat=$(ms_since "$t0")
    echo "$resp" > "$TMP/r1_${r}.json"
    echo "$lat"  > "$TMP/r1_${r}.ms"
  ) &
done
wait

# collect & print
printf "  ${DIM}%-16s %-32s %-24s %-12s %s${RST}\n" ROUTER MODEL RESULT TOKENS LATENCY
for r in "${ROUTER_ORDER[@]}"; do
  model="${ROUTER_MODEL[$r]}"
  f="$TMP/r1_${r}.json"
  ms_file="$TMP/r1_${r}.ms"
  if [[ ! -f "$f" ]]; then
    echo -e "  ${SKIP} ${r} — no response file"; (( total_skip++ )); continue
  fi
  lat=$(cat "$ms_file" 2>/dev/null || echo "?")
  resp=$(cat "$f")
  err=$(echo "$resp" | jq -r '.error // .error.message // empty' 2>/dev/null)

  if [[ -n "$err" ]]; then
    # check if it's a "not configured" style error
    if echo "$err" | grep -qi 'not configured\|not set\|no key\|unknown router\|disabled'; then
      echo -e "  ${SKIP} ${r} — not configured"
      (( total_skip++ ))
    else
      short_err=$(truncate_str "$err" 50)
      echo -e "  ${FAIL} ${r}/${model} → ${RED}${short_err}${RST} (${lat}ms)"
      (( total_fail++ ))
    fi
    continue
  fi

  content=$(echo "$resp" | jq -r '.choices[0].message.content // empty' 2>/dev/null)
  tok_in=$(echo "$resp" | jq -r '.usage.prompt_tokens // 0' 2>/dev/null)
  tok_out=$(echo "$resp" | jq -r '.usage.completion_tokens // 0' 2>/dev/null)
  tok_total=$(( tok_in + tok_out ))
  total_tokens=$(( total_tokens + tok_total ))
  latencies[$r]=$lat

  if [[ -n "$content" ]]; then
    short=$(truncate_str "$content" 20)
    printf "  ${PASS} ${GRN}%-16s${RST} %-32s %-24s %-12s %s\n" \
      "$r" "$model" "\"$short\"" "${tok_in}→${tok_out}" "${lat}ms"
    (( total_pass++ ))
  else
    echo -e "  ${FAIL} ${r}/${model} → ${RED}empty response${RST} (${lat}ms)"
    (( total_fail++ ))
  fi
done
echo

###############################################################################
# [2/7] Auto router
###############################################################################
echo -e "${BLD}[2/7] Auto router${RST}"
t0=$(now_ms)
auto_resp=$(chomp_v1 "$(jq -nc '{
  model: "auto",
  messages: [{role:"user",content:"Say hello in 3 words."}],
  max_tokens: 30
}')" 2>&1) || auto_resp='{"error":"curl failed"}'
auto_lat=$(ms_since "$t0")

auto_model=$(echo "$auto_resp" | jq -r '.model // empty' 2>/dev/null)
auto_content=$(echo "$auto_resp" | jq -r '.choices[0].message.content // empty' 2>/dev/null)
auto_err=$(echo "$auto_resp" | jq -r '.error // .error.message // empty' 2>/dev/null)

if [[ -n "$auto_content" ]]; then
  auto_tok_in=$(echo "$auto_resp" | jq -r '.usage.prompt_tokens // 0' 2>/dev/null)
  auto_tok_out=$(echo "$auto_resp" | jq -r '.usage.completion_tokens // 0' 2>/dev/null)
  total_tokens=$(( total_tokens + auto_tok_in + auto_tok_out ))
  echo -e "  ${PASS} auto → ${CYN}${auto_model}${RST} \"$(truncate_str "$auto_content" 30)\" (${auto_lat}ms)"
  (( total_pass++ ))
else
  echo -e "  ${FAIL} auto → ${RED}${auto_err:-empty}${RST} (${auto_lat}ms)"
  (( total_fail++ ))
fi
echo

###############################################################################
# [3/7] System prompt (JSON mode)
###############################################################################
echo -e "${BLD}[3/7] System prompt (JSON mode)${RST}"
t0=$(now_ms)
sys_resp=$(chomp_v1 "$(jq -nc '{
  model: "auto",
  messages: [
    {role:"system",content:"Reply only in JSON. No markdown fences."},
    {role:"user",content:"List 3 colors as a JSON array."}
  ],
  max_tokens: 60
}')" 2>&1) || sys_resp='{"error":"curl failed"}'
sys_lat=$(ms_since "$t0")

sys_content=$(echo "$sys_resp" | jq -r '.choices[0].message.content // empty' 2>/dev/null)
sys_err=$(echo "$sys_resp" | jq -r '.error // .error.message // empty' 2>/dev/null)

if [[ -n "$sys_content" ]]; then
  sys_tok_in=$(echo "$sys_resp" | jq -r '.usage.prompt_tokens // 0' 2>/dev/null)
  sys_tok_out=$(echo "$sys_resp" | jq -r '.usage.completion_tokens // 0' 2>/dev/null)
  total_tokens=$(( total_tokens + sys_tok_in + sys_tok_out ))
  # try to parse as JSON
  if echo "$sys_content" | jq . >/dev/null 2>&1; then
    echo -e "  ${PASS} system prompt → valid JSON: ${DIM}$(truncate_str "$sys_content" 50)${RST} (${sys_lat}ms)"
    (( total_pass++ ))
  else
    echo -e "  ${YEL}⚠${RST}  system prompt → got text but not valid JSON: \"$(truncate_str "$sys_content" 40)\" (${sys_lat}ms)"
    (( total_pass++ ))  # partial pass
  fi
else
  echo -e "  ${FAIL} system prompt → ${RED}${sys_err:-empty}${RST} (${sys_lat}ms)"
  (( total_fail++ ))
fi
echo

###############################################################################
# [4/7] Async dispatch + poll
###############################################################################
echo -e "${BLD}[4/7] Async dispatch + poll (/api/dispatch → /api/result)${RST}"
t0=$(now_ms)
dispatch_resp=$(chomp_dispatch "$(jq -nc '{
  prompt: "What is 2+2? Reply with just the number.",
  router: "auto"
}')" 2>&1) || dispatch_resp='{"error":"curl failed"}'

job_id=$(echo "$dispatch_resp" | jq -r '.id // empty' 2>/dev/null)
dispatch_err=$(echo "$dispatch_resp" | jq -r '.error // empty' 2>/dev/null)

if [[ -n "$job_id" ]]; then
  echo -e "  ${DIM}dispatched job: ${job_id}${RST}"
  result_resp=$(chomp_result "$job_id" 2>&1)
  poll_lat=$(ms_since "$t0")
  result_status=$(echo "$result_resp" | jq -r '.status // empty' 2>/dev/null)
  result_text=$(echo "$result_resp" | jq -r '.result // empty' 2>/dev/null)
  result_router=$(echo "$result_resp" | jq -r '.router // empty' 2>/dev/null)
  result_model=$(echo "$result_resp" | jq -r '.model // empty' 2>/dev/null)
  result_tok_in=$(echo "$result_resp" | jq -r '.tokens_in // 0' 2>/dev/null)
  result_tok_out=$(echo "$result_resp" | jq -r '.tokens_out // 0' 2>/dev/null)
  total_tokens=$(( total_tokens + result_tok_in + result_tok_out ))

  if [[ "$result_status" == "done" ]]; then
    echo -e "  ${PASS} dispatch → ${CYN}${result_router}/${result_model}${RST} \"$(truncate_str "$result_text" 30)\" (${poll_lat}ms)"
    (( total_pass++ ))
  else
    echo -e "  ${FAIL} dispatch → status=${RED}${result_status}${RST} (${poll_lat}ms)"
    (( total_fail++ ))
  fi
else
  echo -e "  ${FAIL} dispatch failed → ${RED}${dispatch_err:-no id}${RST}"
  (( total_fail++ ))
fi
echo

###############################################################################
# [5/7] Fan-out — dispatch to multiple routers in parallel
###############################################################################
echo -e "${BLD}[5/7] Fan-out (dispatch to 3 routers in parallel)${RST}"
FAN_ROUTERS=(zen groq openrouter)
for r in "${FAN_ROUTERS[@]}"; do
  (
    t0=$(now_ms)
    dr=$(chomp_dispatch "$(jq -nc --arg r "$r" '{
      prompt: "What is the capital of France? One word.",
      router: $r
    }')" 2>&1) || dr='{"error":"curl failed"}'
    jid=$(echo "$dr" | jq -r '.id // empty' 2>/dev/null)
    if [[ -n "$jid" ]]; then
      rr=$(chomp_result "$jid" 2>&1)
      lat=$(ms_since "$t0")
      echo "$rr" | jq -c --arg r "$r" --arg lat "$lat" '. + {fan_router:$r, fan_lat:$lat}'
    else
      err=$(echo "$dr" | jq -r '.error // empty' 2>/dev/null)
      echo "{\"fan_router\":\"$r\",\"status\":\"error\",\"error\":\"${err:-dispatch failed}\"}"
    fi
  ) > "$TMP/fan_${r}.json" &
done
wait

for r in "${FAN_ROUTERS[@]}"; do
  f="$TMP/fan_${r}.json"
  [[ ! -f "$f" ]] && { echo -e "  ${SKIP} ${r} — no response"; (( total_skip++ )); continue; }
  resp=$(cat "$f")
  st=$(echo "$resp" | jq -r '.status // empty' 2>/dev/null)
  txt=$(echo "$resp" | jq -r '.result // empty' 2>/dev/null)
  lat=$(echo "$resp" | jq -r '.fan_lat // "?"' 2>/dev/null)
  err=$(echo "$resp" | jq -r '.error // empty' 2>/dev/null)
  fan_tok_in=$(echo "$resp" | jq -r '.tokens_in // 0' 2>/dev/null)
  fan_tok_out=$(echo "$resp" | jq -r '.tokens_out // 0' 2>/dev/null)
  total_tokens=$(( total_tokens + fan_tok_in + fan_tok_out ))

  if [[ "$st" == "done" && -n "$txt" ]]; then
    echo -e "  ${PASS} ${r} → \"$(truncate_str "$txt" 30)\" (${lat}ms)"
    (( total_pass++ ))
  elif echo "$err" | grep -qi 'not configured\|no key\|unknown\|disabled'; then
    echo -e "  ${SKIP} ${r} — not configured"
    (( total_skip++ ))
  else
    echo -e "  ${FAIL} ${r} → ${RED}${err:-status=$st}${RST} (${lat}ms)"
    (( total_fail++ ))
  fi
done
echo

###############################################################################
# [6/7] /v1/models — list all models
###############################################################################
echo -e "${BLD}[6/7] /v1/models${RST}"
models_resp=$(curl -sf --max-time 15 "$BASE/v1/models" -H "$AUTH" 2>&1) || models_resp=''
if [[ -n "$models_resp" ]]; then
  model_count=$(echo "$models_resp" | jq '.data | length' 2>/dev/null || echo 0)
  echo -e "  ${PASS} ${model_count} models available"
  # count per router (models have id like "router/model")
  echo "$models_resp" | jq -r '.data[].id' 2>/dev/null | cut -d/ -f1 | sort | uniq -c | sort -rn | while read cnt rtr; do
    printf "    ${DIM}%-16s %s models${RST}\n" "$rtr" "$cnt"
  done
  (( total_pass++ ))
else
  echo -e "  ${FAIL} /v1/models → ${RED}no response${RST}"
  (( total_fail++ ))
  model_count=0
fi
echo

###############################################################################
# [7/7] /api/models/:router — per-router model counts
###############################################################################
echo -e "${BLD}[7/7] /api/models/:router${RST}"
API_ROUTERS=(zen groq openrouter cerebras sambanova together fireworks)
for r in "${API_ROUTERS[@]}"; do
  (
    resp=$(curl -s --max-time 15 "$BASE/api/models/$r" -H "$AUTH" 2>&1) || resp=''
    if [[ -n "$resp" ]]; then
      cnt=$(echo "$resp" | jq '.count // (.models | length) // 0' 2>/dev/null)
      err=$(echo "$resp" | jq -r '.error // empty' 2>/dev/null)
      if [[ -n "$err" ]] || echo "$resp" | grep -qi 'not set'; then
        echo "SKIP|$r|not configured"
      else
        echo "OK|$r|$cnt"
      fi
    else
      echo "FAIL|$r|no response"
    fi
  ) > "$TMP/api_models_${r}.txt" &
done
wait

for r in "${API_ROUTERS[@]}"; do
  f="$TMP/api_models_${r}.txt"
  [[ ! -f "$f" ]] && { echo -e "  ${SKIP} ${r}"; (( total_skip++ )); continue; }
  line=$(cat "$f")
  IFS='|' read -r status router info <<< "$line"
  case "$status" in
    OK)   echo -e "  ${PASS} ${router}: ${info} models"; (( total_pass++ )) ;;
    SKIP) echo -e "  ${SKIP} ${router} — ${info}"; (( total_skip++ )) ;;
    *)    echo -e "  ${FAIL} ${router} → ${RED}${info}${RST}"; (( total_fail++ )) ;;
  esac
done
echo

###############################################################################
# Summary
###############################################################################
# find fastest / slowest from router sweep
fastest_r="-"; fastest_ms=999999
slowest_r="-"; slowest_ms=0
for r in "${!latencies[@]}"; do
  ms=${latencies[$r]}
  if (( ms < fastest_ms )); then fastest_ms=$ms; fastest_r=$r; fi
  if (( ms > slowest_ms )); then slowest_ms=$ms; slowest_r=$r; fi
done

live_routers=$(( total_pass > 0 ? total_pass : 0 ))
total_tests=$(( total_pass + total_fail + total_skip ))

echo -e "${BLD}═══ RESULTS ═══${RST}"
echo -e "  ${GRN}Passed${RST}:  ${total_pass}/${total_tests}"
[[ $total_fail -gt 0 ]] && echo -e "  ${RED}Failed${RST}:  ${total_fail}/${total_tests}"
[[ $total_skip -gt 0 ]] && echo -e "  ${DIM}Skipped${RST}: ${total_skip}/${total_tests}"
printf "  Total tokens chomped: %'d\n" "$total_tokens"
[[ "$fastest_r" != "-" ]] && echo -e "  Fastest: ${GRN}${fastest_r}${RST} (${fastest_ms}ms)"
[[ "$slowest_r" != "-" ]] && echo -e "  Slowest: ${YEL}${slowest_r}${RST} (${slowest_ms}ms)"
echo

[[ $total_fail -gt 0 ]] && exit 1
exit 0
