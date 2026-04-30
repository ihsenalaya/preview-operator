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

	platformv1alpha1 "github.com/company/cellenza-operator/api/v1alpha1"
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

func (r *CellenzaReconciler) collectDiagnostics(ctx context.Context, c *platformv1alpha1.Cellenza, reason string, reconcileErr error) *platformv1alpha1.DiagnosticsStatus {
	nsName := c.Status.NamespaceName
	if nsName == "" {
		nsName = r.namespaceName(c)
	}

	diag := &platformv1alpha1.DiagnosticsStatus{
		Reason:    reason,
		Component: diagnosticComponent(reason),
		Message:   reconcileErr.Error(),
		DebugCommands: []string{
			fmt.Sprintf("kubectl describe cellenza %s", c.Name),
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
	return diag
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

func (r *CellenzaReconciler) databaseJobDiagnostic(ctx context.Context, nsName, jobName string) string {
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

func (r *CellenzaReconciler) deploymentDiagnostic(ctx context.Context, nsName, name string) string {
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
		return app == "cellenza-preview"
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

func (r *CellenzaReconciler) warningEvents(ctx context.Context, nsName string, limit int) []string {
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

func (r *CellenzaReconciler) podLogs(ctx context.Context, nsName string, lines int) []string {
	if r.KubeClient == nil {
		return nil
	}
	pods := &corev1.PodList{}
	if err := r.List(ctx, pods, client.InNamespace(nsName), client.MatchingLabels{"app": "cellenza-preview"}); err != nil {
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

func (r *CellenzaReconciler) fetchPodLogs(ctx context.Context, nsName, podName, container string, lines int) []string {
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
