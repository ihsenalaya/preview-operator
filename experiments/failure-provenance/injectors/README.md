# Fault injectors — F1–F10

`inject-fault.sh` applies one controlled fault-injection scenario of the
failure-provenance evaluation. It is the executable form of the `injection`
block of each scenario in [`../scenarios.yaml`](../scenarios.yaml) and of the
matrix in [`../../../docs/research/failure-provenance/experiments.md`](../../../docs/research/failure-provenance/experiments.md).

Each scenario injects **exactly one** fault with a **fixed, known root cause**.
The ground truth is set in `scenarios.yaml` before any run and never changed
afterwards — see the research-integrity rule in the article package.

## Usage

```bash
./inject-fault.sh --list

# Repo faults (F1–F6, F8–F10): patch a checkout of the idp-preview demo app.
git clone https://github.com/ihsenalaya/idp-preview /tmp/idp-preview
./inject-fault.sh --scenario F1 --repo-dir /tmp/idp-preview --commit

# Cluster fault (F7): patch a live Service in a preview namespace.
./inject-fault.sh --scenario F7 --namespace preview-pr-42 --service backend --apply
```

Flags: `--commit` commits the change on branch `fp/<scenario>`; `--dry-run`
previews a repo fault without writing; `--apply` actually applies the F7 cluster
patch (default is dry-run).

## Design — anchored, fail-loud edits

Every edit is **anchored**: the script locates a specific, verified string in
the target file and aborts if that anchor is missing or not unique. A silent
no-op would inject nothing and quietly invalidate a whole experiment cell, so
the script fails loudly instead. Repo faults also refuse to run on a dirty
working tree. The orchestration layer ([`../run-kind-experiments.sh`](../run-kind-experiments.sh))
always injects into a **fresh checkout**, so reverting a fault is simply
discarding that checkout.

The anchors below were verified against `github.com/ihsenalaya/idp-preview` at
the time of writing. If idp-preview changes an anchored line, the corresponding
injector will abort with a clear message — update the anchor, never weaken the
check.

## Scenarios

| ID | Family | File changed | Fault | Expected first failure |
|----|--------|--------------|-------|------------------------|
| F1 | database | `migrations/versions/002_fault.py` (new) | migration runs `CREATE INDX` (invalid SQL) | migration Job |
| F2 | configuration | `app.py` | reads a renamed env var → `KeyError` at startup | app pod CrashLoopBackOff |
| F3 | infrastructure | `scripts/generate_preview_manifest.py` | appends `-fp-nonexistent` to the image tag | ImagePullBackOff |
| F4 | application | `app.py` | renames the `/api/products/top-rated` route → 404 | contract suite |
| F5 | application | `frontend.py` | renames the `catalog-sections` HTML id (JS unchanged) | e2e suite |
| F6 | infrastructure | `app.py` | eager DB connect at import, short timeout, no retry | readiness timeout |
| F7 | infrastructure | live Service (cluster) | sets a Service selector that matches no pods | e2e suite |
| F8 | observability | `app.py` | `GET /api/stats` sleeps 35 s | request timeout / long span |
| F9 | database | `scripts/generate_preview_manifest.py` | disables AI enrichment → no seed data | regression suite |
| F10 | test-reliability | `tests/e2e.py` | `test_discount_filter` fails ~45 % of runs | intermittent (any) |

## Modelling choices and limitations

These are recorded honestly here and feed
[`../../../docs/research/failure-provenance/threats-to-validity.md`](../../../docs/research/failure-provenance/threats-to-validity.md).

- **F4** targets `/api/products/top-rated` because it is declared in
  `api/openapi.yaml` (so the contract suite exercises it) but is *not* checked
  by the smoke or regression suites — this isolates the failure to the contract
  suite. A 404 on a spec-declared endpoint is a contract violation.

- **F6** is inherently a **timing/race fault**: the eager import-time connection
  fails only when the app pod wins the start-up race against the database pod.
  The injector uses a 3 s connect timeout and no retry to lose that race
  reliably on a fresh namespace, but the outcome remains cluster-timing
  dependent. RQ1 explicitly treats this kind of evidence as racy.

- **F9** models **absent** seed data, not malformed seed data. Seed content is
  produced at run time by the AI enrichment step and cannot be controlled
  deterministically, so the injector disables enrichment in the generated
  manifest. The catalogue then stays empty and the regression suite fails on the
  missing product record. The "malformed record" variant is left as future
  work because it cannot be made deterministic with the current demo app.

- **F7** is applied to a **live Service** after the preview namespace exists, so
  it runs from the orchestration layer, not from a synthetic pull request.
  `--service` must name a Service the operator created from `spec.services`.

- **F10** is the **negative control** for RQ4: a correct diagnosis must report
  flakiness and must *not* invent an infrastructure or application cause.
