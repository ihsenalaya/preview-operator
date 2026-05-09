package controller

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	platformv1alpha1 "github.com/ihsenalaya/preview-operator/api/v1alpha1"
)

const (
	componentApp       = "app"
	componentDatabase  = "database"
	componentIngress   = "ingress"
	componentMigration = "migration"
	componentNamespace = "namespace"
	componentPreview   = "preview"
	componentQuota     = "quota"
	componentSeed      = "seed"
	componentService   = "service"
)

const (
	diagnosticConfidenceHigh   = "high"
	diagnosticConfidenceMedium = "medium"
	diagnosticConfidenceLow    = "low"
)

func (r *PreviewReconciler) collectDiagnostics(ctx context.Context, c *platformv1alpha1.Preview, reason string, reconcileErr error) *platformv1alpha1.DiagnosticsStatus {
	nsName := c.Status.NamespaceName
	if nsName == "" {
		nsName = r.namespaceName(c)
	}

	diag := &platformv1alpha1.DiagnosticsStatus{
		Reason:    reason,
		Component: diagnosticComponent(reason),
		Message:   reconcileErr.Error(),
		DebugCommands: []string{
			fmt.Sprintf("kubectl describe preview %s", c.Name),
			fmt.Sprintf("kubectl get pods -n %s", nsName),
			fmt.Sprintf("kubectl get events -n %s --sort-by=.lastTimestamp", nsName),
		},
	}

	if message := r.databaseJobDiagnostic(ctx, nsName, migrationJobName); message != "" {
		diag.Component = componentMigration
		diag.Message = message
		diag.DebugCommands = append(diag.DebugCommands, fmt.Sprintf("kubectl logs -n %s job/%s", nsName, migrationJobName))
	} else if message := r.databaseJobDiagnostic(ctx, nsName, seedJobName); message != "" {
		diag.Component = componentSeed
		diag.Message = message
		diag.DebugCommands = append(diag.DebugCommands, fmt.Sprintf("kubectl logs -n %s job/%s", nsName, seedJobName))
	} else if message := r.deploymentDiagnostic(ctx, nsName, componentApp); message != "" {
		diag.Component = componentApp
		diag.Message = message
		diag.PodLogs = r.podLogs(ctx, nsName, 30)
		diag.DebugCommands = append(diag.DebugCommands, fmt.Sprintf("kubectl describe deployment app -n %s", nsName))
	} else if message := r.deploymentDiagnostic(ctx, nsName, "postgres"); message != "" {
		diag.Component = componentDatabase
		diag.Message = message
		diag.DebugCommands = append(diag.DebugCommands, fmt.Sprintf("kubectl describe deployment postgres -n %s", nsName))
	}

	diag.LastEvents = r.warningEvents(ctx, nsName, 3)
	r.enrichDiagnostics(ctx, c, nsName, diag)
	return diag
}

func (r *PreviewReconciler) enrichDiagnostics(ctx context.Context, c *platformv1alpha1.Preview, nsName string, diag *platformv1alpha1.DiagnosticsStatus) {
	diag.SignificantLogs = r.significantLogExcerpts(ctx, nsName)
	diag.RootCause, diag.Confidence = inferRootCause(diag)
	diag.Recommendations = diagnosticRecommendations(c, diag)
}

func diagnosticComponent(reason string) string {
	switch {
	case strings.Contains(reason, "Migration"):
		return componentMigration
	case strings.Contains(reason, "Seed"):
		return componentSeed
	case strings.Contains(reason, "Database"):
		return componentDatabase
	case strings.Contains(reason, "Deployment"):
		return componentApp
	case strings.Contains(reason, "Ingress"):
		return componentIngress
	case strings.Contains(reason, "Service"):
		return componentService
	case strings.Contains(reason, "Quota"):
		return componentQuota
	case strings.Contains(reason, "Namespace"):
		return componentNamespace
	default:
		return componentPreview
	}
}

func (r *PreviewReconciler) databaseJobDiagnostic(ctx context.Context, nsName, jobName string) string {
	job := &batchv1.Job{}
	if err := r.Get(ctx, types.NamespacedName{Name: jobName, Namespace: nsName}, job); err != nil {
		return ""
	}
	for _, cond := range job.Status.Conditions {
		if cond.Type == batchv1.JobFailed && cond.Status == corev1.ConditionTrue {
			if cond.Message != "" {
				return fmt.Sprintf("Job %s failed: %s", jobName, cond.Message)
			}
			return fmt.Sprintf("Job %s failed", jobName)
		}
	}
	return ""
}

func (r *PreviewReconciler) deploymentDiagnostic(ctx context.Context, nsName, name string) string {
	deploy := &appsv1.Deployment{}
	if err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: nsName}, deploy); err != nil {
		return ""
	}
	for _, cond := range deploy.Status.Conditions {
		if cond.Type == appsv1.DeploymentProgressing && cond.Status == corev1.ConditionFalse {
			if cond.Message != "" {
				return fmt.Sprintf("Deployment %s is not progressing: %s", name, cond.Message)
			}
			return fmt.Sprintf("Deployment %s is not progressing", name)
		}
		if cond.Type == appsv1.DeploymentAvailable && cond.Status == corev1.ConditionFalse && cond.Message != "" {
			return fmt.Sprintf("Deployment %s is unavailable: %s", name, cond.Message)
		}
	}

	pods := &corev1.PodList{}
	if err := r.List(ctx, pods, client.InNamespace(nsName)); err != nil {
		return ""
	}
	for _, pod := range pods.Items {
		if !podMatchesDeployment(pod, name) {
			continue
		}
		if message := podDiagnosticMessage(pod); message != "" {
			return message
		}
	}
	return ""
}

func podMatchesDeployment(pod corev1.Pod, deploymentName string) bool {
	app := pod.Labels["app"]
	if deploymentName == componentApp {
		return app == "preview-preview"
	}
	return app == deploymentName
}

func podDiagnosticMessage(pod corev1.Pod) string {
	for _, status := range pod.Status.ContainerStatuses {
		if status.State.Waiting != nil {
			return fmt.Sprintf("Pod %s container %s is waiting: %s %s",
				pod.Name,
				status.Name,
				status.State.Waiting.Reason,
				status.State.Waiting.Message,
			)
		}
		if status.State.Terminated != nil && status.State.Terminated.ExitCode != 0 {
			return fmt.Sprintf("Pod %s container %s terminated with exit code %d: %s",
				pod.Name,
				status.Name,
				status.State.Terminated.ExitCode,
				status.State.Terminated.Message,
			)
		}
	}
	return ""
}

func (r *PreviewReconciler) warningEvents(ctx context.Context, nsName string, limit int) []string {
	events := &corev1.EventList{}
	if err := r.List(ctx, events, client.InNamespace(nsName)); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return nil
	}

	sort.Slice(events.Items, func(i, j int) bool {
		return events.Items[i].LastTimestamp.After(events.Items[j].LastTimestamp.Time)
	})

	var messages []string
	for _, event := range events.Items {
		if event.Type != corev1.EventTypeWarning {
			continue
		}
		messages = append(messages, fmt.Sprintf("%s/%s: %s", event.InvolvedObject.Kind, event.InvolvedObject.Name, event.Message))
		if len(messages) == limit {
			return messages
		}
	}
	return messages
}

func (r *PreviewReconciler) podLogs(ctx context.Context, nsName string, lines int) []string {
	if r.KubeClient == nil {
		return nil
	}
	pods := &corev1.PodList{}
	if err := r.List(ctx, pods, client.InNamespace(nsName), client.MatchingLabels{"app": "preview-preview"}); err != nil {
		return nil
	}
	for _, pod := range pods.Items {
		for _, cs := range pod.Status.ContainerStatuses {
			if cs.State.Terminated != nil || cs.State.Waiting != nil {
				return r.fetchPodLogs(ctx, nsName, pod.Name, "app", lines)
			}
		}
	}
	return nil
}

func (r *PreviewReconciler) significantLogExcerpts(ctx context.Context, nsName string) []platformv1alpha1.DiagnosticLogExcerpt {
	if r.KubeClient == nil {
		return nil
	}

	candidates := []struct {
		component string
		container string
		labels    client.MatchingLabels
	}{
		{component: componentMigration, container: componentMigration, labels: client.MatchingLabels{"platform.company.io/task": componentMigration}},
		{component: componentSeed, container: componentSeed, labels: client.MatchingLabels{"platform.company.io/task": componentSeed}},
		{component: componentApp, container: componentApp, labels: client.MatchingLabels{"app": "preview-preview"}},
		{component: componentDatabase, container: "postgres", labels: client.MatchingLabels{"app": "postgres"}},
	}

	var excerpts []platformv1alpha1.DiagnosticLogExcerpt
	for _, candidate := range candidates {
		podName := r.firstPodName(ctx, nsName, candidate.labels)
		if podName == "" {
			continue
		}
		lines := r.fetchPodLogs(ctx, nsName, podName, candidate.container, 60)
		significant := selectSignificantLines(lines, 6)
		if len(significant) == 0 {
			continue
		}
		excerpts = append(excerpts, platformv1alpha1.DiagnosticLogExcerpt{
			Component: candidate.component,
			Source:    fmt.Sprintf("pod/%s container/%s", podName, candidate.container),
			Lines:     significant,
		})
	}
	return excerpts
}

func (r *PreviewReconciler) firstPodName(ctx context.Context, nsName string, labels client.MatchingLabels) string {
	pods := &corev1.PodList{}
	if err := r.List(ctx, pods, client.InNamespace(nsName), labels); err != nil {
		return ""
	}
	sort.Slice(pods.Items, func(i, j int) bool {
		return pods.Items[i].CreationTimestamp.After(pods.Items[j].CreationTimestamp.Time)
	})
	if len(pods.Items) == 0 {
		return ""
	}
	return pods.Items[0].Name
}

func (r *PreviewReconciler) fetchPodLogs(ctx context.Context, nsName, podName, container string, lines int) []string {
	tailLines := int64(lines)
	req := r.KubeClient.CoreV1().Pods(nsName).GetLogs(podName, &corev1.PodLogOptions{
		Container: container,
		TailLines: &tailLines,
	})
	stream, err := req.Stream(ctx)
	if err != nil {
		return nil
	}
	defer func() { _ = stream.Close() }()
	data, err := io.ReadAll(io.LimitReader(stream, 8192))
	if err != nil || len(data) == 0 {
		return nil
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

func selectSignificantLines(lines []string, limit int) []string {
	keywords := []string{
		"error", "failed", "failure", "fatal", "panic", "exception", "traceback",
		"denied", "unauthorized", "forbidden", "timeout", "refused", "back-off",
		"backoff", "oom", "killed", "crash", "migration", "duplicate", "constraint",
		"does not exist", "already exists", "connection", "imagepull", "errimagepull",
		"syntaxerror", "nameerror", "typeerror", "valueerror", "importerror", "modulenotfounderror",
	}

	var selected []string
	seen := map[string]struct{}{}
	for i, line := range lines {
		normalized := strings.ToLower(line)
		for _, keyword := range keywords {
			if strings.Contains(normalized, keyword) {
				start := max(0, i-2)
				end := min(len(lines)-1, i+1)
				for j := start; j <= end; j++ {
					trimmed := truncateDiagnosticLine(lines[j])
					if trimmed == "" {
						continue
					}
					if _, ok := seen[trimmed]; ok {
						continue
					}
					selected = append(selected, trimmed)
					seen[trimmed] = struct{}{}
					if len(selected) == limit {
						return selected
					}
				}
				break
			}
		}
	}
	return selected
}

func inferRootCause(diag *platformv1alpha1.DiagnosticsStatus) (string, string) {
	text := strings.ToLower(diag.Message + "\n" + strings.Join(diag.LastEvents, "\n") + "\n" + diagnosticLogText(diag.SignificantLogs))

	switch {
	case containsAny(text, "imagepullbackoff", "errimagepull", "pull access denied", "manifest unknown"):
		return "Container image cannot be pulled", diagnosticConfidenceHigh
	case containsAny(text, "migration") && containsAny(text, "already exists", "duplicate", "constraint", "relation", "syntax error", "failed"):
		return "Database migration failed", diagnosticConfidenceHigh
	case containsAny(text, "seed") && containsAny(text, "duplicate", "constraint", "failed", "error"):
		return "Database seed failed", diagnosticConfidenceHigh
	case containsAny(text, "syntaxerror", "syntax error", "invalid syntax"):
		return "Application failed to start due to a syntax error", diagnosticConfidenceHigh
	case containsAny(text, "traceback", "nameerror", "typeerror", "valueerror", "importerror", "modulenotfounderror"):
		return "Application failed with an unhandled exception", diagnosticConfidenceMedium
	case containsAny(text, "connection refused", "could not connect", "timeout", "no route to host"):
		return "Application cannot reach a required dependency", diagnosticConfidenceMedium
	case containsAny(text, "readiness probe failed", "liveness probe failed", "crashloopbackoff", "back-off restarting"):
		return "Application pod is unhealthy", diagnosticConfidenceMedium
	case containsAny(text, "forbidden", "unauthorized", "permission denied", "denied"):
		return "Permission or authentication failure", diagnosticConfidenceMedium
	case diag.Component != "":
		return fmt.Sprintf("%s component failed during preview reconciliation", diag.Component), diagnosticConfidenceLow
	default:
		return "Preview reconciliation failed; inspect events and logs", diagnosticConfidenceLow
	}
}

func diagnosticRecommendations(c *platformv1alpha1.Preview, diag *platformv1alpha1.DiagnosticsStatus) []string {
	rootCause := strings.ToLower(diag.RootCause)
	switch {
	case strings.Contains(rootCause, "image"):
		return []string{
			fmt.Sprintf("Verify that image `%s` exists and is accessible from the cluster.", c.Spec.Image),
			"Check the image tag produced by CI for this pull request.",
			"Confirm the namespace has the required imagePullSecrets if the registry is private.",
		}
	case strings.Contains(rootCause, "migration"):
		return []string{
			"Check that the migration is idempotent and can run on a fresh preview database.",
			"Look for duplicate table/index creation or schema ordering issues in the highlighted logs.",
			fmt.Sprintf("After fixing the migration, request a DB reset with `kubectl patch preview %s --type=merge -p '{\"spec\":{\"database\":{\"resetRequested\":true}}}'`.", c.Name),
		}
	case strings.Contains(rootCause, "seed"):
		return []string{
			"Make the seed script idempotent with upserts or conflict handling.",
			"Check whether seed data assumes tables or migrations that did not complete.",
			fmt.Sprintf("After fixing the seed, request a DB reset with `kubectl patch preview %s --type=merge -p '{\"spec\":{\"database\":{\"resetRequested\":true}}}'`.", c.Name),
		}
	case strings.Contains(rootCause, "dependency"):
		return []string{
			"Verify service names, ports, and environment variables injected into the app pod.",
			"Check PostgreSQL readiness and credentials when the failure is database-related.",
			"Inspect recent Kubernetes warning events for networking or DNS symptoms.",
		}
	case strings.Contains(rootCause, "unhealthy"):
		return []string{
			"Inspect the highlighted app logs and the deployment description.",
			"Check readiness/liveness probe paths, startup time, and required environment variables.",
			"Rebuild the application image if the failure started after a code change.",
		}
	case strings.Contains(rootCause, "syntax error"):
		return []string{
			"Fix the syntax error reported in the highlighted app logs before rebuilding the image.",
			"Check the recent code change that triggered this preview for malformed Python, shell, or config syntax.",
			"Rebuild and push a fresh application image after the syntax issue is corrected.",
		}
	case strings.Contains(rootCause, "unhandled exception"):
		return []string{
			"Inspect the traceback in the highlighted app logs to identify the failing module or statement.",
			"Check application startup configuration, imports, and required environment variables.",
			"Rebuild the application image after fixing the startup exception.",
		}
	case strings.Contains(rootCause, "permission"):
		return []string{
			"Check service account permissions, registry credentials, and application secrets.",
			"Verify that required Kubernetes Secrets exist in the preview namespace.",
		}
	default:
		return []string{
			"Start with the highlighted logs and recent warning events in this comment.",
			fmt.Sprintf("Run `kubectl describe preview %s` to inspect the full operator status.", c.Name),
			"Check the debug commands below for component-level troubleshooting.",
		}
	}
}

func diagnosticLogText(excerpts []platformv1alpha1.DiagnosticLogExcerpt) string {
	var b strings.Builder
	for _, excerpt := range excerpts {
		b.WriteString(strings.Join(excerpt.Lines, "\n"))
		b.WriteByte('\n')
	}
	return b.String()
}

func containsAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func truncateDiagnosticLine(line string) string {
	line = strings.TrimSpace(line)
	if len(line) <= 240 {
		return line
	}
	return line[:237] + "..."
}
