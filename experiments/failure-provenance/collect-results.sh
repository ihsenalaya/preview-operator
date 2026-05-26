#!/usr/bin/env bash
#
# collect-results.sh — collect failure evidence artifacts for one preview run.
#
# This script is READ-ONLY: it only ever reads cluster state with `kubectl get`
# and `kubectl logs`. It never creates, modifies, or deletes any resource.
#
# It gathers, into an output directory, everything needed to score one run:
#   * the Preview custom resource (spec + status);
#   * the FailureReport custom resource, if present;
#   * Kubernetes events, pod statuses, job statuses for the preview namespace;
#   * pod and job logs;
#   * test result artifacts, if present.
#
# Usage:
#   ./collect-results.sh --preview <name> [--namespace <ns>] [--output-dir <path>]
#
# Options:
#   --preview   <name>   Preview resource name (required).
#   --namespace <ns>     Preview namespace (default: read from Preview status).
#   --output-dir <path>  Output directory (default: ./results/<preview>-<ts>).
#   -h | --help          Show this help.

set -euo pipefail

log()  { printf '[%s] %s\n' "$(date +%H:%M:%S)" "$*"; }
die()  { printf 'ERROR: %s\n' "$*" >&2; exit 1; }
usage() { sed -n '2,24p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; }

PREVIEW=""
NAMESPACE=""
OUTPUT_DIR=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --preview)    PREVIEW="${2:-}"; shift ;;
    --namespace)  NAMESPACE="${2:-}"; shift ;;
    --output-dir) OUTPUT_DIR="${2:-}"; shift ;;
    -h|--help)    usage; exit 0 ;;
    *)            die "unknown argument: $1 (use --help)" ;;
  esac
  shift
done

[[ -n "${PREVIEW}" ]] || die "--preview <name> is required"
command -v kubectl >/dev/null 2>&1 || die "kubectl not found"
kubectl cluster-info >/dev/null 2>&1 || die "no reachable Kubernetes cluster"

# Resolve namespace from the Preview status when not supplied.
if [[ -z "${NAMESPACE}" ]]; then
  NAMESPACE="$(kubectl get preview "${PREVIEW}" -o jsonpath='{.status.namespaceName}' 2>/dev/null || true)"
fi

if [[ -z "${OUTPUT_DIR}" ]]; then
  OUTPUT_DIR="./results/${PREVIEW}-$(date +%Y%m%d-%H%M%S)"
fi
mkdir -p "${OUTPUT_DIR}"
log "collecting into: ${OUTPUT_DIR}"

# get_into FILE  ARGS...  — run `kubectl get ARGS`, tolerate a missing resource.
get_into() {
  local file="$1"; shift
  if kubectl get "$@" >"${OUTPUT_DIR}/${file}" 2>"${OUTPUT_DIR}/${file}.err"; then
    rm -f "${OUTPUT_DIR}/${file}.err"
    log "collected: ${file}"
  else
    log "skipped (not found): ${file}"
    mv "${OUTPUT_DIR}/${file}.err" "${OUTPUT_DIR}/${file}" 2>/dev/null || true
  fi
}

# --- cluster-scoped artifacts (survive namespace teardown) -------------------

get_into preview.yaml        preview "${PREVIEW}" -o yaml
get_into failurereport.yaml  failurereport "${PREVIEW}-failure" -o yaml
get_into reconcileevents.yaml reconcileevents -o yaml --field-selector="" 2>/dev/null || \
  get_into reconcileevents.yaml reconcileevents -o yaml

# --- namespace-scoped artifacts ---------------------------------------------

if [[ -z "${NAMESPACE}" ]]; then
  log "no preview namespace resolved — namespace already deleted, or Preview not found."
  log "cluster-scoped artifacts above are what survived teardown."
else
  log "preview namespace: ${NAMESPACE}"
  get_into events.txt        events -n "${NAMESPACE}" --sort-by=.lastTimestamp
  get_into pods.txt          pods -n "${NAMESPACE}" -o wide
  get_into pods.yaml         pods -n "${NAMESPACE}" -o yaml
  get_into jobs.txt          jobs -n "${NAMESPACE}" -o wide
  get_into deployments.txt   deployments -n "${NAMESPACE}" -o wide
  get_into services.txt      services -n "${NAMESPACE}" -o wide
  get_into endpoints.txt     endpoints -n "${NAMESPACE}"

  # Pod and job logs (best-effort; --all-containers, previous container too).
  mkdir -p "${OUTPUT_DIR}/logs"
  for pod in $(kubectl get pods -n "${NAMESPACE}" -o name 2>/dev/null || true); do
    safe="${pod#pod/}"
    kubectl logs "${pod}" -n "${NAMESPACE}" --all-containers --tail=200 \
      >"${OUTPUT_DIR}/logs/${safe}.log" 2>/dev/null || true
    kubectl logs "${pod}" -n "${NAMESPACE}" --all-containers --previous --tail=200 \
      >"${OUTPUT_DIR}/logs/${safe}.previous.log" 2>/dev/null || true
  done
  log "collected: pod/job logs -> logs/"
fi

# --- run metadata ------------------------------------------------------------

{
  echo "preview=${PREVIEW}"
  echo "namespace=${NAMESPACE:-<deleted-or-unknown>}"
  echo "collected_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "kube_context=$(kubectl config current-context 2>/dev/null || echo unknown)"
} >"${OUTPUT_DIR}/collection-metadata.txt"

log "done. Artifacts in ${OUTPUT_DIR}"
