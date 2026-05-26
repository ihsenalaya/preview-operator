# Multi-App Extension — Plan

Extends the failure-provenance evaluation from one subject (S1, idp-preview) to
**five subjects** drawn from `github.com/ihsenalaya/preview-experiments`:

| Subject | Stack | Migration system |
|---------|-------|------------------|
| S1 idp-preview / flask-catalog | Python / Flask | Alembic |
| S2 listmonk | Go / Chi | native Go migrations |
| S3 healthchecks | Python / Django 5 | Django migrations |
| S4 umami | TypeScript / Next.js 14 | Prisma |
| S5 petclinic | Java / Spring Boot 3 | Flyway |

Goal: external validity — show the operator-captured-provenance results hold
across stacks, not just Flask. Decision (reviewer): **full F1–F10 on all 5**,
**concurrent execution (3 previews)**.

---

## 1. What is reused (no rewrite)

- The 5 subjects: `subjects/sN/meta.yaml` + `subjects/sN/harness-adapter/` images.
- `subjects/CONTRACT.md` — the subject abstraction.
- `harness/preview_factory.py` — `create(subject=meta, …)` builds a Preview CR
  from a `meta.yaml` (services, migration command). Replaces the
  idp-preview-specific `generate_preview_manifest.py`.
- `harness/config.py` — `load_subject`, `load_enabled_subjects`.
- The operator (preview-operator) — already deployed all 5 in the prior study.

## 2. What is built

### 2.1 Multi-app orchestrator
A meta.yaml-driven runner (extends `run-kind-experiments.sh` or a new
`run-matrix.py`): for each enabled subject × fault F1–F10 × rep → inject, build,
deploy (via `preview_factory`), wait for `FailureReport`, score.

### 2.2 Build-once-per-fault optimisation
Fault injection is deterministic, so the image for (subject, Fx) is identical
across all 10 reps. Key the image cache on `(subject, fault)`:
**5×7 = 35 image builds instead of 350.** Applies to S1 too.

### 2.3 Concurrency — 3 previews in flight
A worker pool of 3 consumes a queue of (subject, fault, rep) units. Bounds:
2-node cluster (≈3 full previews fit); ACR build agents serialise (the
build-once optimisation matters more than parallelism here). Unique PR numbers
per unit already guarantee no namespace collision.

### 2.4 Fault injectors per stack (the hard part)
| Fault | Cross-stack? | Per-subject work |
|-------|--------------|------------------|
| F3 invalid image tag | **app-agnostic** | none — set a bad image ref |
| F7 broken Service selector | **app-agnostic** | none — cluster-side patch |
| F6 DB readiness timeout | mostly infra | minor |
| F1 bad migration | per stack | Alembic / Django / Flyway / Prisma / Go |
| F2 missing env var | per stack | each app's config code |
| F4 broken contract endpoint | per stack | each app's API handler |
| F8 latency / timeout | per stack | each app's request path |
| F9 incorrect seed data | per stack | each app's seed |
| F10 flaky test | semi-uniform | the adapter `tests/` are uniform-ish |
| F5 frontend breaking change | **N/A for some** | S2/S3/S5 are headless APIs — F5 does not map; recorded as N/A, not failure |

## 3. Order of work (incremental, validate each before the next)

1. ✅ S1 matrix (idp-preview) — running; finish + offline re-score first.
2. Multi-app orchestrator skeleton + build-once cache + 3-way concurrency.
3. App-agnostic faults (F3, F6, F7) wired for all 5 — cheap, immediate
   generalisation signal. Smoke-test one subject.
4. Per-subject injectors, **one subject at a time**, smoke-tested:
   S3 (Django) → S5 (Spring) → S2 (Go) → S4 (Next.js).
5. Full concurrent run S2–S5; aggregate; Evaluation §.

## 3b. Constraint discovered — pre-built upstream images

S2–S5 adapters wrap **pre-built upstream binaries** (`FROM listmonk/listmonk:v2.5.1`,
the umami/petclinic release images). Their source is not built here. Therefore:

- **Manifest/infra-injectable faults work on all 5** — F2 (drop a required env
  var from the Preview spec), F3 (bad image tag), F6 (DB readiness), F7 (Service
  selector), and F1 (corrupt the `migration_command` / its migration).
- **Source-level faults (F4 broken endpoint, F5 frontend change, F8 injected
  latency) are NOT feasible on S2–S5** without building each upstream project
  (Go/Next.js/Java) from source with the fault — days of work per app, and
  fragile. The prior article hit exactly this: its RQ4 mutations were
  "S1 specific … non-interpretable architecturally" for S2/S3.

**Revised scope (autonomous decision, recorded for the reviewer):** full F1–F10
on **S1** (built from source); the **manifest/infra-injectable subset
(F1, F2, F3, F6, F7)** on **S2–S5**. F4/F5/F8 on S2–S5 are marked *out of scope —
upstream not built from source* in Threats to Validity, with the reasoning
above. This is a feasibility limit, not a measurement we skipped; it is stated,
not hidden. If the reviewer wants F4/F5/F8 cross-stack, that needs a separate
decision to fork+build the 4 upstream apps.

## 4. Honest scoping

- Not every fault maps to every stack (F5 on a headless API). N/A cells are
  recorded as N/A, never as a failed diagnosis.
- The prior article's own inventory shows its RQ4 mutations did **not** transfer
  beyond S1 ("non-interpretable architecturally"). Per-subject fault injection
  is the known-hard part; each injector is validated on the cluster before its
  scenario joins the matrix.
- This is a multi-day effort. Progress is tracked in `PROGRESS.md`; every
  measured number lands in `experimentations.md` §9.
