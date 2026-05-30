# Troubleshooting — kagent & Test Strategist

Problèmes rencontrés en production et leurs solutions.

---

## 1. TestPlan reste en `Pending` indéfiniment

**Symptôme**
```
kubectl get testplan -n preview-pr-N
NAME          PHASE     AGE
pr-N-abc123   Pending   10m
```
Le controller attend, tombe sur le timeout, et crée un plan FullSuite fallback.

**Cause A — Le CronJob trigger ne tourne pas**
```bash
kubectl get cronjob test-strategist-trigger -n kagent-system
kubectl get jobs -n kagent-system -l app.kubernetes.io/component=test-strategist-trigger
```
Si aucun Job n'a tourné depuis 2 min, vérifier :
- Le CronJob est bien déployé (`kubectl apply -k config/overlays/with-test-strategist`)
- L'image `curlimages/curl` est accessible depuis le cluster

**Cause B — L'agent a du mal à écrire le TestPlan (RBAC)**
```bash
kubectl logs -n kagent-system -l app.kubernetes.io/component=test-strategist-trigger --tail=50
```
Si `403 Forbidden` → vérifier que le ClusterRole et le ClusterRoleBinding sont bien appliqués :
```bash
kubectl apply -f config/rbac/agent_role.yaml
kubectl apply -f config/rbac/agent_rolebinding.yaml
```

**Cause C — L'agent ne trouve pas les TestPlans**
Le prompt de l'agent dit de chercher dans `all namespaces`. Si le MCP server kagent n'a pas les droits cluster-wide, les TestPlans ne seront pas visibles.
→ Vérifier que le ServiceAccount `kagent-test-strategist` a bien un ClusterRoleBinding (pas un RoleBinding namespace).

---

## 2. Le preview utilise FullSuite alors que `mode: Auto` est configuré

**Symptôme**
```
kubectl get preview pr-N -o jsonpath='{.status.testPlanResolution}'
{"source":"Full","rationale":"Full suite selected by FallbackPolicy..."}
```

**Cause A — `testStrategy` absent du Preview CR**
Le pipeline CI n'injecte pas `spec.testStrategy`. Vérifier que le script `generate_preview_manifest.py` est bien appelé dans le workflow et que son output inclut le bloc `testStrategy:`.

```bash
# Vérifier localement ce que le script produit :
python3 scripts/generate_preview_manifest.py \
  --pr-number 1 --branch main --image nginx:alpine \
  --base-sha HEAD~1 --head-sha HEAD \
  --repo owner/repo --repo-owner owner --repo-name repo \
  --deployment-id 0 | grep -A5 testStrategy
```

**Cause B — `changeContext` absent**
Sans `changeContext`, le controller ne peut pas passer le contexte à l'agent (le stub TestPlan est créé sans SHA, l'agent n'a rien à lire).
→ Même vérification que Cause A.

**Cause C — Timeout agent atteint avant que le plan soit rempli**
Le timeout par défaut est 60s. Si l'agent met plus de temps (LLM lent, MCP lent) :
```yaml
spec:
  testStrategy:
    agentTimeoutSeconds: 300  # augmenter selon besoin
```

---

## 3. Plan rejeté — confidence trop basse

**Symptôme**
```
kubectl get testplan pr-N-abc -o jsonpath='{.status}'
{"phase":"Rejected","rejectionReason":"confidence 55 < threshold 70"}
```

**Cause** — L'agent est incertain sur l'impact du diff.

**Solutions**
- Abaisser le threshold si acceptable : `spec.testStrategy.confidenceThreshold: 60`
- Améliorer le contexte donné à l'agent : s'assurer que `diffPatch` est bien populé (voir §6)
- Enrichir les ReconcileEvents historiques — plus il y a d'historique, plus l'agent est confiant

---

## 4. Plan rejeté — `mustRun ∩ canSkip` non vide

**Symptôme**
```
{"phase":"Rejected","rejectionReason":"test \"smoke/*\" appears in both mustRun and canSkip"}
```

**Cause** — L'agent a hallociné et mis la même suite dans les deux listes.

**Solution** — Le prompt système l'interdit explicitement. Si ça arrive souvent :
1. Vérifier que le système prompt dans `k8s/kagent/agents/prompts/test-strategist.md` est bien chargé par l'agent
2. Ajouter une ligne de rappel dans la section `## Important` du CR `test-strategist-agent.yaml`
3. Baisser la température du modèle dans `ModelConfig`

---

## 5. Violation architecturale — controller appelait l'agent via HTTP (résolu v1.0.31)

**Problème historique (avant v1.0.31)**
Le controller appelait directement `triggerTestStrategistAgent()` — une goroutine qui faisait un POST HTTP vers l'A2A endpoint de l'agent. Cela violait le principe du CRD bus : le controller avait un client HTTP vers l'agent.

**Symptômes** — `kagent not enabled` dans les logs même avec `spec.kagent.enabled: true`, race conditions entre la goroutine et le reconcile.

**Résolution**
- Supprimé `triggerTestStrategistAgent()` de `internal/controller/kagent.go`
- Le controller crée à la place un Job éphémère par TestPlan via `createTestStrategistTriggerJob()` (`internal/controller/testplan_strategy.go`). Le pod du Job (`curlimages/curl:8.7.1`) fait le POST HTTP vers l'agent puis se termine ; le Job est nettoyé par `ttlSecondsAfterFinished` (5 min).
- Le controller lui-même ne fait aucun appel HTTP : il crée le stub TestPlan et le Job, puis attend que l'agent remplisse le plan.

---

## 6. `diffPatch` vide — l'agent raisonne sur les noms de fichiers seulement

**Symptôme** — L'agent prend des décisions conservatives (confidence ~70) même pour des diffs évidents.

**Cause** — Le script `generate_preview_manifest.py` n'a pas pu récupérer l'historique git complet.

```bash
# Vérifier dans les logs du job CI :
git fetch --unshallow
# Si "shallow repository" → l'action checkout doit avoir fetch-depth: 0
```

**Solution** — Dans le workflow, s'assurer que git a l'historique complet :
```yaml
- uses: actions/checkout@v4
  with:
    fetch-depth: 0   # nécessaire pour git diff base...head
```
Sans `fetch-depth: 0`, git n'a qu'un clone superficiel et `git diff` retourne rien.

---

## 7. Preview `Failed` — `Init:0/2` — postgres ne démarre pas

**Problème historique (avant v1.0.28)**
`databaseEnabled()` retournait `false` même quand `spec.database.enabled: true` parce que `detectedImpacts.database: false` court-circuitait la décision.

**Résolution**
Supprimé le shortcut `changeContext` de `databaseEnabled()`. La décision est maintenant basée uniquement sur `spec.database.enabled`.

**Vérification**
```bash
kubectl get preview pr-N -o jsonpath='{.spec.database.enabled}'  # doit être true
kubectl get pod -n preview-pr-N                                   # postgres pod doit être Running
```

---

## Commandes de diagnostic rapide

```bash
# État du TestPlan pour une PR
kubectl get testplan -n preview-pr-N
kubectl describe testplan -n preview-pr-N <name>

# Décision de résolution
kubectl get preview pr-N -o jsonpath='{.status.testPlanResolution}' | jq .

# Logs du CronJob trigger
kubectl logs -n kagent-system -l app.kubernetes.io/component=test-strategist-trigger --tail=100

# Derniers ReconcileEvents
kubectl get reconcileevent -n preview-pr-N --sort-by=.spec.occurredAt | tail -10

# TestRun (suites effectivement lancées)
kubectl get testrun -n preview-pr-N
kubectl get testrun -n preview-pr-N <name> -o jsonpath='{.spec.selectedTests}' | jq .
```
