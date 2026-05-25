#!/usr/bin/env bash
# post-backfill-pipeline.sh — Run after backfill v2 completes.
#
# 1. Verify all 60 new reports captured
# 2. Move new reports from INNER (multiapp/results-multiapp) to OUTER (results-multiapp)
# 3. Run rule-grounded scoring (no LLM needed) immediately
# 4. Run LLM scoring with AI_KEY_A / AI_KEY_B (user must have exported)
# 5. Run K8sGPT-PT + Kagent-PT replay on new captures
# 6. Re-run final rescores
set -uo pipefail

REPO=/home/azureuser/preview-operator
INNER=$REPO/experiments/failure-provenance/multiapp/results-multiapp
OUTER=$REPO/experiments/failure-provenance/results-multiapp
LOGDIR=/tmp/post-backfill-$(date -u +%Y%m%d-%H%M%S)
mkdir -p "$LOGDIR"
echo "Logs: $LOGDIR"

echo "=== Step 1: verify backfill ==="
n_ok=0; n_miss=0
for sub in s2-listmonk s3-healthchecks s4-umami s5-petclinic; do
  for f in F1 F2 F3 F6 F7; do
    for r in r1 r2 r3; do
      if test -f "$INNER/$sub/$f/$r/report.json"; then
        n_ok=$((n_ok+1))
      else
        n_miss=$((n_miss+1))
        echo "  STILL MISSING: $sub $f $r"
      fi
    done
  done
done
echo "Backfilled OK: $n_ok / 60"
echo "Still missing: $n_miss / 60"
[ "$n_ok" -ge 50 ] || { echo "Too few captures; aborting."; exit 1; }

echo "=== Step 2: move new captures to outer path (backup outer first) ==="
backup_root=$OUTER/.backup-outer-pre-pipeline-$(date -u +%Y%m%d-%H%M%S)
for sub in s2-listmonk s3-healthchecks s4-umami s5-petclinic; do
  for f in F1 F2 F3 F6 F7; do
    for r in r1 r2 r3; do
      inner_dir="$INNER/$sub/$f/$r"
      outer_dir="$OUTER/$sub/$f/$r"
      if test -f "$inner_dir/report.json"; then
        # Backup existing outer
        if [ -d "$outer_dir" ]; then
          backup_dir="$backup_root/$sub/$f"
          mkdir -p "$backup_dir"
          mv "$outer_dir" "$backup_dir/r$r"
        fi
        mkdir -p "$OUTER/$sub/$f"
        cp -r "$inner_dir" "$OUTER/$sub/$f/r$r"
      fi
    done
  done
done
echo "Outer path updated. Backups in: $backup_root"

echo "=== Step 3: rule-grounded scoring on outer path ==="
cd $REPO
DIAG=$REPO/experiments/failure-provenance/multiapp/bin/fp-diagnose
n_done=0
for sub in s2-listmonk s3-healthchecks s4-umami s5-petclinic; do
  for f in F1 F2 F3 F6 F7; do
    for r in r1 r2 r3; do
      rpt="$OUTER/$sub/$f/$r/report.json"
      [ -s "$rpt" ] || continue
      for level in C1 C2 C3 C4 C5; do
        out="$OUTER/$sub/$f/$r/diag-${level}-rule-grounded.json"
        [ -s "$out" ] && continue
        $DIAG -engine rule -mode grounded -level "$level" -report "$rpt" -out "$out" \
              >> "$LOGDIR/rule-grounded.log" 2>&1 && n_done=$((n_done+1))
      done
    done
  done
done
echo "Rule-grounded diag files written: $n_done"

echo "=== Step 4: LLM scoring (requires AI_KEY_A and AI_KEY_B in env) ==="
if [ -z "${AI_KEY_A:-}" ] || [ -z "${AI_KEY_B:-}" ]; then
  echo "ABORT step 4-6: AI_KEY_A and/or AI_KEY_B not set."
  echo "  export them and re-run from step 4."
  echo "  Example:"
  echo "    export AI_KEY_A=<azure-openai-key>"
  echo "    export AI_KEY_B=<cohere-key>"
  exit 0
fi

cd $REPO/experiments/failure-provenance/multiapp
bash run-diagnose.sh 2>&1 | tee "$LOGDIR/run-diagnose.log"

# Also run freeform variants
echo "=== Step 4b: LLM-freeform scoring ==="
AI_API_URL_A=https://preview-openai-idp.openai.azure.com/openai/deployments/gpt-4o-mini
AI_API_URL_B=https://fp-foundry-133641.cognitiveservices.azure.com/openai/deployments/cohere-command-a
n_a=0; n_b=0
for sub in s2-listmonk s3-healthchecks s4-umami s5-petclinic; do
  for f in F1 F2 F3 F6 F7; do
    for r in r1 r2 r3; do
      rpt="$OUTER/$sub/$f/$r/report.json"
      [ -s "$rpt" ] || continue
      for level in C1 C2 C3 C4 C5; do
        out_a="$OUTER/$sub/$f/$r/diag-${level}-llm-freeform.json"
        out_b="$OUTER/$sub/$f/$r/diag-${level}-llm-freeform-llmb.json"
        if [ ! -s "$out_a" ]; then
          $DIAG -engine llm -mode freeform -model gpt-4o-mini \
                -ai-base-url $AI_API_URL_A -ai-api-key "$AI_KEY_A" \
                -level $level -report "$rpt" -out "$out_a" \
                >> "$LOGDIR/freeform-a.log" 2>&1 && n_a=$((n_a+1))
        fi
        if [ ! -s "$out_b" ]; then
          $DIAG -engine llm -mode freeform -model cohere-command-a \
                -ai-base-url $AI_API_URL_B -ai-api-key "$AI_KEY_B" \
                -level $level -report "$rpt" -out "$out_b" \
                >> "$LOGDIR/freeform-b.log" 2>&1 && n_b=$((n_b+1))
        fi
      done
    done
  done
done
echo "Freeform diag files written: A=$n_a, B=$n_b"

echo "=== Step 5: K8sGPT-PT + Kagent-PT replay on new captures ==="
cd $REPO
python3 experiments/failure-provenance/analysis/k8sgpt-replay.py --force 2>&1 | tee "$LOGDIR/k8sgpt-replay.log" | tail -5
python3 experiments/failure-provenance/analysis/kagent-replay.py --force 2>&1 | tee "$LOGDIR/kagent-replay.log" | tail -5

echo "=== Step 6: Final rescores ==="
python3 experiments/failure-provenance/analysis/35-unified-rescore.py 2>&1 | tail -10
python3 experiments/failure-provenance/analysis/36-unified-figures.py 2>&1 | tail -5
python3 experiments/failure-provenance/analysis/34-b2-post-teardown-rescore.py 2>&1 | tail -10
python3 experiments/failure-provenance/analysis/19-evidence-precision.py 2>&1 | tail -5
python3 experiments/failure-provenance/analysis/21-lmm.py 2>&1 | tail -5
python3 experiments/failure-provenance/analysis/21c-glmm-logit-s1s5.py 2>&1 | tail -5
python3 experiments/failure-provenance/analysis/22-tukey.py 2>&1 | tail -5
python3 experiments/failure-provenance/analysis/23-mcnemar.py 2>&1 | tail -5
python3 experiments/failure-provenance/analysis/24-cd-diagrams.py 2>&1 | tail -5
python3 experiments/failure-provenance/analysis/25-pareto.py 2>&1 | tail -5
python3 experiments/failure-provenance/analysis/30b-evidence-ladder-s1s5.py 2>&1 | tail -5
python3 experiments/failure-provenance/analysis/17-multiapp-rescore.py 2>&1 | tail -5

echo ""
echo "=== Pipeline DONE ==="
echo "Logs: $LOGDIR"
