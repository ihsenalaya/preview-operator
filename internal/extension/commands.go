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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	platformv1alpha1 "github.com/company/cellenza-operator/api/v1alpha1"
)

// parsePRArg extracts the cellenza name from an arg like "pr-42", "42", or "#42".
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

func (s *Server) getCellenza(ctx context.Context, name string) (*platformv1alpha1.Cellenza, error) {
	cz := &platformv1alpha1.Cellenza{}
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

	cz, err := s.getCellenza(ctx, name)
	if err != nil {
		return fmt.Sprintf("Environnement `%s` introuvable.\n\nUtilise `@cellenza list` pour voir les environnements actifs.", name)
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
	}

	if gh := cz.Status.GitHub; gh != nil && gh.DeploymentState != "" {
		b.WriteString(fmt.Sprintf("\n**GitHub Deployment:** `%s`\n", gh.DeploymentState))
	}

	if aiEnabled(cz) || cz.Status.AIEnrichment != nil {
		b.WriteString("\n**IA:**\n")
		if ai := cz.Status.AIEnrichment; ai != nil {
			b.WriteString(fmt.Sprintf("- Phase: %s\n", defaultAIStatus(ai.Phase, "Pending")))
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

	b.WriteString(fmt.Sprintf("\n---\n`@cellenza logs %s` · `@cellenza extend %s` · `@cellenza reset-db %s` · `@cellenza enrich %s`", name, name, name, name))
	return b.String()
}

func (s *Server) cmdLogs(ctx context.Context, args []string) string {
	name := parsePRArg(args)
	if name == "" {
		return "Usage: `@cellenza logs pr-<N>`"
	}

	cz, err := s.getCellenza(ctx, name)
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
		return fmt.Sprintf("Aucun log disponible pour `%s` (phase: `%s`).\n\nEssaie:\n```bash\nkubectl logs -n %s -l app=cellenza-preview --tail=50\n```",
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
	if err := s.crClient.List(ctx, pods, client.InNamespace(nsName), client.MatchingLabels{"app": "cellenza-preview"}); err != nil {
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
		return "Usage: `@cellenza extend pr-<N> [24h]`"
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

	cz, err := s.getCellenza(ctx, name)
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
		return "Usage: `@cellenza wake pr-<N>`"
	}

	cz, err := s.getCellenza(ctx, name)
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
		return "Usage: `@cellenza reset-db pr-<N>`"
	}

	cz, err := s.getCellenza(ctx, name)
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

	return fmt.Sprintf("**Reset DB lancé** pour `%s`\n\nL'opérateur va:\n1. Supprimer les jobs migration et seed\n2. Recréer la base de données\n3. Rejouer les migrations\n4. Rejouer le seed\n\nSuivi: `@cellenza status %s`", name, name)
}

func (s *Server) cmdEnrich(ctx context.Context, args []string) string {
	name := parsePRArg(args)
	if name == "" {
		return "Usage: `@cellenza enrich pr-<N>`"
	}

	cz, err := s.getCellenza(ctx, name)
	if err != nil {
		return fmt.Sprintf("Environnement `%s` introuvable.", name)
	}

	statusBase := client.MergeFrom(cz.DeepCopy())
	cz.Status.AIEnrichment = nil
	if err := s.crClient.Status().Patch(ctx, cz, statusBase); err != nil {
		return fmt.Sprintf("Erreur lors du reset IA (status): %v", err)
	}

	nsName := cz.Status.NamespaceName
	if nsName == "" {
		nsName = fmt.Sprintf("preview-pr-%d", cz.Spec.PRNumber)
	}

	toDelete := []client.Object{
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "ai-enrichment", Namespace: nsName}},
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "ai-seed", Namespace: nsName}},
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "ai-tests", Namespace: nsName}},
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "ai-schema-dump", Namespace: nsName}},
	}
	for _, obj := range toDelete {
		if err := s.crClient.Delete(ctx, obj); err != nil && !errors.IsNotFound(err) {
			return fmt.Sprintf("Erreur lors du nettoyage IA: %v", err)
		}
	}

	return fmt.Sprintf("**Enrichissement IA relancé** pour `%s`\n\nL'opérateur va:\n1. Relire le diff PR\n2. Régénérer le seed SQL\n3. Rejouer les tests\n\nSuivi: `@cellenza status %s`", name, name)
}

func (s *Server) cmdList(ctx context.Context) string {
	list := &platformv1alpha1.CellenzaList{}
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

	b.WriteString("\n`@cellenza status pr-<N>` pour les détails d'un environnement.")
	return b.String()
}

func cmdHelp() string { //nolint:misspell
	return `**Cellenza Extension — Commandes disponibles**

| Commande | Description |
|---|---|
| ` + "`@cellenza list`" + ` | Liste tous les environnements actifs |
| ` + "`@cellenza status pr-42`" + ` | État détaillé d'un environnement |
| ` + "`@cellenza logs pr-42`" + ` | Derniers logs du pod applicatif |
| ` + "`@cellenza extend pr-42 [24h]`" + ` | Prolonge le TTL |
| ` + "`@cellenza wake pr-42`" + ` | Redémarre un environnement mis en veille |
| ` + "`@cellenza reset-db pr-42`" + ` | Recrée la base de données + rejoue seed |
| ` + "`@cellenza enrich pr-42`" + ` | Relance la génération IA de seed et de tests |
| ` + "`@cellenza help`" + ` | Affiche cette aide |`
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

func aiEnabled(cz *platformv1alpha1.Cellenza) bool {
	return cz.Spec.AIEnrichment != nil && cz.Spec.AIEnrichment.Enabled
}

func aiSeedTaskEnabled(cz *platformv1alpha1.Cellenza) bool {
	if !aiEnabled(cz) {
		return false
	}
	if cz.Spec.AIEnrichment.Seed == nil {
		return true
	}
	return cz.Spec.AIEnrichment.Seed.Enabled
}

func aiTestsTaskEnabled(cz *platformv1alpha1.Cellenza) bool {
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
