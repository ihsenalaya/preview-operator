# Plan d'implémentation — AI Enrichment pour Cellenza

## Objectif

Après le déploiement réussi d'un preview environment, générer automatiquement via l'IA :

1. **Des données de seed cohérentes** adaptées au contenu de la Pull Request
2. **Des tests automatisés** ciblant les chemins de code modifiés

Le développeur et le reviewer trouvent l'environnement immédiatement utilisable,
avec des données pertinentes au changement effectué — sans aucune intervention manuelle.

## Pourquoi l'opérateur est indispensable

| Capacité | GitHub Action seule | Avec l'opérateur |
|---|---|---|
| Appeler l'IA | ✅ | ✅ |
| Accès réseau direct à la DB preview | ❌ | ✅ |
| Déclenchement post-migration garanti | ❌ (fragile) | ✅ |
| Credentials DB injectés automatiquement | ❌ | ✅ |
| Reset via `@cellenza enrich` | ❌ | ✅ |
| Indépendant du système CI | ❌ | ✅ |

## Exemple de résultat final

Commentaire PR après déploiement réussi :

```markdown
### 🤖 AI Enrichment
- **Seed** : ✅ Données insérées avec succès
- **Tests** : ✅ 3/3 passés
  - `POST /messages` → 201 Created
  - `GET /messages` → 200 OK
  - `GET /messages/1` → 200 OK
```

Commande Copilot pour relancer :

```
@cellenza enrich pr-42
```

---

## Fichiers à créer / modifier

| Fichier | Action |
|---|---|
| `api/v1alpha1/cellenza_types.go` | Nouveaux types CRD |
| `api/v1alpha1/zz_generated.deepcopy.go` | Auto-généré (`make generate`) |
| `config/crd/bases/*.yaml` | Auto-généré (`make manifests`) |
| `internal/ai/client.go` | **Nouveau** — client IA HTTP |
| `internal/controller/ai_enrichment.go` | **Nouveau** — logique principale |
| `internal/controller/cellenza_controller.go` | Branchement étape 10 |
| `internal/controller/github.go` | Section AI dans commentaire PR |
| `internal/extension/commands.go` | Commande `@cellenza enrich` |
| `cmd/main.go` | Env var `AI_API_URL` |

---

## Étapes d'implémentation

### ÉTAPE 1 — Nouveaux types dans le CRD

**Fichier :** `api/v1alpha1/cellenza_types.go`

Ajouter les structs suivantes :

```go
// AIEnrichmentSpec configures AI-powered seed data and test generation
// after the preview environment reaches the Running phase.
type AIEnrichmentSpec struct {
    // Enabled controls whether AI enrichment runs after the environment is ready.
    // +kubebuilder:default=false
    Enabled bool `json:"enabled,omitempty"`

    // APISecretRef points to a Secret containing the AI provider API key (key: "api-key").
    // +optional
    APISecretRef *SecretKeyRef `json:"apiSecretRef,omitempty"`

    // Model is the AI model to use (e.g. "gpt-4o-mini", "gpt-4o").
    // +kubebuilder:default="gpt-4o-mini"
    // +optional
    Model string `json:"model,omitempty"`

    // Seed configures AI seed data generation and execution.
    // +optional
    Seed *AIEnrichmentTaskSpec `json:"seed,omitempty"`

    // Tests configures AI test generation and execution.
    // +optional
    Tests *AIEnrichmentTaskSpec `json:"tests,omitempty"`
}

// AIEnrichmentTaskSpec configures one AI enrichment task (seed or tests).
type AIEnrichmentTaskSpec struct {
    // Enabled controls whether this task runs.
    // +kubebuilder:default=false
    Enabled bool `json:"enabled,omitempty"`

    // Image is the container image used to run the task.
    // Defaults to "postgres:15-alpine" for seed, "python:3.12-slim" for tests.
    // +optional
    Image string `json:"image,omitempty"`
}

// SecretKeyRef points to a specific key inside a Kubernetes Secret.
type SecretKeyRef struct {
    // Name is the Secret name.
    // +kubebuilder:validation:MinLength=1
    Name string `json:"name"`

    // Namespace is the Secret namespace. Defaults to cellenza-operator-system.
    // +optional
    Namespace string `json:"namespace,omitempty"`

    // Key is the Secret data key. Defaults to "api-key".
    // +optional
    Key string `json:"key,omitempty"`
}

// AIEnrichmentStatus describes the observed AI enrichment state.
type AIEnrichmentStatus struct {
    // Phase: Pending | Generating | Running | Succeeded | Failed
    // +optional
    Phase string `json:"phase,omitempty"`

    // SeedStatus: Skipped | Running | Succeeded | Failed
    // +optional
    SeedStatus string `json:"seedStatus,omitempty"`

    // TestsStatus: Skipped | Running | Succeeded | Failed
    // +optional
    TestsStatus string `json:"testsStatus,omitempty"`

    // TestResults contains stdout lines from the test job.
    // +optional
    TestResults []string `json:"testResults,omitempty"`

    // Summary is a human-readable enrichment summary for the PR comment.
    // +optional
    Summary string `json:"summary,omitempty"`

    // Error stores the latest enrichment error message.
    // +optional
    Error string `json:"error,omitempty"`

    // CompletedAt is when the enrichment completed.
    // +optional
    CompletedAt *metav1.Time `json:"completedAt,omitempty"`
}
```

Ajouter dans `CellenzaSpec` :

```go
// AIEnrichment configures AI-powered seed data and test generation after deployment.
// +optional
AIEnrichment *AIEnrichmentSpec `json:"aiEnrichment,omitempty"`
```

Ajouter dans `CellenzaStatus` :

```go
// AIEnrichment describes the observed AI enrichment state.
// +optional
AIEnrichment *AIEnrichmentStatus `json:"aiEnrichment,omitempty"`
```

Ajouter dans les constantes de conditions :

```go
ConditionAIEnrichmentReady = "AIEnrichmentReady"
```

---

### ÉTAPE 2 — Régénérer les manifests CRD

```bash
make manifests generate
```

Met à jour automatiquement :
- `config/crd/bases/platform.company.io_cellenzas.yaml`
- `api/v1alpha1/zz_generated.deepcopy.go`

**Ne pas modifier ces fichiers manuellement.**

---

### ÉTAPE 3 — Client IA HTTP

**Nouveau fichier :** `internal/ai/client.go`

Client HTTP compatible OpenAI (fonctionne avec OpenAI, GitHub Models, Azure OpenAI — même format d'API).

```go
package ai

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "strings"
    "time"
)

const defaultModel = "gpt-4o-mini"

// Client is an OpenAI-compatible AI client.
type Client struct {
    BaseURL    string
    APIKey     string
    Model      string
    HTTPClient *http.Client
}

// NewClient creates a new AI client.
func NewClient(baseURL, apiKey, model string) *Client {
    if model == "" {
        model = defaultModel
    }
    return &Client{
        BaseURL:    strings.TrimRight(baseURL, "/"),
        APIKey:     apiKey,
        Model:      model,
        HTTPClient: &http.Client{Timeout: 60 * time.Second},
    }
}

// GenerateRequest holds the context sent to the AI.
type GenerateRequest struct {
    PRDiff   string // git diff of the pull request (truncated to 8000 chars)
    DBSchema string // pg_dump --schema-only output (empty if no database)
    AppURL   string // preview app URL (e.g. http://app:8080)
    Branch   string
    PRNumber int
}

// GenerateResponse holds the AI-generated content.
type GenerateResponse struct {
    SeedSQL    string // SQL INSERT statements to populate the preview database
    TestScript string // Python script using requests to test the app
}

// Generate calls the AI API and returns seed SQL and a test script.
func (c *Client) Generate(ctx context.Context, req GenerateRequest) (*GenerateResponse, error) {
    systemPrompt := `You are a developer tool for Kubernetes preview environments.
Given a pull request diff and optionally a database schema, generate:
1. seed_sql: SQL INSERT statements that populate the preview database with realistic data
   relevant to the PR changes. Use only tables that exist in the schema.
   If no schema is provided, return an empty string.
2. test_script: A Python script using the 'requests' library that tests the HTTP endpoints
   modified or added by the PR. The script must use the APP_URL environment variable as base URL.
   Print each test result on a separate line as: "PASS <method> <path>" or "FAIL <method> <path> - <reason>".
   Exit with code 1 if any test fails.

Respond ONLY with valid JSON: {"seed_sql": "...", "test_script": "..."}`

    userPrompt := fmt.Sprintf(
        "Branch: %s\nPR #%d\n\nDiff:\n%s\n\nDB Schema:\n%s\n\nApp URL env var: APP_URL=%s",
        req.Branch, req.PRNumber,
        truncate(req.PRDiff, 8000),
        truncate(req.DBSchema, 4000),
        req.AppURL,
    )

    body, _ := json.Marshal(map[string]any{
        "model": c.Model,
        "messages": []map[string]string{
            {"role": "system", "content": systemPrompt},
            {"role": "user", "content": userPrompt},
        },
        "temperature":     0.2,
        "response_format": map[string]string{"type": "json_object"},
    })

    httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
        c.BaseURL+"/chat/completions", bytes.NewReader(body))
    if err != nil {
        return nil, err
    }
    httpReq.Header.Set("Content-Type", "application/json")
    httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)

    resp, err := c.HTTPClient.Do(httpReq)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    respBody, _ := io.ReadAll(resp.Body)
    if resp.StatusCode != http.StatusOK {
        return nil, fmt.Errorf("AI API error %d: %s", resp.StatusCode, string(respBody))
    }

    var apiResp struct {
        Choices []struct {
            Message struct {
                Content string `json:"content"`
            } `json:"message"`
        } `json:"choices"`
    }
    if err := json.Unmarshal(respBody, &apiResp); err != nil || len(apiResp.Choices) == 0 {
        return nil, fmt.Errorf("invalid AI response: %w", err)
    }

    var generated struct {
        SeedSQL    string `json:"seed_sql"`
        TestScript string `json:"test_script"`
    }
    if err := json.Unmarshal([]byte(apiResp.Choices[0].Message.Content), &generated); err != nil {
        return nil, fmt.Errorf("failed to parse AI JSON response: %w", err)
    }

    return &GenerateResponse{
        SeedSQL:    generated.SeedSQL,
        TestScript: generated.TestScript,
    }, nil
}

func truncate(s string, max int) string {
    if len(s) <= max {
        return s
    }
    return s[:max] + "\n... (truncated)"
}
```

---

### ÉTAPE 4 — Fetch du diff PR depuis GitHub

**Nouveau fichier :** `internal/controller/ai_enrichment.go` (première fonction)

Réutilise `r.githubToken()` et `r.GitHubHTTPClient` existants dans `github.go`.

```go
const aiEnrichmentConfigMap = "ai-enrichment"
const aiSeedJobName         = "ai-seed"
const aiTestJobName         = "ai-tests"
const aiSchemaJobName       = "ai-schema-dump"

func (r *CellenzaReconciler) fetchPRDiff(ctx context.Context, c *platformv1alpha1.Cellenza, token string) (string, error) {
    if !githubEnabled(c) || c.Spec.GitHub.Owner == "" {
        return "", nil
    }
    url := fmt.Sprintf("%s/repos/%s/%s/pulls/%d",
        r.GitHubAPIBaseURL, c.Spec.GitHub.Owner, c.Spec.GitHub.Repo, c.Spec.PRNumber)

    req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
    req.Header.Set("Authorization", "Bearer "+token)
    req.Header.Set("Accept", "application/vnd.github.diff")

    resp, err := r.GitHubHTTPClient.Do(req)
    if err != nil {
        return "", err
    }
    defer resp.Body.Close()
    body, _ := io.ReadAll(resp.Body)
    if resp.StatusCode != http.StatusOK {
        return "", fmt.Errorf("GitHub diff fetch error %d", resp.StatusCode)
    }
    return string(body), nil
}
```

---

### ÉTAPE 5 — Fetch du schéma DB via Job `pg_dump`

**Fichier :** `internal/controller/ai_enrichment.go`

Crée un Job éphémère dans le namespace preview.

```
Job: ai-schema-dump
  Image: postgres:15-alpine
  Command: ["sh", "-c", "pg_dump --schema-only -h postgres -U $POSTGRES_USER $POSTGRES_DB"]
  EnvFrom: Secret postgres-credentials
  TTL: 60s
```

Séquence :
1. Créer le Job si absent
2. Si Job en cours → requeue 5s
3. Si Job terminé → lire les logs du pod via `r.KubeClient`
4. Supprimer le Job (nettoyage immédiat, TTL en backup)
5. Retourner le DDL en string

---

### ÉTAPE 6 — Appel IA + stockage dans ConfigMap

**Fichier :** `internal/controller/ai_enrichment.go`

```go
func (r *CellenzaReconciler) generateAndStoreAIContent(
    ctx context.Context,
    c *platformv1alpha1.Cellenza,
    nsName string,
) error {
    // 1. Lire la clé API depuis le Secret référencé dans spec.aiEnrichment.apiSecretRef
    // 2. fetchPRDiff() si github.enabled
    // 3. fetchDBSchema() si database.enabled (via Job pg_dump)
    // 4. Appeler ai.Client.Generate() avec diff + schema
    // 5. Créer ConfigMap "ai-enrichment" dans nsName :
    //      data["seed.sql"]  = SeedSQL généré
    //      data["test.py"]   = TestScript généré
}
```

Le ConfigMap est créé **une seule fois** — si il existe déjà, la fonction retourne nil sans appeler l'IA (idempotent).

---

### ÉTAPE 7 — Job d'exécution du seed IA

**Fichier :** `internal/controller/ai_enrichment.go`

```
Job: ai-seed (dans namespace preview)
  Image: postgres:15-alpine (ou spec.aiEnrichment.seed.image)
  Command: ["psql", "-h", "postgres", "-U", "$(POSTGRES_USER)", "-d", "$(POSTGRES_DB)", "-f", "/data/seed.sql"]
  Volume: ConfigMap "ai-enrichment" → clé "seed.sql" → /data/seed.sql
  EnvFrom: Secret postgres-credentials
  backoffLimit: 1
  ttlSecondsAfterFinished: 300
```

Suit exactement le même pattern que `reconcileDatabaseTask()` dans `cellenza_controller.go`.

Retourne : `"Skipped"` / `"Running"` / `"Succeeded"` / `"Failed"`

---

### ÉTAPE 8 — Job d'exécution des tests IA

**Fichier :** `internal/controller/ai_enrichment.go`

```
Job: ai-tests (dans namespace preview)
  Image: python:3.12-slim (ou spec.aiEnrichment.tests.image)
  Command: ["sh", "-c", "pip install requests -q && python /data/test.py"]
  Volume: ConfigMap "ai-enrichment" → clé "test.py" → /data/test.py
  Env: APP_URL=http://app:8080
  backoffLimit: 0
  ttlSecondsAfterFinished: 300
```

Après complétion : lire les logs du pod pour extraire les lignes `PASS`/`FAIL`.
Stocker dans `status.aiEnrichment.testResults`.

---

### ÉTAPE 9 — Orchestrateur `reconcileAIEnrichment()`

**Fichier :** `internal/controller/ai_enrichment.go`

```
reconcileAIEnrichment(ctx, cellenza, nsName)
  │
  ├── Guard: spec.aiEnrichment == nil || !enabled → return nil (skip)
  ├── Guard: status.aiEnrichment.phase == "Succeeded" → return nil (idempotent)
  │
  ├── ConfigMap "ai-enrichment" absent ?
  │     → update status.phase = "Generating"
  │     → appeler generateAndStoreAIContent()
  │     → requeue 5s
  │
  ├── spec.aiEnrichment.seed.enabled ?
  │     → reconcileAISeedJob()
  │     → "Running" → requeue 10s
  │     → "Failed"  → update status.seedStatus, continuer quand même
  │
  ├── spec.aiEnrichment.tests.enabled ?
  │     → reconcileAITestJob()
  │     → "Running" → requeue 10s
  │     → lire logs → stocker dans status.testResults
  │
  └── Tous terminés
        → update status.aiEnrichment.phase = "Succeeded"
        → SetCondition AIEnrichmentReady = True
        → update status (Status().Update)
```

---

### ÉTAPE 10 — Branchement dans le controller principal

**Fichier :** `internal/controller/cellenza_controller.go`

Ajouter le champ dans la struct :

```go
type CellenzaReconciler struct {
    client.Client
    Scheme           *runtime.Scheme
    GitHubAPIBaseURL string
    GitHubHTTPClient *http.Client
    KubeClient       kubernetes.Interface
    AIAPIBaseURL     string  // ← nouveau
}
```

Ajouter une fonction helper :

```go
func aiEnrichmentEnabled(c *platformv1alpha1.Cellenza) bool {
    return c.Spec.AIEnrichment != nil && c.Spec.AIEnrichment.Enabled
}
```

Dans la fonction `Reconcile()`, après la transition vers `Running` (après le `Status().Update` de l'étape 9 actuelle) :

```go
// 10. AI Enrichment — déclenché après Running
if aiEnrichmentEnabled(cellenza) {
    if result, err := r.reconcileAIEnrichment(ctx, cellenza, nsName); err != nil || result.RequeueAfter > 0 {
        return result, err
    }
}
```

---

### ÉTAPE 11 — Mise à jour du commentaire PR GitHub

**Fichier :** `internal/controller/github.go`

Dans la fonction qui construit le commentaire de succès (chercher `buildReadyComment` ou équivalent),
ajouter une section conditionnelle si `c.Status.AIEnrichment != nil` :

```go
func buildAIEnrichmentSection(c *platformv1alpha1.Cellenza) string {
    ai := c.Status.AIEnrichment
    if ai == nil {
        return ""
    }

    var sb strings.Builder
    sb.WriteString("\n### 🤖 AI Enrichment\n")

    seedIcon := statusIcon(ai.SeedStatus)
    testsIcon := statusIcon(ai.TestsStatus)

    if c.Spec.AIEnrichment.Seed != nil && c.Spec.AIEnrichment.Seed.Enabled {
        sb.WriteString(fmt.Sprintf("- **Seed** : %s `%s`\n", seedIcon, ai.SeedStatus))
    }
    if c.Spec.AIEnrichment.Tests != nil && c.Spec.AIEnrichment.Tests.Enabled {
        sb.WriteString(fmt.Sprintf("- **Tests** : %s `%s`\n", testsIcon, ai.TestsStatus))
        for _, line := range ai.TestResults {
            sb.WriteString(fmt.Sprintf("  - `%s`\n", line))
        }
    }
    if ai.Error != "" {
        sb.WriteString(fmt.Sprintf("\n> ⚠️ Erreur : %s\n", ai.Error))
        sb.WriteString("> Relancer avec `@cellenza enrich pr-N`\n")
    }
    return sb.String()
}

func statusIcon(s string) string {
    switch s {
    case "Succeeded":
        return "✅"
    case "Failed":
        return "❌"
    case "Running":
        return "⏳"
    default:
        return "⏭️"
    }
}
```

---

### ÉTAPE 12 — Commande Copilot `@cellenza enrich`

**Fichier :** `internal/extension/commands.go`

Ajouter dans le switch de dispatch :

```go
case "enrich":
    return cmdEnrich(ctx, r, args)
```

Implémentation :

```go
func cmdEnrich(ctx context.Context, r client.Client, args []string) string {
    // 1. Parser l'argument "pr-42" ou "42"
    name := parsePRName(args)
    if name == "" {
        return "Usage : `@cellenza enrich pr-<N>`"
    }

    // 2. Fetch le Cellenza
    c := &platformv1alpha1.Cellenza{}
    if err := r.Get(ctx, types.NamespacedName{Name: name}, c); err != nil {
        return fmt.Sprintf("Environnement `%s` introuvable.", name)
    }

    // 3. Reset status.aiEnrichment pour forcer la re-génération
    c.Status.AIEnrichment = nil
    if err := r.Status().Update(ctx, c); err != nil {
        return fmt.Sprintf("Erreur lors du reset : %v", err)
    }

    // 4. Supprimer le ConfigMap et les Jobs dans le namespace preview
    nsName := fmt.Sprintf("preview-pr-%d", c.Spec.PRNumber)
    // → kubectl delete configmap ai-enrichment -n nsName
    // → kubectl delete job ai-seed ai-tests -n nsName (ignorer NotFound)

    return fmt.Sprintf(
        "**Enrichissement IA relancé** pour `%s`\n\nL'opérateur va :\n"+
            "1. Relire le diff PR\n2. Régénérer le seed SQL\n3. Rejouer les tests\n\n"+
            "Suivi : `@cellenza status %s`", name, name)
}
```

Ajouter dans `@cellenza help` :

```
| `@cellenza enrich pr-42` | Relance la génération IA de seed et de tests |
```

---

### ÉTAPE 13 — Configuration de l'opérateur

**Fichier :** `cmd/main.go`

Lire l'URL de l'API IA depuis l'environnement :

```go
aiAPIURL := os.Getenv("AI_API_URL")
if aiAPIURL == "" {
    aiAPIURL = "https://api.openai.com/v1"
}
```

Passer au reconciler :

```go
if err = (&controller.CellenzaReconciler{
    Client:           mgr.GetClient(),
    Scheme:           mgr.GetScheme(),
    GitHubAPIBaseURL: defaultGitHubAPIBaseURL,
    GitHubHTTPClient: &http.Client{Timeout: 15 * time.Second},
    KubeClient:       kubeClient,
    AIAPIBaseURL:     aiAPIURL,  // ← nouveau
}).SetupWithManager(mgr); err != nil {
    ...
}
```

**Secret Kubernetes à créer une fois :**

```bash
kubectl create secret generic ai-api-key \
  --namespace=cellenza-operator-system \
  --from-literal=api-key="sk-..."
```

**Exemple de manifest Cellenza avec AI Enrichment :**

```yaml
apiVersion: platform.company.io/v1alpha1
kind: Cellenza
metadata:
  name: pr-42
spec:
  branch: feature/ma-feature
  prNumber: 42
  image: ghcr.io/ihsenalaya/cellenza-demo-app:latest
  database:
    enabled: true
  github:
    enabled: true
    owner: ihsenalaya
    repo: cellenza-operator
    commentOnReady: true
    tokenSecretRef:
      name: github-token-pr-42
  aiEnrichment:
    enabled: true
    apiSecretRef:
      name: ai-api-key
    model: gpt-4o-mini
    seed:
      enabled: true
    tests:
      enabled: true
```

---

### ÉTAPE 14 — Régénérer, lint, tests

```bash
make manifests generate   # Régénère CRD YAML + deepcopy
make lint-fix             # Corrige le style golangci-lint
make test                 # Tests unitaires + envtest
```

---

### ÉTAPE 15 — Test manuel end-to-end

```bash
# 1. Créer le secret API key
kubectl create secret generic ai-api-key \
  --namespace=cellenza-operator-system \
  --from-literal=api-key="$OPENAI_API_KEY"

# 2. Appliquer le manifest avec aiEnrichment activé
kubectl apply -f docs/examples/ai-enrichment-demo.yaml

# 3. Suivre le cycle de vie
kubectl get cellenza pr-42 --watch

# 4. Vérifier les Jobs créés dans le namespace preview
kubectl get jobs -n preview-pr-42

# 5. Lire les logs du job de tests
kubectl logs -n preview-pr-42 job/ai-tests

# 6. Vérifier le commentaire PR sur GitHub

# 7. Tester la commande Copilot
# @cellenza enrich pr-42
```

**Vérifications attendues :**
- `kubectl get cellenza pr-42 -o jsonpath='{.status.aiEnrichment}'` → phase `Succeeded`
- Jobs `ai-seed` et `ai-tests` en état `Complete`
- Commentaire PR GitHub avec section `🤖 AI Enrichment`
- `@cellenza status pr-42` affiche l'état de l'enrichissement

---

## Choix technique : provider IA

Le code utilise une API compatible OpenAI (même format pour tous les providers).
Configurer via la variable d'environnement `AI_API_URL` du pod opérateur :

| Provider | AI_API_URL | Secret |
|---|---|---|
| OpenAI (défaut) | `https://api.openai.com/v1` | Clé OpenAI |
| GitHub Models | `https://models.inference.ai.azure.com` | Token GitHub (PAT) |
| Azure OpenAI | `https://<resource>.openai.azure.com/openai` | Clé Azure |
| Anthropic (via proxy) | `https://api.anthropic.com/v1` | Clé Anthropic |

**Recommandation production :** OpenAI API (token stable, SLA 99.9%, rate limits prévisibles).
**Recommandation dev/POC :** GitHub Models (réutilise le token GitHub, gratuit dans les limites du free tier).
