#!/usr/bin/env bash
#
# run-kind-experiments.sh — drive the failure-provenance fault-injection matrix.
#
# This script orchestrates the experiment runs described in
# docs/research/failure-provenance/methodology.md and experiments.md. It is
# deliberately SAFE:
#   * bash strict mode;
#   * checks its dependencies before doing anything;
#   * defaults to --dry-run (prints the plan, changes nothing);
#   * never deletes a namespace it did not create (guarded by a label);
#   * prints every action it takes.
#
# Full end-to-end execution also needs the Phase-5 integration (fault injection
# into the idp-preview demo app, and wiring of EnsureFailureReport onto the
# operator failure path). Those steps are marked TODO_PHASE5 below; until they are
# wired, --execute runs the safe parts and stops cleanly at the unwired step.
#
# Usage:
#   ./run-kind-experiments.sh [--dry-run|--execute] [options]
#
# Options:
#   --dry-run                 Print the plan only (default).
#   --execute                 Run the safe steps; stop at unwired Phase-5 steps.
#   --scenario  <ID|all>      Scenario(s) to run: F1..F10 or "all" (default: all).
#   --config    <C|all>       Configuration(s): C1..C5 or "all" (default: all).
#   --repetitions <N>         Repetitions per cell (default: from scenarios.yaml).
#   --cluster-type <kind|aks> Cluster label recorded in results (default: kind).
#   --output-dir <path>       Where results are written (default: ./results).
#   -h | --help               Show this help.

set -euo pipefail

# --- constants ---------------------------------------------------------------

readonly SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly SCENARIOS_FILE="${SCRIPT_DIR}/scenarios.yaml"
readonly RESULTS_TEMPLATE="${SCRIPT_DIR}/results-template.csv"
# Experiment-created namespaces carry this label; cleanup only ever touches these.
readonly EXPERIMENT_LABEL="failure-provenance.experiment/owned=true"

readonly ALL_SCENARIOS=(F1 F2 F3 F4 F5 F6 F7 F8 F9 F10)
readonly ALL_CONFIGS=(C1 C2 C3 C4 C5)

# --- defaults ----------------------------------------------------------------

MODE="dry-run"
SELECT_SCENARIO="all"
SELECT_CONFIG="all"
REPETITIONS="10"
CLUSTER_TYPE="kind"
OUTPUT_DIR="${SCRIPT_DIR}/results"

# --- logging -----------------------------------------------------------------

log()  { printf '[%s] %s\n' "$(date +%H:%M:%S)" "$*"; }
step() { printf '\n=== %s ===\n' "$*"; }
die()  { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

# --- argument parsing --------------------------------------------------------

usage() { sed -n '2,40p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; }

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run)      MODE="dry-run" ;;
    --execute)      MODE="execute" ;;
    --scenario)     SELECT_SCENARIO="${2:-}"; shift ;;
    --config)       SELECT_CONFIG="${2:-}"; shift ;;
    --repetitions)  REPETITIONS="${2:-}"; shift ;;
    --cluster-type) CLUSTER_TYPE="${2:-}"; shift ;;
    --output-dir)   OUTPUT_DIR="${2:-}"; shift ;;
    -h|--help)      usage; exit 0 ;;
    *)              die "unknown argument: $1 (use --help)" ;;
  esac
  shift
done

[[ "${REPETITIONS}" =~ ^[0-9]+$ ]] || die "--repetitions must be a positive integer"

# --- dependency checks -------------------------------------------------------

check_deps() {
  step "Checking dependencies"
  local missing=()
  for tool in kubectl kind docker; do
    if command -v "${tool}" >/dev/null 2>&1; then
      log "found: ${tool}"
    else
      missing+=("${tool}")
    fi
  done
  for opt in jq yq; do
    command -v "${opt}" >/dev/null 2>&1 && log "found (optional): ${opt}" \
      || log "missing (optional): ${opt} — recommended for result parsing"
  done
  [[ ${#missing[@]} -eq 0 ]] || die "missing required tools: ${missing[*]}"
  [[ -f "${SCENARIOS_FILE}" ]]   || die "scenarios file not found: ${SCENARIOS_FILE}"
  [[ -f "${RESULTS_TEMPLATE}" ]] || die "results template not found: ${RESULTS_TEMPLATE}"
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

# --- cluster check (execute mode only) ---------------------------------------

ensure_cluster() {
  step "Checking the ${CLUSTER_TYPE} cluster"
  if ! kubectl cluster-info >/dev/null 2>&1; then
    die "no reachable Kubernetes cluster. Create one first, e.g.:  kind create cluster"
  fi
  log "cluster reachable: $(kubectl config current-context)"
}

# --- per-run handling --------------------------------------------------------

# run_one SCENARIO CONFIG REP — perform (or print) one experiment run.
run_one() {
  local scenario="$1" config="$2" rep="$3"
  local run_id="${scenario}-${config}-r${rep}"
  log "run ${run_id} | cluster=${CLUSTER_TYPE} | output=${OUTPUT_DIR}/${run_id}"

  if [[ "${MODE}" == "dry-run" ]]; then
    return 0
  fi

  # TODO_PHASE5: the following steps require the Phase-5 integration.
  #   1. inject the scenario fault into a synthetic idp-preview PR;
  #   2. apply the Preview CR (namespace labelled "${EXPERIMENT_LABEL}");
  #   3. wait for the failure and for the operator to write the FailureReport;
  #   4. collect artifacts via collect-results.sh;
  #   5. let the operator tear the namespace down, then re-check evidence survival.
  log "TODO_PHASE5: fault injection + operator wiring not yet available — skipping ${run_id}"
}

# --- main --------------------------------------------------------------------

main() {
  step "Failure-provenance experiment runner (mode: ${MODE})"
  check_deps

  mapfile -t scenarios < <(selected_scenarios)
  mapfile -t configs   < <(selected_configs)

  local total=$(( ${#scenarios[@]} * ${#configs[@]} * REPETITIONS ))
  log "scenarios : ${scenarios[*]}"
  log "configs   : ${configs[*]}"
  log "reps/cell : ${REPETITIONS}"
  log "total runs: ${total}"

  if [[ "${MODE}" == "execute" ]]; then
    ensure_cluster
    mkdir -p "${OUTPUT_DIR}"
    [[ -f "${OUTPUT_DIR}/results.csv" ]] || cp "${RESULTS_TEMPLATE}" "${OUTPUT_DIR}/results.csv"
    log "results CSV: ${OUTPUT_DIR}/results.csv"
  else
    log "dry-run: no cluster contacted, nothing created or deleted"
  fi

  step "Planned runs"
  for scenario in "${scenarios[@]}"; do
    for config in "${configs[@]}"; do
      for (( rep=1; rep<=REPETITIONS; rep++ )); do
        run_one "${scenario}" "${config}" "${rep}"
      done
    done
  done

  step "Done (${MODE})"
  if [[ "${MODE}" == "dry-run" ]]; then
    log "re-run with --execute once the Phase-5 integration is wired."
  fi
}

main "$@"
