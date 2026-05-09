package extension

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	platformv1alpha1 "github.com/ihsenalaya/preview-operator/api/v1alpha1"
)

// parsePRArg extracts the preview name from an arg like "pr-42", "42", or "#42".
func parsePRArg(args []string) string {
	if len(args) == 0 {
		return ""
	}
	s := strings.TrimPrefix(args[0], "#")
	if !strings.HasPrefix(s, "pr-") {
		s = "pr-" + s
	}
	return s
}

func (s *Server) getPreview(ctx context.Context, name string) (*platformv1alpha1.Preview, error) {
	cz := &platformv1alpha1.Preview{}
	if err := s.crClient.Get(ctx, types.NamespacedName{Name: name}, cz); err != nil {
		return nil, err
	}
	return cz, nil
}

func (s *Server) cmdStatus(ctx context.Context, args []string) string {
	name := parsePRArg(args)
	if name == "" {
		return s.cmdList(ctx)
	}

	cz, err := s.getPreview(ctx, name)
	if err != nil {
		return fmt.Sprintf("Environnement `%s` introuvable.\n\nUtilise `@preview list` pour voir les environnements actifs.", name)
	}

	var b strings.Builder
	phase := string(cz.Status.Phase)
	icon := phaseIcon(cz.Status.Phase)

	b.WriteString(fmt.Sprintf("## %s %s — %s\n\n", icon, name, phase))

	if cz.Status.URL != "" {
		b.WriteString(fmt.Sprintf("**URL:** %s\n\n", cz.Status.URL))
	}

	b.WriteString(fmt.Sprintf("**Branch:** `%s`\n", cz.Spec.Branch))
	b.WriteString(fmt.Sprintf("**Tier:** `%s`\n", cz.Spec.ResourceTier))
	b.WriteString(fmt.Sprintf("**Replicas:** %d\n", cz.Spec.Replicas))

	if cz.Status.ReadyAt != nil {
		elapsed := time.Since(cz.Status.ReadyAt.Time).Round(time.Minute)
		b.WriteString(fmt.Sprintf("**Running depuis:** %s\n", formatDuration(elapsed)))
	}

	if cz.Status.ExpiresAt != nil {
		remaining := time.Until(cz.Status.ExpiresAt.Time).Round(time.Minute)
		if remaining > 0 {
			b.WriteString(fmt.Sprintf("**TTL:** expire dans %s\n", formatDuration(remaining)))
		} else {
			b.WriteString("**TTL:** expiré\n")
		}
	}

	if db := cz.Status.Database; db != nil {
		b.WriteString("\n**Base de données:**\n")
		b.WriteString(fmt.Sprintf("- PostgreSQL: %s\n", readyStr(db.Ready)))
		if db.Migration != "" {
			b.WriteString(fmt.Sprintf("- Migration: %s\n", db.Migration))
		}
		if db.Seed != "" {
			b.WriteString(fmt.Sprintf("- Seed: %s\n", db.Seed))
		}
		if len(db.Checkpoints) > 0 {
			b.WriteString(fmt.Sprintf("- Checkpoints: %s\n", strings.Join(db.Checkpoints, ", ")))
		}
	}

	if gh := cz.Status.GitHub; gh != nil && gh.DeploymentState != "" {
		b.WriteString(fmt.Sprintf("\n**GitHub Deployment:** `%s`\n", gh.DeploymentState))
	}

	if aiEnabled(cz) || cz.Status.AIEnrichment != nil {
		b.WriteString("\n**IA:**\n")
		if ai := cz.Status.AIEnrichment; ai != nil {
			b.WriteString(fmt.Sprintf("- Phase: %s\n", defaultAIStatus(ai.Phase, "Pending")))
			if ai.RerunOnly {
				b.WriteString("- Mode: AI-only rerun (testSuite standard suspendu)\n")
			}
			if aiSeedTaskEnabled(cz) {
				b.WriteString(fmt.Sprintf("- Seed: %s\n", defaultAIStatus(ai.SeedStatus, "Pending")))
			}
			if aiTestsTaskEnabled(cz) {
				b.WriteString(fmt.Sprintf("- Tests: %s\n", defaultAIStatus(ai.TestsStatus, "Pending")))
			}
			for _, line := range ai.TestResults {
				b.WriteString(fmt.Sprintf("- %s\n", line))
			}
			if ai.Error != "" {
				b.WriteString(fmt.Sprintf("- Erreur: %s\n", ai.Error))
			}
		} else {
			b.WriteString("- Phase: Pending\n")
		}
	}

	if cz.Status.Phase == platformv1alpha1.PhaseFailed && cz.Status.Diagnostics != nil {
		diag := cz.Status.Diagnostics
		b.WriteString(fmt.Sprintf("\n**Erreur:** %s\n", diag.Message))
		if len(diag.PodLogs) > 0 {
			b.WriteString("\n**Derniers logs:**\n```\n")
			b.WriteString(strings.Join(diag.PodLogs, "\n"))
			b.WriteString("\n```\n")
		}
	}

	b.WriteString(fmt.Sprintf("\n---\n`@preview logs %s` · `@preview extend %s` · `@preview reset-db %s` · `@preview retest-ai %s` · `@preview save-db %s post-seed` · `@preview restore-db %s post-seed`", name, name, name, name, name, name))
	return b.String()
}

func (s *Server) cmdLogs(ctx context.Context, args []string) string {
	name := parsePRArg(args)
	if name == "" {
		return "Usage: `@preview logs pr-<N>`"
	}

	cz, err := s.getPreview(ctx, name)
	if err != nil {
		return fmt.Sprintf("Environnement `%s` introuvable.", name)
	}

	nsName := cz.Status.NamespaceName
	if nsName == "" {
		nsName = fmt.Sprintf("preview-pr-%d", cz.Spec.PRNumber)
	}

	logs := s.fetchLogs(ctx, nsName, 40)
	if len(logs) == 0 {
		if cz.Status.Diagnostics != nil && len(cz.Status.Diagnostics.PodLogs) > 0 {
			logs = cz.Status.Diagnostics.PodLogs
		}
	}

	if len(logs) == 0 {
		return fmt.Sprintf("Aucun log disponible pour `%s` (phase: `%s`).\n\nEssaie:\n```bash\nkubectl logs -n %s -l app=preview-preview --tail=50\n```",
			name, cz.Status.Phase, nsName)
	}

	return fmt.Sprintf("**Logs — %s** (namespace: `%s`)\n\n```\n%s\n```",
		name, nsName, strings.Join(logs, "\n"))
}

func (s *Server) fetchLogs(ctx context.Context, nsName string, lines int) []string {
	if s.kubeClient == nil {
		return nil
	}
	pods := &corev1.PodList{}
	if err := s.crClient.List(ctx, pods, client.InNamespace(nsName), client.MatchingLabels{"app": "preview-preview"}); err != nil {
		return nil
	}
	for _, pod := range pods.Items {
		tailLines := int64(lines)
		req := s.kubeClient.CoreV1().Pods(nsName).GetLogs(pod.Name, &corev1.PodLogOptions{
			Container: "app",
			TailLines: &tailLines,
		})
		stream, err := req.Stream(ctx)
		if err != nil {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(stream, 16384))
		_ = stream.Close()
		if err == nil && len(data) > 0 {
			return strings.Split(strings.TrimSpace(string(data)), "\n")
		}
	}
	return nil
}

func (s *Server) cmdExtend(ctx context.Context, args []string) string {
	name := parsePRArg(args)
	if name == "" {
		return "Usage: `@preview extend pr-<N> [24h]`"
	}

	duration := "24h"
	for _, a := range args[1:] {
		if strings.HasSuffix(a, "h") || strings.HasSuffix(a, "m") {
			duration = a
		}
	}

	d, err := time.ParseDuration(duration)
	if err != nil {
		return fmt.Sprintf("Durée invalide: `%s`. Exemples: `24h`, `48h`, `120m`.", duration)
	}

	cz, err := s.getPreview(ctx, name)
	if err != nil {
		return fmt.Sprintf("Environnement `%s` introuvable.", name)
	}

	// Patch spec.ttl so the controller knows the new TTL on next reconcile
	specBase := client.MergeFrom(cz.DeepCopy())
	cz.Spec.TTL = duration
	if err := s.crClient.Patch(ctx, cz, specBase); err != nil {
		return fmt.Sprintf("Erreur lors de l'extension (spec): %v", err)
	}

	// Compute new expiry and patch status.expiresAt directly so effect is immediate
	base := time.Now()
	if cz.Status.ExpiresAt != nil {
		base = cz.Status.ExpiresAt.Time
	}
	newExpiry := metav1.NewTime(base.Add(d))

	statusBase := client.MergeFrom(cz.DeepCopy())
	cz.Status.ExpiresAt = &newExpiry
	if err := s.crClient.Status().Patch(ctx, cz, statusBase); err != nil {
		return fmt.Sprintf("Erreur lors de l'extension (status): %v", err)
	}

	return fmt.Sprintf("**TTL étendu** pour `%s`\n\n- Durée ajoutée: **%s**\n- Nouvelle expiration: `%s`\n- Phase actuelle: `%s`",
		name, duration, newExpiry.Format("2006-01-02 15:04 UTC"), cz.Status.Phase)
}

func (s *Server) cmdWake(ctx context.Context, args []string) string {
	name := parsePRArg(args)
	if name == "" {
		return "Usage: `@preview wake pr-<N>`"
	}

	cz, err := s.getPreview(ctx, name)
	if err != nil {
		return fmt.Sprintf("Environnement `%s` introuvable.", name)
	}

	if cz.Spec.Replicas > 0 {
		return fmt.Sprintf("`%s` est déjà actif (replicas: %d, phase: `%s`).", name, cz.Spec.Replicas, cz.Status.Phase)
	}

	patch := client.MergeFrom(cz.DeepCopy())
	cz.Spec.Replicas = 1
	if err := s.crClient.Patch(ctx, cz, patch); err != nil {
		return fmt.Sprintf("Erreur lors du réveil: %v", err)
	}

	return fmt.Sprintf("**Réveil lancé** pour `%s`\n\nReplicas: 0 → 1\nL'opérateur va redémarrer le pod. URL disponible dans ~30s: %s",
		name, cz.Status.URL)
}

func (s *Server) cmdResetDB(ctx context.Context, args []string) string {
	name := parsePRArg(args)
	if name == "" {
		return "Usage: `@preview reset-db pr-<N>`"
	}

	cz, err := s.getPreview(ctx, name)
	if err != nil {
		return fmt.Sprintf("Environnement `%s` introuvable.", name)
	}

	if cz.Spec.Database == nil || !cz.Spec.Database.Enabled {
		return fmt.Sprintf("`%s` n'a pas de base de données activée (`spec.database.enabled: false`).", name)
	}

	patch := client.MergeFrom(cz.DeepCopy())
	cz.Spec.Database.ResetRequested = true
	if err := s.crClient.Patch(ctx, cz, patch); err != nil {
		return fmt.Sprintf("Erreur lors du reset: %v", err)
	}

	return fmt.Sprintf("**Reset DB lancé** pour `%s`\n\nL'opérateur va:\n1. Supprimer les jobs migration et seed\n2. Recréer la base de données\n3. Rejouer les migrations\n4. Rejouer le seed\n\nSuivi: `@preview status %s`", name, name)
}

func (s *Server) cmdSaveDB(ctx context.Context, args []string) string {
	name, checkpoint, err := checkpointCommandArgs(args)
	if err != nil {
		return err.Error()
	}

	cz, err := s.getPreview(ctx, name)
	if err != nil {
		return fmt.Sprintf("Environnement `%s` introuvable.", name)
	}
	if cz.Spec.Database == nil || !cz.Spec.Database.Enabled {
		return fmt.Sprintf("`%s` n'a pas de base de données activée (`spec.database.enabled: false`).", name)
	}
	if err := s.startCheckpointSave(ctx, cz, checkpoint); err != nil {
		return fmt.Sprintf("Erreur lors de la sauvegarde du checkpoint: %v", err)
	}

	return fmt.Sprintf("**Checkpoint DB lancé** pour `%s`\n\n- Action: save\n- Checkpoint: `%s`\n\nSuivi: `@preview list-checkpoints %s`", name, checkpoint, name)
}

func (s *Server) cmdRestoreDB(ctx context.Context, args []string) string {
	name, checkpoint, err := checkpointCommandArgs(args)
	if err != nil {
		return err.Error()
	}

	cz, err := s.getPreview(ctx, name)
	if err != nil {
		return fmt.Sprintf("Environnement `%s` introuvable.", name)
	}
	if cz.Spec.Database == nil || !cz.Spec.Database.Enabled {
		return fmt.Sprintf("`%s` n'a pas de base de données activée (`spec.database.enabled: false`).", name)
	}
	if err := s.startCheckpointRestore(ctx, cz, checkpoint); err != nil {
		return fmt.Sprintf("Erreur lors de la restauration du checkpoint: %v", err)
	}

	return fmt.Sprintf("**Restauration DB lancée** pour `%s`\n\n- Action: restore\n- Checkpoint: `%s`\n\nSuivi: `@preview status %s`", name, checkpoint, name)
}

func (s *Server) cmdListCheckpoints(ctx context.Context, args []string) string {
	name := parsePRArg(args)
	if name == "" {
		return "Usage: `@preview list-checkpoints pr-<N>`"
	}

	cz, err := s.getPreview(ctx, name)
	if err != nil {
		return fmt.Sprintf("Environnement `%s` introuvable.", name)
	}
	if cz.Spec.Database == nil || !cz.Spec.Database.Enabled {
		return fmt.Sprintf("`%s` n'a pas de base de données activée (`spec.database.enabled: false`).", name)
	}

	checkpoints := checkpointNames(cz)
	if len(checkpoints) == 0 {
		return fmt.Sprintf("Aucun checkpoint enregistré pour `%s`.", name)
	}
	return fmt.Sprintf("**Checkpoints DB — %s**\n\n- %s", name, strings.Join(checkpoints, "\n- "))
}

func checkpointCommandArgs(args []string) (string, string, error) {
	name := parsePRArg(args)
	if name == "" || len(args) < 2 {
		return "", "", fmt.Errorf("Usage: `@preview save-db pr-<N> <nom>` ou `@preview restore-db pr-<N> <nom>`")
	}
	return name, args[1], nil
}

func checkpointNames(cz *platformv1alpha1.Preview) []string {
	if cz.Status.Database == nil {
		return nil
	}
	return cz.Status.Database.Checkpoints
}

func checkpointExists(cz *platformv1alpha1.Preview, checkpoint string) bool {
	for _, name := range checkpointNames(cz) {
		if name == checkpoint {
			return true
		}
	}
	return false
}

const (
	aiPromptNamespace = "preview-operator-system"
	aiPromptKey       = "instructions"
)

func (s *Server) cmdSetPrompt(ctx context.Context, args []string) string {
	name := parsePRArg(args)
	if name == "" || len(args) < 2 {
		return "Usage: `@preview set-prompt pr-<N> <instructions>`\n\nExample: `@preview set-prompt pr-42 Ne génère pas de tests pour les endpoints HTML`"
	}

	if _, err := s.getPreview(ctx, name); err != nil {
		return fmt.Sprintf("Environnement `%s` introuvable.\n\nUtilise `@preview list` pour voir les environnements actifs.", name)
	}

	instructions := strings.Join(args[1:], " ")
	cmName := "ai-prompt-" + name

	existing := &corev1.ConfigMap{}
	err := s.crClient.Get(ctx, types.NamespacedName{Name: cmName, Namespace: aiPromptNamespace}, existing)
	if errors.IsNotFound(err) {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cmName,
				Namespace: aiPromptNamespace,
				Labels: map[string]string{
					"app.kubernetes.io/managed-by":      "preview-operator",
					"platform.company.io/preview-name": name,
					"app.kubernetes.io/component":       "ai-prompt",
				},
			},
			Data: map[string]string{aiPromptKey: instructions},
		}
		if err := s.crClient.Create(ctx, cm); err != nil {
			return fmt.Sprintf("Erreur lors de la création du prompt: %v", err)
		}
	} else if err != nil {
		return fmt.Sprintf("Erreur: %v", err)
	} else {
		patch := client.MergeFrom(existing.DeepCopy())
		existing.Data = map[string]string{aiPromptKey: instructions}
		if err := s.crClient.Patch(ctx, existing, patch); err != nil {
			return fmt.Sprintf("Erreur lors de la mise à jour du prompt: %v", err)
		}
	}

	return fmt.Sprintf("**Prompt IA mis à jour** pour `%s`\n\nInstructions enregistrées:\n```\n%s\n```\n\nLance `@preview retest-ai %s` pour régénérer avec le nouveau prompt.", name, instructions, name)
}

func (s *Server) cmdShowPrompt(ctx context.Context, args []string) string {
	name := parsePRArg(args)
	if name == "" {
		return "Usage: `@preview show-prompt pr-<N>`"
	}

	cmName := "ai-prompt-" + name
	cm := &corev1.ConfigMap{}
	err := s.crClient.Get(ctx, types.NamespacedName{Name: cmName, Namespace: aiPromptNamespace}, cm)
	if errors.IsNotFound(err) {
		return fmt.Sprintf("Aucun prompt personnalisé pour `%s`.\n\nUtilise `@preview set-prompt %s <instructions>` pour en définir un.", name, name)
	}
	if err != nil {
		return fmt.Sprintf("Erreur: %v", err)
	}

	instructions := cm.Data[aiPromptKey]
	if instructions == "" {
		return fmt.Sprintf("Le prompt de `%s` est vide.", name)
	}
	return fmt.Sprintf("**Prompt IA — %s**\n\n```\n%s\n```", name, instructions)
}

func (s *Server) cmdRunSQL(ctx context.Context, args []string) string {
	name := parsePRArg(args)
	if name == "" || len(args) < 2 {
		return "Usage: `@preview run-sql pr-<N> <sql>`\n\nExample: `@preview run-sql pr-42 SELECT COUNT(*) FROM products;`"
	}

	cz, err := s.getPreview(ctx, name)
	if err != nil {
		return fmt.Sprintf("Environnement `%s` introuvable.", name)
	}
	if cz.Spec.Database == nil || !cz.Spec.Database.Enabled {
		return fmt.Sprintf("`%s` n'a pas de base de données activée.", name)
	}

	nsName := cz.Status.NamespaceName
	if nsName == "" {
		nsName = fmt.Sprintf("preview-pr-%d", cz.Spec.PRNumber)
	}

	sql := strings.Join(args[1:], " ")
	jobName := fmt.Sprintf("run-sql-%d", time.Now().Unix())
	backoffLimit := int32(0)
	ttl := int32(120)

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: nsName,
			Labels:    map[string]string{"app.kubernetes.io/managed-by": "preview-extension"},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &ttl,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{{
						Name:    "psql",
						Image:   "postgres:15-alpine",
						Command: []string{"psql", "-c", sql},
						Env: []corev1.EnvVar{
							{Name: "PGHOST", Value: "postgres"},
							{Name: "PGPORT", Value: "5432"},
							{
								Name: "PGUSER",
								ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "postgres-credentials"},
									Key:                  "POSTGRES_USER",
								}},
							},
							{
								Name: "PGPASSWORD",
								ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "postgres-credentials"},
									Key:                  "POSTGRES_PASSWORD",
								}},
							},
							{
								Name: "PGDATABASE",
								ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "postgres-credentials"},
									Key:                  "POSTGRES_DB",
								}},
							},
						},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    *mustParseQuantity("50m"),
								corev1.ResourceMemory: *mustParseQuantity("64Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    *mustParseQuantity("200m"),
								corev1.ResourceMemory: *mustParseQuantity("128Mi"),
							},
						},
					}},
				},
			},
		},
	}

	if err := s.crClient.Create(ctx, job); err != nil {
		return fmt.Sprintf("Erreur lors de la création du job: %v", err)
	}

	return fmt.Sprintf("**SQL lancé** sur `%s`\n\n```sql\n%s\n```\n\nJob: `%s/%s`\n\nRésultat dans ~5s:\n```bash\nkubectl logs -n %s job/%s\n```",
		name, sql, nsName, jobName, nsName, jobName)
}

func mustParseQuantity(s string) *resource.Quantity {
	q := resource.MustParse(s)
	return &q
}

func (s *Server) cmdRetestAI(ctx context.Context, args []string) string {
	name := parsePRArg(args)
	if name == "" {
		return "Usage: `@preview retest-ai pr-<N>`"
	}

	cz, err := s.getPreview(ctx, name)
	if err != nil {
		return fmt.Sprintf("Environnement `%s` introuvable.", name)
	}
	if !aiEnabled(cz) {
		return fmt.Sprintf("`%s` n'a pas l'enrichissement IA activé (`spec.aiEnrichment.enabled: false`).", name)
	}
	if cz.Spec.AIEnrichment.RerunRequested || (cz.Status.AIEnrichment != nil && cz.Status.AIEnrichment.RerunOnly) {
		return fmt.Sprintf("Un rerun IA est déjà en cours pour `%s`.\n\nSuivi: `@preview status %s`", name, name)
	}

	patch := client.MergeFrom(cz.DeepCopy())
	cz.Spec.AIEnrichment.RerunRequested = true
	if err := s.crClient.Patch(ctx, cz, patch); err != nil {
		return fmt.Sprintf("Erreur lors du rerun IA: %v", err)
	}

	var steps []string
	if cz.Spec.Database != nil && cz.Spec.Database.Enabled {
		steps = append(steps, "1. Rejouer migration + seed de la base")
		steps = append(steps, "2. Ignorer le test suite standard pour ce cycle")
		steps = append(steps, "3. Régénérer `seed.sql` et `test.py`")
		steps = append(steps, "4. Rejouer `ai-seed` puis `ai-tests`")
	} else {
		steps = append(steps, "1. Ignorer le test suite standard pour ce cycle")
		steps = append(steps, "2. Régénérer `seed.sql` et `test.py`")
		steps = append(steps, "3. Rejouer `ai-seed` puis `ai-tests`")
	}

	return fmt.Sprintf("**Rerun IA lancé** pour `%s`\n\nL'opérateur va:\n%s\n\nSuivi: `@preview status %s`", name, strings.Join(steps, "\n"), name)
}

func (s *Server) cmdEnrich(ctx context.Context, args []string) string {
	return s.cmdRetestAI(ctx, args)
}

func (s *Server) cmdList(ctx context.Context) string {
	list := &platformv1alpha1.PreviewList{}
	if err := s.crClient.List(ctx, list); err != nil {
		return fmt.Sprintf("Erreur lors de la liste: %v", err)
	}

	if len(list.Items) == 0 {
		return "Aucun environnement preview actif."
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("**Environnements actifs — %d**\n\n", len(list.Items)))
	b.WriteString("| Nom | Branch | Phase | TTL restant |\n")
	b.WriteString("|---|---|---|---|\n")

	for _, cz := range list.Items {
		ttl := "—"
		if cz.Status.ExpiresAt != nil {
			remaining := time.Until(cz.Status.ExpiresAt.Time).Round(time.Minute)
			if remaining > 0 {
				ttl = formatDuration(remaining)
			} else {
				ttl = "expiré"
			}
		}
		b.WriteString(fmt.Sprintf("| `%s` | `%s` | %s `%s` | %s |\n",
			cz.Name,
			cz.Spec.Branch,
			phaseIcon(cz.Status.Phase),
			cz.Status.Phase,
			ttl,
		))
	}

	b.WriteString("\n`@preview status pr-<N>` pour les détails d'un environnement.")
	return b.String()
}

func cmdHelp() string { //nolint:misspell
	return `**Preview Extension — Commandes disponibles**

| Commande | Description |
|---|---|
| ` + "`@preview list`" + ` | Liste tous les environnements actifs |
| ` + "`@preview status pr-42`" + ` | État détaillé d'un environnement |
| ` + "`@preview logs pr-42`" + ` | Derniers logs du pod applicatif |
| ` + "`@preview extend pr-42 [24h]`" + ` | Prolonge le TTL |
| ` + "`@preview wake pr-42`" + ` | Redémarre un environnement mis en veille |
| ` + "`@preview reset-db pr-42`" + ` | Recrée la base de données + rejoue seed |
| ` + "`@preview save-db pr-42 post-seed`" + ` | Sauvegarde un checkpoint DB sous ce nom |
| ` + "`@preview restore-db pr-42 post-seed`" + ` | Restaure la DB depuis un checkpoint |
| ` + "`@preview list-checkpoints pr-42`" + ` | Liste les checkpoints DB disponibles |
| ` + "`@preview run-sql pr-42 <sql>`" + ` | Exécute du SQL arbitraire sur la base de données |
| ` + "`@preview retest-ai pr-42`" + ` | Relance un cycle IA-only géré par l'opérateur |
| ` + "`@preview enrich pr-42`" + ` | Alias rétrocompatible de ` + "`retest-ai`" + ` |
| ` + "`@preview set-prompt pr-42 <instructions>`" + ` | Définit les instructions IA pour cet environnement |
| ` + "`@preview show-prompt pr-42`" + ` | Affiche le prompt IA actuel |
| ` + "`@preview help`" + ` | Affiche cette aide |`
}

func phaseIcon(phase platformv1alpha1.EnvironmentPhase) string {
	switch phase {
	case platformv1alpha1.PhaseRunning:
		return "✅"
	case platformv1alpha1.PhaseProvisioning:
		return "🔄"
	case platformv1alpha1.PhasePending:
		return "⏳"
	case platformv1alpha1.PhaseFailed:
		return "❌"
	case platformv1alpha1.PhaseTerminating:
		return "🗑️"
	default:
		return "❓"
	}
}

func readyStr(ready bool) string {
	if ready {
		return "✅ Ready"
	}
	return "⏳ Not ready"
}

func formatDuration(d time.Duration) string {
	d = d.Round(time.Minute)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h > 0 && m > 0 {
		return fmt.Sprintf("%dh%dm", h, m)
	}
	if h > 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dm", m)
}

func aiEnabled(cz *platformv1alpha1.Preview) bool {
	return cz.Spec.AIEnrichment != nil && cz.Spec.AIEnrichment.Enabled
}

func aiSeedTaskEnabled(cz *platformv1alpha1.Preview) bool {
	if !aiEnabled(cz) {
		return false
	}
	if cz.Spec.AIEnrichment.Seed == nil {
		return true
	}
	return cz.Spec.AIEnrichment.Seed.Enabled
}

func aiTestsTaskEnabled(cz *platformv1alpha1.Preview) bool {
	if !aiEnabled(cz) {
		return false
	}
	if cz.Spec.AIEnrichment.Tests == nil {
		return true
	}
	return cz.Spec.AIEnrichment.Tests.Enabled
}

func defaultAIStatus(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
