# Change Context — PR diff as a first-class input

`spec.changeContext` lets the pull request diff drive Kubernetes reconciliation.
Instead of always provisioning a full preview stack, the controller reads the
classified diff and skips work that isn't needed for this specific PR.

## Architecture

```
PR opened or updated
  └─ GitHub Actions: diff-classifier --repo org/app --pr 42 --token $TOKEN
       │  queries GitHub API, classifies each changed file
       │  outputs JSON merge-patch
       ▼
     kubectl patch preview pr-42 --type=merge -p "$PATCH"
       │  spec.changeContext populated
       ▼
     Preview CR — controller reads detectedImpacts:
       database=true   → provision PostgreSQL + run migrations
       apiContract=true → enable Microcks contract tests
       requiresSeedData=true → run AI seed-data job
       frontend && !backend → skip regression tests
```

`diff-classifier` runs in CI and has the GitHub token. The controller never
fetches PR data from GitHub itself.

## How to use

### 1. Build the classifier (once per release)

```bash
go build -o dist/diff-classifier ./cmd/diff-classifier
```

### 2. Run in GitHub Actions

```yaml
- name: Build diff-classifier
  run: go build -o /tmp/diff-classifier ./cmd/diff-classifier

- name: Classify PR diff
  id: classify
  run: |
    PATCH=$(/tmp/diff-classifier \
      --repo "${{ github.repository }}" \
      --pr "${{ github.event.pull_request.number }}" \
      --token "${{ secrets.GITHUB_TOKEN }}")
    echo "patch=$PATCH" >> "$GITHUB_OUTPUT"

- name: Patch Preview CR
  run: |
    kubectl patch preview pr-${{ github.event.pull_request.number }} \
      --type=merge -p '${{ steps.classify.outputs.patch }}'
```

The full workflow is at `.github/workflows/preview-env.yml`.

### 3. Sample output (JSON merge-patch)

```json
{
  "spec": {
    "changeContext": {
      "diffRef": {
        "provider": "github",
        "repository": "ihsenalaya/idp-preview",
        "pullRequestNumber": 42,
        "baseSHA": "abc111",
        "headSHA": "abc123"
      },
      "summary": {
        "changedFilesCount": 8,
        "additions": 120,
        "deletions": 45
      },
      "changedFiles": [
        {"path": "db/migrations/20260510_add_table.sql", "type": "database-migration"},
        {"path": "api/openapi.yaml",                     "type": "api-contract"},
        {"path": "src/payments/service.ts",              "type": "backend"},
        {"path": "web/src/components/Payment.tsx",       "type": "frontend"}
      ],
      "detectedImpacts": {
        "database": true,
        "apiContract": true,
        "backend": true,
        "frontend": true,
        "requiresSeedData": true,
        "requiresContractTests": true,
        "requiresRegressionTests": true
      }
    }
  }
}
```

## Sample Preview CR with changeContext

```yaml
apiVersion: platform.company.io/v1alpha1
kind: Preview
metadata:
  name: pr-42
spec:
  branch: feature/payments
  prNumber: 42
  image: ghcr.io/ihsenalaya/idp-preview:sha-abc123
  ttl: 48h
  database:
    enabled: true
    migration:
      enabled: true
      command: ["python", "-m", "alembic", "upgrade", "head"]
  kagent:
    enabled: true
  github:
    enabled: true
    owner: ihsenalaya
    repo: idp-preview
  changeContext:
    diffRef:
      provider: github
      repository: ihsenalaya/idp-preview
      pullRequestNumber: 42
      baseSHA: abc111
      headSHA: abc123
    summary:
      changedFilesCount: 4
      additions: 87
      deletions: 12
    changedFiles:
      - path: db/migrations/20260510_add_orders.sql
        type: database-migration
      - path: src/orders/service.py
        type: backend
    detectedImpacts:
      database: true
      backend: true
      requiresSeedData: true
      requiresRegressionTests: true
```

## Controller behavior reference

| `detectedImpacts` field | Effect when `false` (and `changeContext` is set) |
|---|---|
| `database` | DB not provisioned even if `spec.database.enabled=true` |
| `apiContract` | Microcks contract tests skipped |
| `requiresSeedData` | AI seed-data job skipped |
| `frontend && !backend` | Regression tests skipped |

## Backward compatibility

`spec.changeContext` is **optional**. Any existing Preview CR without the field
continues to work exactly as before — all features enabled by their respective
`spec.*` flags, with no impact from change context.

## File classification rules

| Pattern | Type |
|---|---|
| `db/migrations/**`, `*.sql` | `database-migration` |
| `api/openapi.yaml`, `api/**/*.proto` | `api-contract` |
| `src/**/*.{ts,py,go}` | `backend` |
| `web/**`, `frontend/**`, `*.tsx`, `*.css` | `frontend` |
| `**/*.md`, `docs/**` | `docs` |
| everything else | `other` |

Rules are applied in priority order; first match wins. Add new rules in
`pkg/changecontext/classifier/classifier.go`.
