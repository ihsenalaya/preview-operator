#!/usr/bin/env bash
# run-diagnose.sh — Batch fp-diagnose across the 200 multi-app captures.
#
# Splits LLM-A (gpt-4o-mini, high quota) and LLM-B (cohere-command-a,
# lower quota) into two pools running concurrently. Each worker retries
# 429s up to 5 times with exponential backoff.
set -uo pipefail

REPO=/home/azureuser/preview-operator
RESULTS=$REPO/experiments/failure-provenance/results-multiapp
DIAGNOSE=$REPO/experiments/failure-provenance/multiapp/bin/fp-diagnose

AI_API_URL_A=https://preview-openai-idp.openai.azure.com/openai/deployments/gpt-4o-mini
AI_MODEL_A=gpt-4o-mini
AI_API_URL_B=https://fp-foundry-133641.cognitiveservices.azure.com/openai/deployments/cohere-command-a
AI_MODEL_B=cohere-command-a

JOBS_A=/tmp/multiapp-diagnose-jobs-A.txt
JOBS_B=/tmp/multiapp-diagnose-jobs-B.txt
ERR_LOG=/tmp/multiapp-diagnose-errors.log
> "$JOBS_A"; > "$JOBS_B"; > "$ERR_LOG"

for sdir in "$RESULTS"/s2-listmonk "$RESULTS"/s3-healthchecks "$RESULTS"/s4-umami "$RESULTS"/s5-petclinic; do
  [ -d "$sdir" ] || continue
  for fdir in "$sdir"/F*; do
    [ -d "$fdir" ] || continue
    for rdir in "$fdir"/r*; do
      report="$rdir/report.json"
      [ -s "$report" ] || continue
      for level in C1 C2 C3 C4 C5; do
        out_a="$rdir/diag-${level}-llm-grounded.json"
        [ -s "$out_a" ] || echo "$report|$level|$out_a" >> "$JOBS_A"
        out_b="$rdir/diag-${level}-llm-grounded-llmb.json"
        [ -s "$out_b" ] || echo "$report|$level|$out_b" >> "$JOBS_B"
      done
    done
  done
done

echo "LLM-A jobs: $(wc -l < $JOBS_A)"
echo "LLM-B jobs: $(wc -l < $JOBS_B)"

# Worker function with retry on 429
run_with_retry() {
  local engine="$1" spec="$2"
  IFS='|' read -r report level out <<< "$spec"
  local url key model
  if [ "$engine" = "A" ]; then
    url=$AI_API_URL_A; key=$AI_KEY_A; model=$AI_MODEL_A
  else
    url=$AI_API_URL_B; key=$AI_KEY_B; model=$AI_MODEL_B
  fi
  local attempt=0 backoff=3
  while [ $attempt -lt 5 ]; do
    local tmp_err=$(mktemp)
    if "$DIAGNOSE" -engine llm -mode grounded -model "$model" \
                   -ai-base-url "$url" -ai-api-key "$key" \
                   -level "$level" -report "$report" -out "$out" 2>"$tmp_err"; then
      rm -f "$tmp_err"
      return 0
    fi
    # Check if it's a 429 (rate limit)
    if grep -q "429\|RateLimitReached" "$tmp_err"; then
      cat "$tmp_err" >> "$ERR_LOG"
      sleep $backoff
      backoff=$((backoff * 2))
      attempt=$((attempt + 1))
    else
      cat "$tmp_err" >> "$ERR_LOG"
      echo "FAIL $engine $report $level (non-429)" >> "$ERR_LOG"
      rm -f "$tmp_err"
      return 1
    fi
    rm -f "$tmp_err"
  done
  echo "FAIL $engine $report $level (5 retries exhausted)" >> "$ERR_LOG"
  return 1
}

run_a() { run_with_retry A "$1"; }
run_b() { run_with_retry B "$1"; }

export -f run_a run_b run_with_retry
export AI_KEY_A AI_KEY_B AI_API_URL_A AI_API_URL_B AI_MODEL_A AI_MODEL_B DIAGNOSE ERR_LOG

# LLM-A: high parallelism
< "$JOBS_A" xargs -P 10 -I {} bash -c 'run_a "$@"' _ {} &
A_PID=$!

# LLM-B: low parallelism
< "$JOBS_B" xargs -P 3 -I {} bash -c 'run_b "$@"' _ {} &
B_PID=$!

echo "Pool A PID=$A_PID, Pool B PID=$B_PID"
wait $A_PID; echo "LLM-A pool done"
wait $B_PID; echo "LLM-B pool done"
echo "Total errors: $(grep -c FAIL "$ERR_LOG" 2>/dev/null || echo 0)"
