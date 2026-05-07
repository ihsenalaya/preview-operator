# Work in Progress — Multi-Service Support (front + back + DB)

## Objectif
Ajouter la capacité de déployer un **frontend** + **backend** + **base de données** dans un seul environnement Cellenza via un champ `spec.services[]`.

## Exemple YAML cible
```yaml
apiVersion: platform.company.io/v1alpha1
kind: Cellenza
metadata:
  name: pr-42
spec:
  branch: feature/my-feature
  prNumber: 42
  image: ignored-when-services-set
  resourceTier: medium
  ttl: 48h
  database:
    enabled: true
    databaseName: appdb
    migration:
      enabled: true
      command: ["python", "-m", "alembic", "upgrade", "head"]
  services:
    - name: backend
      image: ghcr.io/acme/myapp-api:sha-abc123
      port: 8080
      pathPrefix: /api
      env:
        - name: LOG_LEVEL
          value: debug
    - name: frontend
      image: ghcr.io/acme/myapp-ui:sha-abc123
      port: 3000
      pathPrefix: /
      env:
        - name: VITE_API_URL
          value: http://pr-42.preview.localtest.me/api
```

## Ce qui est déjà fait ✅

### `api/v1alpha1/cellenza_types.go`
- [x] Ajout du struct `ServiceSpec` (Name, Image, Port, PathPrefix, Replicas, Env)
- [x] Ajout du champ `Services []ServiceSpec` dans `CellenzaSpec`

### `internal/controller/cellenza_controller.go`
- [x] Import `"sort"` ajouté
- [x] Helpers ajoutés : `multiServiceEnabled`, `serviceDeploymentName`, `appServiceURL`, `mainAppImage`
- [x] `reconcileResourceQuota` — headroom par service supplémentaire ajouté
- [x] `reconcileDeployment` → dispatch vers `reconcileSingleDeployment` (ancien code) ou `reconcileServiceDeployments` (nouveau)
- [x] `reconcileServiceDeployments` — nouvelle fonction : crée un Deployment `svc-<name>` par service avec init-container postgres wait, credentials DB injectés, env vars customs
- [x] `reconcileMultiServices` — nouvelle fonction : crée un ClusterIP Service `svc-<name>` par service
- [x] `reconcileMultiServiceIngress` — nouvelle fonction : ingress path-based (plus long prefix en premier, sans rewrite-target)

## Ce qui reste à faire ❌

### `internal/controller/cellenza_controller.go`

1. **`reconcileService`** (ligne ~1193) — ajouter dispatch :
   ```go
   func (r *CellenzaReconciler) reconcileService(ctx context.Context, c *platformv1alpha1.Cellenza, nsName string) error {
       if multiServiceEnabled(c) {
           return r.reconcileMultiServices(ctx, c, nsName)
       }
       // ... code existant inchangé ...
   ```

2. **`reconcileIngress`** (ligne ~1223) — ajouter dispatch :
   ```go
   func (r *CellenzaReconciler) reconcileIngress(ctx context.Context, c *platformv1alpha1.Cellenza, nsName string) error {
       if multiServiceEnabled(c) {
           return r.reconcileMultiServiceIngress(ctx, c, nsName)
       }
       // ... code existant inchangé ...
   ```

3. **`appDeploymentReady`** (ligne ~1109) — ajouter paramètre `deployName string` :
   ```go
   func (r *CellenzaReconciler) appDeploymentReady(ctx context.Context, nsName, deployName string) (bool, string, error) {
       deploy := &appsv1.Deployment{}
       if err := r.Get(ctx, types.NamespacedName{Name: deployName, Namespace: nsName}, deploy); err != nil {
   ```

4. **`handleAppAvailability`** (ligne ~1030) — vérifier tous les services en mode multi :
   ```go
   func (r *CellenzaReconciler) handleAppAvailability(ctx context.Context, c *platformv1alpha1.Cellenza, nsName string) (bool, ctrl.Result, error) {
       deployNames := []string{"app"}
       if multiServiceEnabled(c) {
           deployNames = make([]string, len(c.Spec.Services))
           for i, svc := range c.Spec.Services {
               deployNames[i] = serviceDeploymentName(svc.Name)
           }
       }
       for _, deployName := range deployNames {
           appReady, appReason, err := r.appDeploymentReady(ctx, nsName, deployName)
           if err != nil {
               result, err := r.setFailedStatus(ctx, c, appReason, err)
               return true, result, err
           }
           if !appReady {
               c.Status.Phase = platformv1alpha1.PhaseProvisioning
               c.Status.NamespaceName = nsName
               c.Status.ObservedGeneration = c.Generation
               c.SetCondition(metav1.Condition{
                   Type:               platformv1alpha1.ConditionReady,
                   Status:             metav1.ConditionFalse,
                   Reason:             appReason,
                   Message:            fmt.Sprintf("Waiting for %s deployment to become available", deployName),
                   LastTransitionTime: metav1.Now(),
               })
               if err := r.Status().Update(ctx, c); err != nil {
                   return true, ctrl.Result{}, err
               }
               syncGitHubAfterStatus(ctx, r, c, "")
               return true, ctrl.Result{RequeueAfter: 10 * time.Second}, nil
           }
       }
       return false, ctrl.Result{}, nil
   }
   ```

### `internal/controller/tests.go`

5. **`smokeScript`** (ligne ~44) — lire `APP_URL` depuis env :
   Remplacer :
   ```python
   BASE='http://app:80'
   ```
   Par :
   ```python
   import os
   BASE=os.environ.get('APP_URL','http://app:80')
   ```
   Et dans `smokeTestJob` (ligne ~239), injecter `APP_URL` après la création du job :
   ```go
   job := r.testJob(c, nsName, smokeJobName, image, cmd, testSuiteConfigMap, "smoke.py", smokeJobName, false)
   job.Spec.Template.Spec.Containers[0].Env = append(
       job.Spec.Template.Spec.Containers[0].Env,
       corev1.EnvVar{Name: "APP_URL", Value: appServiceURL(c)},
   )
   return job
   ```

6. **`regressionTestJob`** (ligne ~252) :
   - Remplacer `c.Spec.Image` par `mainAppImage(c)` pour l'image
   - Remplacer `"http://app:80"` par `appServiceURL(c)` pour `APP_URL`

7. **`e2eTestJob`** (ligne ~274) :
   - Remplacer `c.Spec.Image` (variable `appImage`) par `mainAppImage(c)`
   - Remplacer `"http://app:80"` par `appServiceURL(c)` pour `APP_URL`

### `internal/controller/ai_enrichment.go`

8. **`aiTestJob`** (ligne ~603) — remplacer ligne 612 :
   Remplacer :
   ```go
   job.Spec.Template.Spec.Containers[0].Env = append(job.Spec.Template.Spec.Containers[0].Env, corev1.EnvVar{Name: "APP_URL", Value: defaultAIInternalAppURL})
   ```
   Par :
   ```go
   job.Spec.Template.Spec.Containers[0].Env = append(job.Spec.Template.Spec.Containers[0].Env, corev1.EnvVar{Name: "APP_URL", Value: appServiceURL(c)})
   ```

### Git — conflit README.md à résoudre

Le fichier `README.md` a encore des markers de conflit (`<<<<<<<`, `=======`, `>>>>>>>`) issus du merge précédent. Conflits restants aux lignes approx :
- Lignes ~931–969 : section "Global AI prompt via Helm" → garder la version remote (plus détaillée)
- Lignes ~1307–1714 : grande section (Lifecycle + Status fields + Copilot Extension setup) → garder la version remote + ajouter le heading `### Full-stack with GitHub integration, tests, and AI enrichment`
- Lignes ~1834–1840 : commentaire `ai.systemPrompt` → garder la version remote
- Lignes ~2038–2109 : tableau CI pipelines + section Architecture → garder la version remote

Après résolution : `git add README.md && git commit -m "fix: resolve merge conflicts"`

### Étape finale

9. `go build ./...` — vérifier que ça compile
10. Commiter tout : `git add -A && git commit -m "feat: multi-service support (frontend + backend + database)"`
11. `git push origin main` ou créer une branche `feat/multi-service`

## Notes architecture

- Deployments nommés `svc-<name>` (ex: `svc-frontend`, `svc-backend`)
- Services K8s nommés identiquement : `svc-frontend`, `svc-backend`
- Ingress : paths triés par longueur décroissante (ex: `/api` avant `/`)
- Sans `nginx.ingress.kubernetes.io/rewrite-target` en mode multi-service
- Les credentials DB sont injectés dans **tous** les services (frontend peut les ignorer)
- `appServiceURL(c)` retourne `http://svc-<premier-service>:<port>` en mode multi
- `mainAppImage(c)` retourne l'image du premier service en mode multi
