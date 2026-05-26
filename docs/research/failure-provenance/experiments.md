# Experiments — Fault Injection Matrix

This chapter defines the controlled fault-injection experiments. Ten scenarios
(F1–F10) cover six failure families. Each scenario is deterministic: the injected
change is scripted, and the root cause is fixed before any run.

The machine-readable form of this matrix is `experiments/failure-provenance/scenarios.yaml`.
This document and that file **must stay consistent** — `scenarios.yaml` is authoritative
for the harness.

---

## Failure families

| Family | Scenarios | Diagnostic signal centre of gravity |
|--------|-----------|-------------------------------------|
| Database | F1, F9 | Job logs, SQL errors |
| Configuration | F2 | Pod restarts, env diff |
| Infrastructure | F3, F6, F7 | Kubernetes events, resource status |
| Application | F4, F5 | Test failures, code diff |
| Observability / performance | F8 | Traces, timeout logs |
| Test reliability | F10 | Failure pattern across repetitions |

---

## Scenario template

Every scenario is specified with the same nine fields:

- **Injected change** — the exact scripted modification.
- **Expected failed test suite** — which suite is expected to fail.
- **Expected Kubernetes signal** — pod/job/event/resource symptom.
- **Expected log evidence** — log lines a complete bundle should contain.
- **Expected trace evidence** — only where applicable; otherwise "n/a".
- **Expected diagnosis** — the human-readable probable cause.
- **Ground truth** — `(component, failure_category)`.
- **Success criterion** — when the run counts as a correct diagnosis.

The *expected evidence* fields together define the `expected_evidence_items` list used
by the Evidence Preservation Rate metric (`metrics.md`).

---

## F1 — Invalid SQL migration

- **Injected change.** Introduce a syntactically or semantically invalid statement into
  a migration file under `db/migrations/` (e.g. a typo in `CREATE TABLE`, or a column
  reference that does not exist).
- **Expected failed test suite.** Migration job fails before the app starts; migration
  test suite fails.
- **Expected Kubernetes signal.** Migration `Job` reaches `Failed`; backoff visible.
- **Expected log evidence.** PostgreSQL/migration-tool SQL error in the migration job
  log (e.g. `syntax error at or near`, `column ... does not exist`).
- **Expected trace evidence.** n/a.
- **Expected diagnosis.** "Database migration failed due to an invalid SQL statement."
- **Ground truth.** component = `migration-job`, category = `database`.
- **Success criterion.** Diagnosis names the migration job, category `database`, and
  references the SQL error log line and the changed migration file.

## F2 — Missing environment variable

- **Injected change.** Remove or rename a required env var (or the ConfigMap/Secret key
  it comes from) consumed by the application container.
- **Expected failed test suite.** Smoke tests fail; app never becomes ready.
- **Expected Kubernetes signal.** Application pod enters `CrashLoopBackOff`.
- **Expected log evidence.** Pod log shows a missing-configuration / key error (e.g.
  `KeyError`, `environment variable ... not set`, nil-config panic).
- **Expected trace evidence.** n/a.
- **Expected diagnosis.** "Application crashes because a required environment variable
  is missing."
- **Ground truth.** component = `app-deployment`, category = `configuration`.
- **Success criterion.** Diagnosis names the app deployment, category `configuration`,
  and references the CrashLoopBackOff state and the missing-variable log line.

## F3 — Invalid container image tag

- **Injected change.** Set `spec.image` (or a service image) to a non-existent tag.
- **Expected failed test suite.** No suite runs; the environment never reaches Running.
- **Expected Kubernetes signal.** Pod stuck in `ImagePullBackOff` / `ErrImagePull`;
  `Failed to pull image` warning event.
- **Expected log evidence.** No container logs (container never starts); evidence is in
  the Kubernetes event, not logs.
- **Expected trace evidence.** n/a.
- **Expected diagnosis.** "The specified container image tag does not exist and cannot
  be pulled."
- **Ground truth.** component = `app-deployment`, category = `infrastructure`.
- **Success criterion.** Diagnosis names the deployment, category `infrastructure`,
  references the `ImagePullBackOff` event and the offending image reference.

## F4 — Removed or broken API endpoint

- **Injected change.** Remove or change a backend route/controller so an API endpoint
  the contract depends on is missing or returns an incompatible response.
- **Expected failed test suite.** Contract tests fail (and/or regression tests).
- **Expected Kubernetes signal.** App pod is healthy/`Running` — there is **no**
  infrastructure symptom; this is an application-logic failure.
- **Expected log evidence.** Backend access/error log showing 404/5xx on the endpoint;
  contract test output showing the mismatch.
- **Expected trace evidence.** Optional — a failed request span if tracing is enabled.
- **Expected diagnosis.** "A backend API change broke a contract-tested endpoint."
- **Ground truth.** component = `backend` / `app-deployment`, category = `application`.
- **Success criterion.** Diagnosis names the backend, category `application`,
  references the contract-test failure and the changed backend route/controller file.

## F5 — Frontend breaking change

- **Injected change.** Change a UI selector, route, or element id that the end-to-end
  browser tests depend on.
- **Expected failed test suite.** E2E tests fail.
- **Expected Kubernetes signal.** All pods healthy; no infrastructure symptom.
- **Expected log evidence.** E2E/browser test log showing the element/route not found.
- **Expected trace evidence.** n/a (unless the frontend is traced).
- **Expected diagnosis.** "A frontend change broke an end-to-end user flow."
- **Ground truth.** component = `frontend`, category = `application`.
- **Success criterion.** Diagnosis names the frontend, category `application`,
  references the E2E failure and the changed frontend file.

## F6 — Database readiness timeout

- **Injected change.** Make the application start before the database is ready (remove
  a readiness gate / init wait, or slow the database start) so the app times out
  connecting.
- **Expected failed test suite.** Smoke tests fail intermittently or consistently.
- **Expected Kubernetes signal.** Readiness/liveness probe failure events on the app
  pod; database pod still starting.
- **Expected log evidence.** App log shows a database connection timeout/refused;
  database pod status shows it was not yet ready.
- **Expected trace evidence.** Optional — long/failed startup spans if traced.
- **Expected diagnosis.** "The application started before the database was ready and
  timed out connecting."
- **Ground truth.** component = `app-deployment` (ordering vs `postgres`),
  category = `infrastructure`.
- **Success criterion.** Diagnosis identifies the readiness/ordering problem, category
  `infrastructure`, and references the probe-failure events and the connection-timeout
  log. *Note:* F6 is deliberately a harder, multi-signal case for RQ2.

## F7 — Broken Service selector

- **Injected change.** Change a `Service` selector so it no longer matches the
  application pods.
- **Expected failed test suite.** E2E (and smoke) tests time out — traffic never
  reaches the pods.
- **Expected Kubernetes signal.** The `Service` has **no endpoints** (empty
  `Endpoints`/`EndpointSlice`); pods themselves are healthy.
- **Expected log evidence.** Test log shows connection timeouts; app logs show **no**
  inbound requests (a notable *absence* of evidence).
- **Expected trace evidence.** n/a.
- **Expected diagnosis.** "The Service selector does not match the application pods, so
  the Service has no endpoints."
- **Ground truth.** component = `service`, category = `infrastructure`.
- **Success criterion.** Diagnosis names the service, category `infrastructure`,
  references the empty-endpoints signal and the changed service manifest. *Note:* this
  scenario tests whether the bundle captures *resource state* (endpoints), not just
  logs.

## F8 — Artificial latency / timeout

- **Injected change.** Inject latency into a backend handler or dependency call (e.g. a
  sleep, a throttled dependency) so a request exceeds the test timeout.
- **Expected failed test suite.** E2E tests fail on timeout.
- **Expected Kubernetes signal.** Pods healthy; no infrastructure symptom.
- **Expected log evidence.** Test log shows a request timeout; backend log shows a slow
  handler.
- **Expected trace evidence.** A long-duration span for the slow operation — **only if
  app-level OpenTelemetry instrumentation and a trace backend are deployed**. If not,
  record trace evidence as *not available* and rely on logs (see `methodology.md` §2).
- **Expected diagnosis.** "A slow backend operation exceeded the request timeout."
- **Ground truth.** component = `backend`, category = `observability` (performance).
- **Success criterion.** Diagnosis identifies the latency/timeout, category
  `observability`, references the timeout log and, where available, the long trace
  span. F8 is the primary scenario exercising trace evidence.

## F9 — Incorrect seed data

- **Injected change.** Make required test data missing or malformed in the seed step
  (omit a row, corrupt a value).
- **Expected failed test suite.** Regression tests fail (a query/assertion depends on
  the missing/malformed record).
- **Expected Kubernetes signal.** Seed `Job` may succeed (data is *wrong*, not absent)
  or fail (data is *missing*); app pods healthy.
- **Expected log evidence.** Seed job log; regression test log showing the failing
  assertion; the missing/malformed database record.
- **Expected trace evidence.** n/a.
- **Expected diagnosis.** "Required seed data is missing or malformed, causing a
  regression test to fail."
- **Ground truth.** component = `seed-job`, category = `database`.
- **Success criterion.** Diagnosis names the seed step, category `database`, references
  the seed log / regression failure and the missing record.

## F10 — Flaky test

- **Injected change.** Introduce non-deterministic test behaviour (e.g. a race, a
  time-of-day dependency, an unseeded random) — **not** a real infrastructure or
  application fault.
- **Expected failed test suite.** Any suite — intermittently across repetitions.
- **Expected Kubernetes signal.** None consistent; infrastructure is healthy.
- **Expected log evidence.** Intermittent failure pattern across the ≥10 repetitions;
  no stable infrastructure or application cause.
- **Expected trace evidence.** n/a.
- **Expected diagnosis.** "The test is flaky (non-deterministic); there is no stable
  infrastructure or application root cause."
- **Ground truth.** component = `test-suite`, category = `test-reliability`.
- **Success criterion.** Diagnosis identifies flakiness, category `test-reliability`,
  and — importantly — does **not** invent an infrastructure/application cause. F10 is a
  deliberate *negative control* for RQ4: a system that confidently blames a real
  component here is hallucinating.

---

## Scenario summary table

| ID | Family | Injected change | Failing suite | Key Kubernetes signal | Ground truth (component / category) |
|----|--------|-----------------|---------------|-----------------------|-------------------------------------|
| F1 | Database | Invalid SQL in migration | Migration | Migration `Job` Failed | `migration-job` / `database` |
| F2 | Configuration | Removed/renamed env var | Smoke | `CrashLoopBackOff` | `app-deployment` / `configuration` |
| F3 | Infrastructure | Non-existent image tag | (none runs) | `ImagePullBackOff` event | `app-deployment` / `infrastructure` |
| F4 | Application | Broken backend endpoint | Contract | Pods healthy (logic fault) | `backend` / `application` |
| F5 | Application | Changed UI selector/route | E2E | Pods healthy (logic fault) | `frontend` / `application` |
| F6 | Infrastructure | App starts before DB ready | Smoke | Probe-failure events | `app-deployment` / `infrastructure` |
| F7 | Infrastructure | Broken Service selector | E2E | Service has no endpoints | `service` / `infrastructure` |
| F8 | Observability | Injected latency | E2E | Pods healthy; slow span | `backend` / `observability` |
| F9 | Database | Missing/malformed seed data | Regression | Seed `Job` (mixed) | `seed-job` / `database` |
| F10 | Test reliability | Non-deterministic test | Any (intermittent) | None consistent | `test-suite` / `test-reliability` |

---

## Repetitions and configurations

Per `methodology.md`:

- **Repetitions.** Each scenario is run **≥10 times** per comparison configuration
  (C1–C5). Scenarios with high variance (F10, and all LLM-in-the-loop runs) use more.
- **Configurations.** C1 logs only · C2 logs+events · C3 logs+events+tests · C4 full
  bundle · C5 provenance graph + full bundle. The grounding ablation (free-form vs
  grounded) is applied on top of C4 and C5 for RQ4.
- **Concurrency.** Where feasible, run parallel previews at **1, 5, 10** concurrent PRs
  to observe capture under load (RQ5).
- **Clusters.** Kind (primary). AKS if available — reported **separately**.

Total core runs (lower bound): 10 scenarios × 5 configurations × 10 repetitions =
**500 runs** on Kind, excluding the RQ4 grounding ablation, the concurrency sweep, and
any AKS replication. Budget cluster time accordingly (this is Phase 5, not a single
session).

---

## Demo application — confirmed support

The reference demo application is **`idp-preview`** (`github.com/ihsenalaya/idp-preview`):
a Flask + PostgreSQL catalog app with Alembic migrations, an OpenAPI contract
(`api/openapi.yaml`), a frontend, and `smoke` / `contract` (Microcks) / `regression` /
`e2e` (Playwright) test scripts that all emit `PASS` / `FAIL` lines. Jaeger and the
OpenTelemetry auto-instrumentation are deployed (`jaeger.yaml`, `otel.yaml`), so trace
evidence is realistically available.

A structural review confirms the demo app supports **all ten** scenarios. Injection
point per scenario (authoritative against the actual app structure):

| ID | Injection point in `idp-preview` |
|----|----------------------------------|
| F1 | New broken migration in `migrations/versions/` |
| F2 | Remove the `DATABASE_URL` default in `app.py`; operator does not inject it |
| F3 | Invalid image tag in the CI workflow / `scripts/generate_preview_manifest.py` |
| F4 | Break an endpoint handler in `app.py` (e.g. wrong status, broken SQL) |
| F5 | Change a selector / `data-testid` / markup in `frontend.py` |
| F6 | Add a startup delay / wrong host in `app.py` `get_conn()` |
| F7 | Service-selector mismatch (operator-managed; inject via Preview CR) |
| F8 | Add `pg_sleep(...)` or a sleep in an `app.py` query handler |
| F9 | Malform the AI seed job input / inject a bad seed record |
| F10 | Add a non-deterministic wait/race in `tests/e2e.py` or `tests/regression.py` |

Remaining check before Phase 5:

- [ ] For F5, confirm the `data-testid` attributes the Playwright `tests/e2e.py`
      expects are actually present in `frontend.py` (the exploration flagged a possible
      gap) — if missing, add them to the demo app first so the *baseline* run passes.

---

## Appendix — Defect chain discovered during execution

Recorded for full Q1 transparency. None of these defects altered the
final headline numbers; they are documented as an honest reproducibility
appendix. Each defect → fix → commit is traceable.

### S1 matrix (Phase 5a)

The S1 matrix went through 16 defect cycles between 2026-05-22 14:46
and 2026-05-23 10:30 before the final 100/100 capture set landed. The
defects, in chronological order, are the items in `PROGRESS.md §6`:
status-clobber (`b808588`), OpenAI 429 retry (`5b43311`), manifest-
generator CWD (`cfc9a41`), baseline image unpullable (`b1bb928`),
migration enabling (`a1206dd`), AI 429 retry on the operator side
(`a1206dd`), `ActiveDeadlineSeconds` on test Jobs (`aacb1b3`), FullSuite
bypass under the experiment label (`39231a5`, `ea84a29`), F7
wait_for_service helper (`6f65849`, `0291247`).

### Multi-app matrix (Phase 5b)

Three additional defects were discovered when running the same code
against the multi-app subjects S2-S5:

1. Subject-index collision when two parallel processes covered the same
   `--subjects` list with different positions → stable `SUBJECT_IDX`
   mapping introduced (commit `b7e5a23`).
2. F7 race against the operator's Service reconciler (commit `ca5a88a`)
   — redesigned at the meta.yaml level (see `threats-to-validity.md §7.1`).
3. `collectionDurationMillis` rounded to zero on sub-millisecond
   assembly + `omitempty` dropped the field — operator patched to also
   emit microsecond resolution and memory counters (commit `e0eae6f`).

### Closure path

Each defect was closed with a code change, a regression test, and a
re-collection of the affected cells before any number in `EVALUATION-
DRAFT.md` was published. `PROGRESS.md` ticks 0-9 + the `Q1-COMPLIANCE.md`
file-by-file checklist are the audit trail.

### Phase 5c — post-teardown replay comparators (2026-05-25)

A fourth set of edits brought the post-teardown comparator baselines
(K8sGPT-PT and Kagent-PT) onto the same five-subject footing as the
operator's own diagnoser. The work-units below are tracked in
`experimentations.md §11` and `execution-plan.md` Phase 5c.

1. **`failurereport.yaml` synthesis** — 340 captures across S2--S5
   had a `report.json` but no CRD YAML on disk because the original
   matrix run skipped the YAML export step. A one-shot converter
   (`/tmp/convert-reports.py`) read each `report.json` and emitted the
   equivalent `failurereport.yaml` in the capture's `artifacts/`
   directory (the JSON already encoded the CRD status fields used by
   the replay harness, so the conversion is loss-less).
2. **K8sGPT replay harness** (`analysis/k8sgpt-replay.py`) — per
   capture, decode `evidenceItems` into a synthetic
   `Pod`/`Job`/`Service` bundle, apply to a disposable namespace on
   the AKS cluster, patch the status subresource, invoke
   `k8sgpt analyze --namespace <ns> --filter Pod,Job,…`, save the
   JSON, delete the namespace. Sequential by design (~10 s/cell).
3. **Kagent replay harness** (`analysis/kagent-replay.py`) — same
   synthesis + namespace lifecycle, but instead of `k8sgpt analyze`
   the script POSTs an A2A `message/send` to the in-cluster
   `kagent-system/k8s-agent` via `kubectl port-forward` and parses the
   agent's JSON reply. Slower (~25--30 s/cell) because of the
   agentic reasoning chain.
4. **Unified scoring** (`analysis/34-b2-post-teardown-rescore.py` +
   `analysis/35-unified-rescore.py`) — pool S1--S5 captures with a
   vocab-aware aligned-top-1 matcher; emit per-subject and pooled
   summaries, including the comparator results.
5. **Unified figures** (`analysis/36-unified-figures.py`) —
   `engine-pooled-bars-s1s5.png` and
   `engine-by-subject-heatmap-s1s5.png` replace the S1-only
   bar/heatmap figures.

The article's §5.8 B2a-PT and B2b-PT paragraphs and the cross-subject
pooled table (`tab:multi-engine-pooled`) now report five-subject
numbers; the figures are referenced from §sec:multi-app.

### Phase 5e/5f — rep-parity backfill for S2--S5 (2026-05-25 13:00--17:00)

A post-Phase-5c audit revealed that S2--S5 still carried 7 reps for
the fault subset $\{F1, F2, F3, F6, F7\}$ versus 10 reps for S1 ---
a residue of the option-A refactor of 2026-05-23. Phase 5e + 5f
close this 60-cell gap so the per-fault $n$ is uniform across
subjects.

1. **Phase 5e capture backfill** (`/tmp/backfill-missing-reps-v2.py`)
   --- imports `multiapp/run-matrix.py`'s `run_unit()`, targets
   exactly the 60 missing $(\textrm{subject}, F, r)$ tuples. v1 ran
   with `REPORT_TIMEOUT_S=360` and lost F2/F3 to operator-side
   capture latency; v2 with `REPORT_TIMEOUT_S=900` and concurrency 5
   closed 46/60 in $\sim$55\,min (F1, F2, F6, F7 all OK; F3 still
   timed out).
2. **Full scoring on the new captures** (`/tmp/full-scoring.sh`) ---
   one pass per engine $\times$ mode $\times$ level for the 46 new
   reports: 5 levels $\times$ (rule-grounded + LLM-A grounded +
   LLM-A freeform + LLM-B grounded + LLM-B freeform) $\times$ 46 =
   1150 diag files. Pool A (`gpt-4o-mini`) at concurrency 10, pool B
   (`cohere-command-a`) at concurrency 3 to honour TPM quotas.
3. **F3 root-cause diagnosis + Phase 5f**
   (`/tmp/backfill-f3-only.py`) --- manual probe (`pr-99999`) plus
   operator-log inspection identified the operator's
   `provisioningDeadline = 15 * time.Minute` as the only path to a
   FailureReport when the migration Job pod sits in
   `ImagePullBackOff` indefinitely. Phase 5f raises
   `REPORT_TIMEOUT_S` to 1200\,s for a 5\,min margin and re-captures
   the 12 remaining F3 cells, concurrency 3 (each cell takes
   $\sim$15\,min wall-clock, total $\sim$80\,min for the four
   batches).
4. **K8sGPT-PT + Kagent-PT re-replay on the 46 new captures**
   (`analysis/k8sgpt-replay.py --subset s2+s3+s4+s5 --max-reps 3
   --force` + same for `kagent-replay.py`) --- forces re-processing
   for the 120 $(\textrm{subject}, F, r)$ cells with $r \in \{1, 2,
   3\}$ so the comparator outputs match the new captures.
5. **Idempotent rescore re-run** ---
   `35-unified-rescore.py`, `36-unified-figures.py`,
   `34-b2-post-teardown-rescore.py`, `19-evidence-precision.py`,
   `21-lmm.py`, `21c-glmm-logit-s1s5.py`, `22-tukey.py`,
   `23-mcnemar.py`, `24-cd-diagrams.py`, `25-pareto.py`,
   `30b-evidence-ladder-s1s5.py`, `17-multiapp-rescore.py` --- all
   re-emit on the now-uniform 5-subject $\times$ 10-fault $\times$
   $\leq$10-rep matrix.

`threats-to-validity.md §9.3` documents the rep-parity backfill and
the F3 operator-deadline finding. `PROGRESS.md` carries the per-step
ticks (timestamped tour-de-table 12:35--17:00 UTC).
