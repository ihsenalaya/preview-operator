#!/usr/bin/env bash
#
# inject-fault.sh — apply one fault-injection scenario (F1..F10) of the
# failure-provenance evaluation.
#
# Each scenario injects exactly one controlled fault with a known root cause,
# matching the machine-readable matrix in ../scenarios.yaml and the per-scenario
# description in ../../../docs/research/failure-provenance/experiments.md.
#
# Two kinds of fault:
#
#   repo faults (F1-F6, F8-F10)  modify a checkout of the idp-preview demo app
#                                (or its CI manifest generator). Need --repo-dir.
#   cluster fault (F7)           patch a live Service in a preview namespace.
#                                Needs --namespace.
#
# Every edit is anchored: if the anchor text is missing or not unique the script
# aborts loudly instead of injecting nothing. A silent no-op would invalidate the
# experiment, so this is deliberate.
#
# Usage:
#   inject-fault.sh --scenario F1 --repo-dir ./idp-preview [--commit] [--dry-run]
#   inject-fault.sh --scenario F7 --namespace preview-pr-42 --service backend [--apply]
#   inject-fault.sh --list
#
# This script never deletes anything and never pushes. The orchestration layer
# (../run-kind-experiments.sh) is responsible for fresh checkouts and for
# pushing synthetic pull requests.

set -euo pipefail

# --- scenario catalogue ----------------------------------------------------

SCENARIOS="F1 F2 F3 F4 F5 F6 F7 F8 F9 F10"

describe() {
  case "$1" in
    F1)  echo "invalid SQL migration (database)";;
    F2)  echo "missing environment variable (configuration)";;
    F3)  echo "invalid container image tag (infrastructure)";;
    F4)  echo "broken contract-tested API endpoint (application)";;
    F5)  echo "frontend breaking change (application)";;
    F6)  echo "database readiness timeout (infrastructure)";;
    F7)  echo "broken Service selector (infrastructure)";;
    F8)  echo "artificial latency / timeout (observability)";;
    F9)  echo "incorrect / absent seed data (database)";;
    F10) echo "flaky test (test-reliability)";;
    *)   echo "unknown";;
  esac
}

# --- helpers ---------------------------------------------------------------

die()  { echo "inject-fault: $*" >&2; exit 1; }
note() { echo "  $*"; }

# replace_once FILE OLD NEW — replace exactly one occurrence of OLD with NEW.
# Aborts if OLD is absent or appears more than once.
replace_once() {
  REPL_FILE="$1" REPL_OLD="$2" REPL_NEW="$3" python3 - <<'PY'
import os, sys
f   = os.environ["REPL_FILE"]
old = os.environ["REPL_OLD"]
new = os.environ["REPL_NEW"]
try:
    s = open(f, encoding="utf-8").read()
except OSError as e:
    sys.exit(f"inject-fault: cannot read {f}: {e}")
n = s.count(old)
if n != 1:
    sys.exit(f"inject-fault: anchor must occur exactly once in {f}, found {n}: {old!r}")
open(f, "w", encoding="utf-8").write(s.replace(old, new, 1))
PY
}

# insert_after FILE ANCHOR_SUBSTR TEXT — insert TEXT on the line(s) after the
# single line that contains ANCHOR_SUBSTR. Aborts if the anchor is absent or
# appears on more than one line.
insert_after() {
  INS_FILE="$1" INS_ANCHOR="$2" INS_TEXT="$3" python3 - <<'PY'
import os, sys
f      = os.environ["INS_FILE"]
anchor = os.environ["INS_ANCHOR"]
text   = os.environ["INS_TEXT"]
try:
    lines = open(f, encoding="utf-8").read().splitlines(keepends=True)
except OSError as e:
    sys.exit(f"inject-fault: cannot read {f}: {e}")
hits = [i for i, ln in enumerate(lines) if anchor in ln]
if len(hits) != 1:
    sys.exit(f"inject-fault: anchor must occur on exactly one line in {f}, "
             f"found {len(hits)}: {anchor!r}")
i = hits[0]
block = text if text.endswith("\n") else text + "\n"
lines.insert(i + 1, block)
open(f, "w", encoding="utf-8").write("".join(lines))
PY
}

require_file() { [ -f "$1" ] || die "expected file not found: $1 (is --repo-dir an idp-preview checkout?)"; }
require_absent() { [ -e "$1" ] || return 0; die "file already exists, refusing to overwrite: $1"; }

# --- repo fault injectors --------------------------------------------------
# Each takes the idp-preview checkout directory as $1.

inject_F1() {  # invalid SQL migration
  local repo="$1"
  local target="$repo/migrations/versions/002_fault.py"
  require_file "$repo/migrations/versions/001_initial_schema.py"
  require_absent "$target"
  cp "$FIXTURES/F1-migration-002_fault.py" "$target"
  note "added $target (migration with invalid SQL: CREATE INDX)"
  CHANGED=("migrations/versions/002_fault.py")
}

inject_F2() {  # missing environment variable
  local repo="$1"
  require_file "$repo/app.py"
  replace_once "$repo/app.py" \
    'os.environ.get("DATABASE_URL", "")' \
    'os.environ["DATABASE_URL_FP_MISSING"]'
  note "app.py now reads a non-existent env var -> KeyError on startup"
  CHANGED=("app.py")
}

inject_F3() {  # invalid container image tag
  local repo="$1"
  require_file "$repo/scripts/generate_preview_manifest.py"
  insert_after "$repo/scripts/generate_preview_manifest.py" \
    'args = p.parse_args()' \
    '    args.image = args.image + "-fp-nonexistent"  # F3 fault: image tag does not exist'
  note "generated manifest will reference a non-existent image tag -> ImagePullBackOff"
  CHANGED=("scripts/generate_preview_manifest.py")
}

inject_F4() {  # broken contract-tested API endpoint
  local repo="$1"
  require_file "$repo/app.py"
  # /api/products/top-rated is declared in api/openapi.yaml and exercised by the
  # contract suite, but not by smoke or regression — so only the contract suite
  # fails. Renaming the route makes the declared endpoint return 404.
  replace_once "$repo/app.py" \
    '@app.route("/api/products/top-rated", methods=["GET"])' \
    '@app.route("/api/products/top-rated-fp-broken", methods=["GET"])'
  note "GET /api/products/top-rated now 404s -> contract test fails"
  CHANGED=("app.py")
}

inject_F5() {  # frontend breaking change
  local repo="$1"
  require_file "$repo/frontend.py"
  # The catalogue JS does document.getElementById('catalog-sections'); renaming
  # only the HTML id (not the JS) breaks product rendering for the whole page.
  replace_once "$repo/frontend.py" \
    '<div id="catalog-sections">' \
    '<div id="catalog-sections-fp-broken">'
  note "frontend container id renamed -> catalogue never renders -> e2e fails"
  CHANGED=("frontend.py")
}

inject_F6() {  # database readiness timeout
  local repo="$1"
  require_file "$repo/app.py"
  # Eagerly open a database connection at import time, with a short timeout and
  # no retry. When the app pod starts before the database pod is accepting
  # connections, import fails -> the pod crash-loops and readiness never passes.
  insert_after "$repo/app.py" \
    'app = Flask(__name__)' \
    '
# F6 fault: eager database connection at import, before the database is ready.
psycopg2.connect(os.environ.get("DATABASE_URL", ""), connect_timeout=3).close()'
  note "app connects to the database at import time -> readiness timeout if DB not ready"
  CHANGED=("app.py")
}

inject_F8() {  # artificial latency / timeout
  local repo="$1"
  require_file "$repo/app.py"
  # /api/stats is in the contract spec and is loaded by the e2e dashboard, but
  # is not checked by the smoke suite — so the latency surfaces as e2e/contract
  # timeouts and a long trace span, with healthy pods.
  insert_after "$repo/app.py" \
    'def api_stats():' \
    '    import time as _fp_time; _fp_time.sleep(35)  # F8 fault: injected latency'
  note "GET /api/stats sleeps 35s -> request timeout, long trace span"
  CHANGED=("app.py")
}

inject_F9() {  # incorrect / absent seed data
  local repo="$1"
  require_file "$repo/scripts/generate_preview_manifest.py"
  # Seed data is produced by AI enrichment, so its content cannot be controlled
  # deterministically. This injector disables enrichment in the generated
  # manifest: the catalogue stays empty and the regression suite fails on the
  # missing product record. See README.md for this modelling choice.
  replace_once "$repo/scripts/generate_preview_manifest.py" \
    '  aiEnrichment:
    enabled: true' \
    '  aiEnrichment:
    enabled: false'
  note "AI seed disabled in the manifest -> empty catalogue -> regression fails"
  CHANGED=("scripts/generate_preview_manifest.py")
}

inject_F10() {  # flaky test
  local repo="$1"
  require_file "$repo/tests/e2e.py"
  # Make one e2e test fail non-deterministically (~45% of runs) with no
  # infrastructure or application cause — the negative control for RQ4.
  insert_after "$repo/tests/e2e.py" \
    'def test_discount_filter(page):' \
    '    import random as _fp_rand
    assert _fp_rand.random() > 0.45, "F10 fault: injected non-deterministic (flaky) failure"'
  note "test_discount_filter now fails ~45% of runs with no stable cause"
  CHANGED=("tests/e2e.py")
}

# --- cluster fault injector (F7) -------------------------------------------

inject_F7() {  # broken Service selector
  local ns="$1" svc="$2" apply="$3"
  [ -n "$ns" ]  || die "F7 needs --namespace <preview namespace>"
  [ -n "$svc" ] || die "F7 needs --service <service name>"
  local patch='{"spec":{"selector":{"app":"fp-nonexistent-selector"}}}'
  echo "  Service:   $svc"
  echo "  Namespace: $ns"
  echo "  Patch:     $patch"
  if [ "$apply" = "true" ]; then
    command -v kubectl >/dev/null || die "kubectl not found"
    kubectl -n "$ns" patch service "$svc" --type=merge -p "$patch"
    note "Service selector patched -> Service has no endpoints"
  else
    echo
    echo "  Dry run. To apply:"
    echo "    kubectl -n $ns patch service $svc --type=merge -p '$patch'"
  fi
}

# --- argument parsing ------------------------------------------------------

SCENARIO=""
REPO_DIR=""
NAMESPACE=""
SERVICE=""
DO_COMMIT=false
DRY_RUN=false
DO_APPLY=false

usage() {
  cat <<EOF
inject-fault.sh — fault injection for the failure-provenance evaluation

  --scenario F1..F10   scenario to inject (required, unless --list)
  --repo-dir DIR       idp-preview checkout (repo faults: F1-F6, F8-F10)
  --namespace NS       preview namespace (cluster fault: F7)
  --service NAME       Service to break (F7)
  --commit             commit the change on a branch fp/<scenario>
  --apply              actually apply the cluster patch (F7); default is dry-run
  --dry-run            show what would change without writing (repo faults)
  --list               list all scenarios and exit
  -h, --help           this help
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --scenario)  SCENARIO="${2:-}"; shift 2;;
    --repo-dir)  REPO_DIR="${2:-}"; shift 2;;
    --namespace) NAMESPACE="${2:-}"; shift 2;;
    --service)   SERVICE="${2:-}"; shift 2;;
    --commit)    DO_COMMIT=true; shift;;
    --apply)     DO_APPLY=true; shift;;
    --dry-run)   DRY_RUN=true; shift;;
    --list)
      echo "Fault-injection scenarios:"
      for s in $SCENARIOS; do printf "  %-4s %s\n" "$s" "$(describe "$s")"; done
      exit 0;;
    -h|--help)   usage; exit 0;;
    *)           die "unknown argument: $1 (try --help)";;
  esac
done

[ -n "$SCENARIO" ] || { usage; die "--scenario is required"; }
echo "$SCENARIOS" | grep -qw "$SCENARIO" || die "unknown scenario: $SCENARIO"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FIXTURES="$SCRIPT_DIR/fixtures"
CHANGED=()

echo "=== inject-fault: $SCENARIO — $(describe "$SCENARIO") ==="

# --- F7: cluster fault -----------------------------------------------------
if [ "$SCENARIO" = "F7" ]; then
  inject_F7 "$NAMESPACE" "$SERVICE" "$([ "$DO_APPLY" = true ] && echo true || echo false)"
  exit 0
fi

# --- repo faults -----------------------------------------------------------
[ -n "$REPO_DIR" ] || die "$SCENARIO is a repo fault and needs --repo-dir"
[ -d "$REPO_DIR" ] || die "--repo-dir is not a directory: $REPO_DIR"
REPO_DIR="$(cd "$REPO_DIR" && pwd)"

if [ "$DRY_RUN" = true ]; then
  echo "  dry-run: would inject $SCENARIO into $REPO_DIR (no files written)"
  exit 0
fi

# Snapshot for a clean abort if the working tree was already dirty.
if [ -d "$REPO_DIR/.git" ] && ! git -C "$REPO_DIR" diff --quiet; then
  die "working tree $REPO_DIR has uncommitted changes; inject into a clean checkout"
fi

"inject_$SCENARIO" "$REPO_DIR"

echo "  changed: ${CHANGED[*]}"

if [ "$DO_COMMIT" = true ]; then
  command -v git >/dev/null || die "git not found"
  [ -d "$REPO_DIR/.git" ] || die "--commit needs a git checkout in $REPO_DIR"
  branch="fp/$(echo "$SCENARIO" | tr 'A-Z' 'a-z')"
  git -C "$REPO_DIR" checkout -q -b "$branch" 2>/dev/null || git -C "$REPO_DIR" checkout -q "$branch"
  git -C "$REPO_DIR" add -- "${CHANGED[@]}"
  git -C "$REPO_DIR" commit -q -m "fp fault $SCENARIO: $(describe "$SCENARIO")

Injected by experiments/failure-provenance/injectors/inject-fault.sh.
Do not merge — this is a controlled fault-injection scenario."
  note "committed on branch $branch"
fi

echo "=== done ==="
