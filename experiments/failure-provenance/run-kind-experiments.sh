#!/usr/bin/env bash
#
# run-kind-experiments.sh — drive the failure-provenance fault-injection matrix.
#
# This script orchestrates the experiment described in
# docs/research/failure-provenance/methodology.md and experiments.md. It is
# deliberately SAFE:
#   * bash strict mode;
#   * checks its dependencies before doing anything;
#   * defaults to --dry-run (prints the full plan, contacts nothing);
#   * only ever deletes Previews it created (guarded by an experiment label);
#   * prints every action it takes.
#
# Design — one cluster run, five diagnostic configurations:
#
#   A *cluster run* is one (scenario, repetition): a synthetic faulted preview
#   is deployed, the operator captures one full (C5) FailureReport, the report
#   is exported, and the preview is torn down. The operator must run with
#   EVIDENCE_COLLECTION enabled and EVIDENCE_LEVEL=C5.
#
#   *Scoring* is then offline: for each configuration C1..C5 and each diagnostic
#   engine/mode, fp-diagnose down-samples that single capture to the level and
#   fp-score appends one results-CSV row. The C1..C5 comparison therefore costs
#   one cluster run, not five.
#
#   cluster runs  = scenarios x repetitions
#   CSV rows      = scenarios x repetitions x configs x engine/mode combinations
#
# Usage:
#   ./run-kind-experiments.sh [--dry-run|--execute] [options]
#
# Options:
#   --dry-run                 Print the full plan only (default).
#   --execute                 Run the experiment for real.
#   --scenario   <ID|all>     Scenario(s): F1..F10 or "all" (default: all).
#   --config     <C|all>      Configuration(s) to score: C1..C5 or "all" (default: all).
#   --repetitions <N>         Cluster repetitions per scenario (default: 10).
#   --cluster-type <kind|aks> Cluster label recorded in results (default: kind).
#   --engines    <list>       Diagnostic engines: rule, llm or rule,llm (default: rule).
#   --output-dir <path>       Where results and artifacts are written (default: ./results).
#   --idp-repo   <url>        idp-preview repository (default: the public repo).
#   --registry   <name>       ACR registry name for image builds (aks; default: testagentdevops).
#   --baseline-image <ref>    Known-good idp-preview image for non-code faults.
#   --pr-base    <N>          Base PR number for synthetic previews (default: 9000).
#   --report-timeout <sec>    Seconds to wait for the FailureReport (default: 1200).
#   --keep                    Do not tear the preview down (debugging).
#   -h | --help               Show this help.

set -euo pipefail

# --- constants ---------------------------------------------------------------

readonly SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
readonly SCENARIOS_FILE="${SCRIPT_DIR}/scenarios.yaml"
readonly RESULTS_TEMPLATE="${SCRIPT_DIR}/results-template.csv"
readonly INJECTOR="${SCRIPT_DIR}/injectors/inject-fault.sh"
readonly COLLECTOR="${SCRIPT_DIR}/collect-results.sh"
# Previews created by this script carry this label; cleanup only ever touches it.
readonly EXPERIMENT_LABEL="failure-provenance.experiment/owned=true"

readonly ALL_SCENARIOS=(F1 F2 F3 F4 F5 F6 F7 F8 F9 F10)
readonly ALL_CONFIGS=(C1 C2 C3 C4 C5)
# Scenarios whose fault is in application code, so the image must be rebuilt.
# The rest (F3 manifest, F7 cluster patch, F9 manifest) use the baseline image.
readonly CODE_FAULTS=" F1 F2 F4 F5 F6 F8 F10 "

# --- defaults ----------------------------------------------------------------

MODE="dry-run"
SELECT_SCENARIO="all"
SELECT_CONFIG="all"
REPETITIONS="10"
CLUSTER_TYPE="kind"
ENGINES="rule"
OUTPUT_DIR="${SCRIPT_DIR}/results"
IDP_REPO="https://github.com/ihsenalaya/idp-preview"
REGISTRY="testagentdevops"
BASELINE_IMAGE="ghcr.io/ihsenalaya/idp-preview:latest"
PR_BASE="9000"
REPORT_TIMEOUT="1200"
KEEP_PREVIEW="false"

# --- logging -----------------------------------------------------------------

log()  { printf '[%s] %s\n' "$(date +%H:%M:%S)" "$*"; }
step() { printf '\n=== %s ===\n' "$*"; }
plan() { printf '    $ %s\n' "$*"; }
die()  { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

# --- argument parsing --------------------------------------------------------

usage() { sed -n '2,50p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; }

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run)        MODE="dry-run" ;;
    --execute)        MODE="execute" ;;
    --scenario)       SELECT_SCENARIO="${2:-}"; shift ;;
    --config)         SELECT_CONFIG="${2:-}"; shift ;;
    --repetitions)    REPETITIONS="${2:-}"; shift ;;
    --cluster-type)   CLUSTER_TYPE="${2:-}"; shift ;;
    --engines)        ENGINES="${2:-}"; shift ;;
    --output-dir)     OUTPUT_DIR="${2:-}"; shift ;;
    --idp-repo)       IDP_REPO="${2:-}"; shift ;;
    --registry)       REGISTRY="${2:-}"; shift ;;
    --baseline-image) BASELINE_IMAGE="${2:-}"; shift ;;
    --pr-base)        PR_BASE="${2:-}"; shift ;;
    --report-timeout) REPORT_TIMEOUT="${2:-}"; shift ;;
    --keep)           KEEP_PREVIEW="true" ;;
    -h|--help)        usage; exit 0 ;;
    *)                die "unknown argument: $1 (use --help)" ;;
  esac
  shift
done

[[ "${REPETITIONS}"    =~ ^[0-9]+$ ]] || die "--repetitions must be a positive integer"
[[ "${PR_BASE}"        =~ ^[0-9]+$ ]] || die "--pr-base must be a positive integer"
[[ "${REPORT_TIMEOUT}" =~ ^[0-9]+$ ]] || die "--report-timeout must be a positive integer"

# Binaries built at startup (execute mode) into this directory.
BIN_DIR="${OUTPUT_DIR}/.bin"
FP_DIAGNOSE="${BIN_DIR}/fp-diagnose"
FP_SCORE="${BIN_DIR}/fp-score"

# --- dependency checks -------------------------------------------------------

check_deps() {
  step "Checking dependencies"
  local missing=()
  for tool in kubectl git go python3; do
    if command -v "${tool}" >/dev/null 2>&1; then
      log "found: ${tool}"
    else
      missing+=("${tool}")
    fi
  done
  # Image-build tooling depends on the cluster type.
  case "${CLUSTER_TYPE}" in
    kind) for t in docker kind; do command -v "${t}" >/dev/null 2>&1 || missing+=("${t}"); done ;;
    aks)  command -v az >/dev/null 2>&1 || missing+=("az") ;;
  esac
  # In dry-run nothing is executed, so a missing tool is only a warning — the
  # plan is still printed. In execute mode every tool is required.
  if [[ ${#missing[@]} -gt 0 ]]; then
    if [[ "${MODE}" == "execute" ]]; then
      die "missing required tools: ${missing[*]}"
    fi
    log "WARNING (dry-run): tools not installed — required for --execute: ${missing[*]}"
  fi
  [[ -f "${SCENARIOS_FILE}" ]]   || die "scenarios file not found: ${SCENARIOS_FILE}"
  [[ -f "${RESULTS_TEMPLATE}" ]] || die "results template not found: ${RESULTS_TEMPLATE}"
  [[ -x "${INJECTOR}" ]]         || die "injector not found/executable: ${INJECTOR}"
  [[ -f "${COLLECTOR}" ]]        || die "collector not found: ${COLLECTOR}"
}

# --- selection helpers -------------------------------------------------------

selected_scenarios() {
  if [[ "${SELECT_SCENARIO}" == "all" ]]; then printf '%s\n' "${ALL_SCENARIOS[@]}"; return; fi
  for s in "${ALL_SCENARIOS[@]}"; do [[ "${s}" == "${SELECT_SCENARIO}" ]] && { echo "${s}"; return; }; done
  die "unknown scenario: ${SELECT_SCENARIO} (expected F1..F10 or all)"
}

selected_configs() {
  if [[ "${SELECT_CONFIG}" == "all" ]]; then printf '%s\n' "${ALL_CONFIGS[@]}"; return; fi
  for c in "${ALL_CONFIGS[@]}"; do [[ "${c}" == "${SELECT_CONFIG}" ]] && { echo "${c}"; return; }; done
  die "unknown configuration: ${SELECT_CONFIG} (expected C1..C5 or all)"
}

# engine_mode_combos — print "engine mode" pairs for the selected engines.
# The rule engine is deterministic and grounded by construction (one combo);
# the LLM engine is run grounded and free-form — the RQ4 ablation.
engine_mode_combos() {
  IFS=',' read -ra engs <<< "${ENGINES}"
  for e in "${engs[@]}"; do
    case "${e}" in
      rule) echo "rule grounded" ;;
      llm)  echo "llm grounded"; echo "llm freeform" ;;
      *)    die "unknown engine: ${e} (expected rule and/or llm)" ;;
    esac
  done
}

is_code_fault() { [[ "${CODE_FAULTS}" == *" $1 "* ]]; }

# --- setup (execute mode) ----------------------------------------------------

ensure_cluster() {
  kubectl cluster-info >/dev/null 2>&1 \
    || die "no reachable Kubernetes cluster (current context: $(kubectl config current-context 2>/dev/null || echo none))"
  log "cluster reachable: $(kubectl config current-context)"
  kubectl get crd failurereports.platform.company.io >/dev/null 2>&1 \
    || die "FailureReport CRD not installed — deploy this fork's operator first"
}

build_binaries() {
  mkdir -p "${BIN_DIR}"
  log "building fp-diagnose and fp-score"
  ( cd "${REPO_ROOT}" && go build -o "${FP_DIAGNOSE}" ./cmd/fp-diagnose )
  ( cd "${REPO_ROOT}" && go build -o "${FP_SCORE}" ./cmd/fp-score )
}

# --- one cluster run ---------------------------------------------------------

# cluster_run SCENARIO REP RUN_DIR — deploy a faulted preview, capture the
# FailureReport, tear down. Writes "${RUN_DIR}/report.json" and, on success,
# echoes the preview name on stdout. Returns non-zero if no report was captured.
cluster_run() {
  local scenario="$1" rep="$2" run_dir="$3"
  local pr_number=$(( PR_BASE + 10#${scenario#F} * 100 + rep ))
  local preview="pr-${pr_number}"
  local checkout="${run_dir}/idp-preview"
  local image="${BASELINE_IMAGE}"

  if [[ "${MODE}" == "dry-run" ]]; then
    echo "  plan for ${scenario} rep ${rep}  ->  preview ${preview}"
    plan "git clone ${IDP_REPO} ${checkout}"
    if [[ "${scenario}" != "F7" ]]; then
      plan "${INJECTOR} --scenario ${scenario} --repo-dir ${checkout} --commit"
    fi
    if is_code_fault "${scenario}"; then
      if [[ "${CLUSTER_TYPE}" == "aks" ]]; then
        plan "az acr build -r ${REGISTRY} -t idp-preview:fp-${scenario}-r${rep} ${checkout}"
      else
        plan "docker build -t idp-preview:fp-${scenario}-r${rep} ${checkout} && kind load docker-image ..."
      fi
    else
      plan "# non-code fault: reuse baseline image ${BASELINE_IMAGE}"
    fi
    plan "(cd ${checkout} && python3 scripts/generate_preview_manifest.py --pr-number ${pr_number} ...) | kubectl apply -f -"
    plan "kubectl label preview ${preview} ${EXPERIMENT_LABEL}"
    [[ "${scenario}" == "F7" ]] && \
      plan "${INJECTOR} --scenario F7 --namespace <preview-ns> --service backend --apply"
    plan "wait <=${REPORT_TIMEOUT}s for failurereport ${preview}-failure"
    plan "kubectl get failurereport ${preview}-failure -o json > ${run_dir}/report.json"
    plan "${COLLECTOR} --preview ${preview} --output-dir ${run_dir}/artifacts"
    [[ "${KEEP_PREVIEW}" == "true" ]] || plan "kubectl delete preview ${preview}  # operator tears the namespace down"
    plan "re-read failurereport ${preview}-failure  # RQ1 survival check"
    return 0
  fi

  # --- execute ---------------------------------------------------------------
  log "${scenario} rep ${rep}: preparing ${preview}"
  git clone --quiet --depth 1 "${IDP_REPO}" "${checkout}"
  local base_sha head_sha
  base_sha="$(git -C "${checkout}" rev-parse HEAD)"

  if [[ "${scenario}" != "F7" ]]; then
    "${INJECTOR}" --scenario "${scenario}" --repo-dir "${checkout}" --commit >/dev/null
    head_sha="$(git -C "${checkout}" rev-parse HEAD)"
  else
    head_sha="${base_sha}"
  fi

  if is_code_fault "${scenario}"; then
    image="idp-preview:fp-${scenario}-r${rep}"
    log "building image ${image}"
    if [[ "${CLUSTER_TYPE}" == "aks" ]]; then
      az acr build -r "${REGISTRY}" -t "${image}" "${checkout}" >/dev/null
      image="${REGISTRY}.azurecr.io/${image}"
    else
      docker build -q -t "${image}" "${checkout}" >/dev/null
      kind load docker-image "${image}" >/dev/null
    fi
  fi

  log "applying Preview ${preview}"
  # The generator derives changeContext from `git diff base...head`, which it
  # runs in the current directory — so it must run inside the checkout, not the
  # experiment repo. Without the cd the diff resolves nothing and every Preview
  # is created with an empty changeContext (no ChangedFile/GitDiff evidence).
  ( cd "${checkout}" && python3 scripts/generate_preview_manifest.py \
    --pr-number "${pr_number}" --branch "fp/${scenario,,}" --image "${image}" \
    --base-sha "${base_sha}" --head-sha "${head_sha}" \
    --repo ihsenalaya/idp-preview --repo-owner ihsenalaya --repo-name idp-preview \
    --deployment-id "${pr_number}" ) \
    | kubectl apply -f - >/dev/null
  kubectl label preview "${preview}" "${EXPERIMENT_LABEL}" --overwrite >/dev/null

  # F7 is a cluster-side fault: patch the Service once the namespace exists.
  if [[ "${scenario}" == "F7" ]]; then
    local ns
    ns="$(wait_for_namespace "${preview}")" || { log "F7: preview namespace never appeared"; return 1; }
    "${INJECTOR}" --scenario F7 --namespace "${ns}" --service backend --apply || true
  fi

  if ! wait_for_report "${preview}-failure"; then
    log "${scenario} rep ${rep}: no FailureReport within ${REPORT_TIMEOUT}s"
    [[ "${KEEP_PREVIEW}" == "true" ]] || kubectl delete preview "${preview}" --ignore-not-found >/dev/null 2>&1
    return 1
  fi
  kubectl get failurereport "${preview}-failure" -o json > "${run_dir}/report.json"
  bash "${COLLECTOR}" --preview "${preview}" --output-dir "${run_dir}/artifacts" >/dev/null 2>&1 || true

  # Teardown + RQ1 survival check: the FailureReport is cluster-scoped, so it
  # must still be readable after the preview namespace is gone.
  local survived="" deleted=""
  if [[ "${KEEP_PREVIEW}" != "true" ]]; then
    kubectl delete preview "${preview}" --ignore-not-found >/dev/null 2>&1 || true
    deleted="true"
    if kubectl get failurereport "${preview}-failure" >/dev/null 2>&1; then
      survived="true"
    else
      survived="false"
    fi
  fi
  printf '%s\n%s\n' "${deleted}" "${survived}" > "${run_dir}/teardown.txt"
  echo "${preview}"
}

# wait_for_namespace PREVIEW — echo the preview namespace once it exists.
wait_for_namespace() {
  local preview="$1" deadline=$(( SECONDS + 300 )) ns=""
  while (( SECONDS < deadline )); do
    ns="$(kubectl get preview "${preview}" -o jsonpath='{.status.namespaceName}' 2>/dev/null || true)"
    if [[ -n "${ns}" ]] && kubectl get ns "${ns}" >/dev/null 2>&1; then
      echo "${ns}"; return 0
    fi
    sleep 5
  done
  return 1
}

# wait_for_report NAME — poll until the FailureReport exists or the timeout hits.
wait_for_report() {
  local name="$1" deadline=$(( SECONDS + REPORT_TIMEOUT ))
  while (( SECONDS < deadline )); do
    if kubectl get failurereport "${name}" >/dev/null 2>&1; then
      return 0
    fi
    sleep 15
  done
  return 1
}

# --- offline scoring ---------------------------------------------------------

# score_run SCENARIO REP RUN_DIR — diagnose the captured report at every selected
# configuration and engine/mode, and append one results-CSV row per combination.
score_run() {
  local scenario="$1" rep="$2" run_dir="$3"
  local report="${run_dir}/report.json"
  local deleted="" survived=""
  if [[ -f "${run_dir}/teardown.txt" ]]; then
    deleted="$(sed -n '1p' "${run_dir}/teardown.txt")"
    survived="$(sed -n '2p' "${run_dir}/teardown.txt")"
  fi

  while read -r config; do
    while read -r engine mode; do
      local run_id="${scenario}-r${rep}-${config}-${engine}-${mode}"
      local diag="${run_dir}/diag-${config}-${engine}-${mode}.json"

      if [[ "${MODE}" == "dry-run" ]]; then
        plan "fp-diagnose --report report.json --level ${config} --engine ${engine} --mode ${mode} --out ${diag##*/}"
        plan "fp-score --report report.json --result ${diag##*/} --scenario ${scenario} --run-id ${run_id} >> results.csv"
        continue
      fi

      "${FP_DIAGNOSE}" --report "${report}" --level "${config}" \
        --engine "${engine}" --mode "${mode}" --out "${diag}" \
        || { log "diagnose failed: ${run_id}"; continue; }
      "${FP_SCORE}" --report "${report}" --result "${diag}" \
        --scenarios "${SCENARIOS_FILE}" --scenario "${scenario}" \
        --run-id "${run_id}" --cluster-type "${CLUSTER_TYPE}" \
        --namespace-deleted "${deleted}" --evidence-survived "${survived}" \
        >> "${OUTPUT_DIR}/results.csv" \
        || log "score failed: ${run_id}"
      log "scored ${run_id}"
    done < <(engine_mode_combos)
  done < <(selected_configs)
}

# --- main --------------------------------------------------------------------

main() {
  step "Failure-provenance experiment runner (mode: ${MODE})"
  check_deps

  mapfile -t scenarios < <(selected_scenarios)
  mapfile -t configs   < <(selected_configs)
  mapfile -t combos    < <(engine_mode_combos)

  local cluster_runs=$(( ${#scenarios[@]} * REPETITIONS ))
  local csv_rows=$(( cluster_runs * ${#configs[@]} * ${#combos[@]} ))
  log "scenarios     : ${scenarios[*]}"
  log "configs       : ${configs[*]}"
  log "engine/mode   : $(engine_mode_combos | paste -sd'|' -)"
  log "repetitions   : ${REPETITIONS}"
  log "cluster runs  : ${cluster_runs}  (scenarios x repetitions)"
  log "CSV rows      : ${csv_rows}  (cluster runs x configs x engine-modes)"

  if [[ "${ENGINES}" == *llm* && -z "${AI_API_KEY:-}${OPENAI_API_KEY:-}" ]]; then
    die "engine 'llm' selected but no AI_API_KEY / OPENAI_API_KEY in the environment"
  fi

  if [[ "${MODE}" == "execute" ]]; then
    ensure_cluster
    mkdir -p "${OUTPUT_DIR}"
    build_binaries
    [[ -f "${OUTPUT_DIR}/results.csv" ]] || cp "${RESULTS_TEMPLATE}" "${OUTPUT_DIR}/results.csv"
    log "results CSV: ${OUTPUT_DIR}/results.csv"
  else
    log "dry-run: no cluster contacted, nothing built, created or deleted"
  fi

  local ok=0 failed=0
  for scenario in "${scenarios[@]}"; do
    for (( rep=1; rep<=REPETITIONS; rep++ )); do
      step "Run: ${scenario} repetition ${rep}/${REPETITIONS}"
      local run_dir="${OUTPUT_DIR}/${scenario}/r${rep}"
      [[ "${MODE}" == "execute" ]] && mkdir -p "${run_dir}"

      if [[ "${MODE}" == "dry-run" ]]; then
        cluster_run "${scenario}" "${rep}" "${run_dir}" || true
        score_run   "${scenario}" "${rep}" "${run_dir}" || true
        continue
      fi

      if cluster_run "${scenario}" "${rep}" "${run_dir}" >/dev/null; then
        score_run "${scenario}" "${rep}" "${run_dir}"
        ok=$(( ok + 1 ))
      else
        log "${scenario} rep ${rep}: cluster run produced no FailureReport — skipped"
        failed=$(( failed + 1 ))
      fi
    done
  done

  step "Done (${MODE})"
  if [[ "${MODE}" == "execute" ]]; then
    log "cluster runs: ${ok} captured, ${failed} produced no report"
    log "results: ${OUTPUT_DIR}/results.csv"
    log "score with the rubric: experiments/failure-provenance/scoring-rubric.md"
  else
    log "re-run with --execute against a cluster running this fork's operator (EVIDENCE_LEVEL=C5)."
  fi
}

main "$@"
