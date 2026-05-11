# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability, please **do not open a public GitHub issue**.  
Send a report to **e7seno@gmail.com** with:

- Description of the vulnerability
- Steps to reproduce
- Potential impact

You will receive an acknowledgement within 48 hours and a fix timeline within 7 days.

---

## Threat Model

### Assets

| Asset | Location | Sensitivity |
|---|---|---|
| GitHub PAT | `preview-github-token` Secret (operator namespace) | High — grants repo write access |
| AI API key | `ai-api-key` Secret (operator namespace) | Medium — triggers billable API calls |
| PostgreSQL credentials | `postgres-credentials` Secret (preview namespace) | Low — ephemeral, preview-only data |
| PR diff content | Sent to AI provider | Medium — may contain proprietary code |

### Trust Boundaries

```
Internet
    │
    │  HTTP/HTTPS (ingress-nginx)
    ▼
Preview namespace (preview-pr-N)
    │  no direct cluster-admin access
    │  NetworkPolicy: only ingress-nginx + inter-pod allowed
    ▼
Operator namespace (preview-operator-system)
    │  secrets: GitHub PAT, AI key
    │  cluster-scoped RBAC
    ▼
Kubernetes API Server
```

### Attack Vectors and Mitigations

#### 1. Secrets exposure via pod exec / log leak

**Risk:** A compromised app container could read mounted secrets or env vars.

**Mitigations:**
- PostgreSQL credentials are injected via `secretKeyRef` — not stored in env files
- Operator secrets (GitHub PAT, AI key) are **never** mounted into preview pods
- Preview namespaces have `pod-security.kubernetes.io/enforce: baseline` — prevents privileged containers
- `pod-security.kubernetes.io/warn: restricted` surfaces hardening opportunities without blocking workloads

**Residual risk:** An attacker with code execution inside the preview pod can exfiltrate `DATABASE_URL`. Acceptable for ephemeral preview data; not for production databases.

#### 2. Namespace escape / cross-PR data pollution

**Risk:** A pod in `preview-pr-42` reaches `preview-pr-43` and reads its database.

**Mitigations:**
- Each preview runs in its own namespace with a `NetworkPolicy` (`preview-isolation`) that:
  - Denies all ingress from other namespaces by default
  - Allows ingress only from `ingress-nginx` namespace and same-namespace pods
  - Allows all egress (needed for AI API, GitHub API, image pulls)
- PostgreSQL binds to `ClusterIP :5432` — only reachable within the preview namespace via DNS
- `ResourceQuota` prevents one preview from exhausting cluster resources and starving others

**Residual risk:** Egress is unrestricted. A malicious container could exfiltrate data outbound. Restricting egress to known CIDRs would harden this further.

#### 3. Privilege escalation via Job images

**Risk:** A Job (smoke, regression, e2e, AI) runs an attacker-controlled image.

**Mitigations:**
- Smoke and checkpoint Jobs use pinned images (`postgres:15-alpine`, `python:3.12-slim`)
- E2E Job uses `playwright/python:v1.44.0-jammy` — pinned by the operator
- Pod Security Standard `baseline` blocks `privileged: true`, hostPath mounts, and host networking

**Residual risk:** If the CI pipeline is compromised and pushes a malicious image, the operator will run it. Admission controllers (OPA/Gatekeeper) and image signing (Cosign) should be added in production.

#### 4. GitHub token abuse

**Risk:** A long-lived GitHub PAT is exfiltrated from the operator's memory or etcd.

**Mitigations:**
- The PAT is stored in a Kubernetes Secret (encrypted at rest if etcd encryption is enabled)
- The operator reads the PAT via `secretKeyRef` — it is never logged or stored in CR status
- RBAC restricts access to the Secret to the operator's ServiceAccount only

**Residual risk (PAT model):** GitHub PATs are long-lived and hard to rotate automatically. See [PAT vs GitHub App](#pat-current-demo-mode-vs-github-app-production-target) below.

#### 5. AI prompt injection

**Risk:** A PR diff contains instructions that hijack the AI's behavior.

**Mitigations:**
- The AI output is limited to SQL (`seed.sql`) and Python (`test.py`) — executed in isolated Jobs
- Malicious SQL could corrupt the ephemeral preview database — acceptable risk for preview-only data
- The system prompt explicitly instructs the AI to generate only data and tests matching the schema

---

## PAT (current demo mode) vs GitHub App (production target)

### Current setup — Personal Access Token

The operator uses a long-lived GitHub PAT stored in a Kubernetes Secret:

```bash
kubectl create secret generic preview-github-token \
  --namespace preview-operator-system \
  --from-literal=token="ghp_xxxxxxxxxxxx"
```

**Why it works for demos:**
- Simple to set up — one command
- No GitHub App registration required
- Works immediately with public and private repos

**Why it is NOT production-ready:**
- PATs are long-lived — a leaked token remains valid until manually revoked
- PATs grant permissions tied to the token owner's account — over-scoped by default
- No automatic rotation
- `GITHUB_TOKEN` from Actions expires after ~1 hour; a long-lived PAT is required for the operator, which increases blast radius

### Target setup — GitHub App (production)

A GitHub App provides short-lived installation tokens (1-hour TTL, auto-rotated):

```
GitHub App
  ├── installed on: your-org/idp-preview
  ├── permissions: deployments (write), pull-requests (write)
  └── private key stored in: Kubernetes Secret (RSA PEM)

Operator flow:
  1. Load private key from Secret
  2. Generate JWT (signed, 10-min TTL)
  3. Exchange JWT for installation token (1-hour TTL)
  4. Use installation token for all GitHub API calls
  5. Token expires automatically — no revocation needed
```

**Production security properties:**
- Installation tokens expire after 1 hour — leaked tokens become useless quickly
- Permissions scoped to the installed repository only
- Private key can be rotated without changing the installation
- GitHub audit log shows all actions attributed to the App

**Migration path:**
1. Create a GitHub App at `github.com/settings/apps/new`
2. Set permissions: `Deployments: Write`, `Pull requests: Write`
3. Install the App on your repository
4. Download the private key (PEM)
5. Create a Secret: `kubectl create secret generic preview-github-app --from-file=private-key.pem=./key.pem --from-literal=app-id=<ID> --from-literal=installation-id=<ID> -n preview-operator-system`
6. Update the operator to generate JWT + exchange for installation token before each GitHub API call

---

## RBAC

The operator's ServiceAccount is granted the minimum permissions required:

| Resource | Verbs | Justification |
|---|---|---|
| `previews` | get, list, watch, create, update, patch, delete | Core CR management |
| `namespaces` | get, list, watch, create, update, patch, delete | Preview namespace lifecycle |
| `resourcequotas` | get, list, watch, create, update, patch, delete | Quota enforcement |
| `networkpolicies` | get, list, watch, create, update, patch, delete | Namespace isolation |
| `secrets` | get, list, watch, create, update, patch, delete | PostgreSQL credentials |
| `configmaps` | get, list, watch, create, update, patch, delete | AI output, test suite state |
| `pods` | get, list, watch | Availability check, diagnostics |
| `pods/log` | get | Failure log collection |
| `events` | get, list, watch | Failure event collection |
| `deployments` | get, list, watch, create, update, patch, delete | App + PostgreSQL deployments |
| `jobs` | get, list, watch, create, update, patch, delete | Migration, seed, test, AI jobs |
| `services` | get, list, watch, create, update, patch, delete | ClusterIP services |
| `ingresses` | get, list, watch, create, update, patch, delete | Preview URL routing |

The Copilot Extension's ServiceAccount has a narrower set — it can only read/patch Preview CRs and read pod logs. It cannot access Secrets.

---

## Known Limitations

1. **No image signing verification** — the operator trusts `spec.image` without verifying signatures. Use Cosign + admission policy in production.
2. **Egress unrestricted** — preview pods can reach the internet. Restrict to known CIDRs if the cluster hosts sensitive data.
3. **GitHub PAT is shared** across all preview environments. Per-team keys would reduce blast radius.
4. **No audit logging** — operator actions are not emitted as Kubernetes audit events. Enable API server audit logging in production.
5. **etcd not encrypted by default** — Kubernetes Secrets are base64-encoded unless `EncryptionConfiguration` is configured on the API server.
