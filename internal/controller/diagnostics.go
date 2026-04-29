package controller

import (
	"context"
	"fmt"
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
		diag.Component = "migration"
		diag.Message = message
		diag.DebugCommands = append(diag.DebugCommands, fmt.Sprintf("kubectl logs -n %s job/%s", nsName, migrationJobName))
	} else if message := r.databaseJobDiagnostic(ctx, nsName, seedJobName); message != "" {
		diag.Component = "seed"
		diag.Message = message
		diag.DebugCommands = append(diag.DebugCommands, fmt.Sprintf("kubectl logs -n %s job/%s", nsName, seedJobName))
	} else if message := r.deploymentDiagnostic(ctx, nsName, "app"); message != "" {
		diag.Component = "app"
		diag.Message = message
		diag.DebugCommands = append(diag.DebugCommands, fmt.Sprintf("kubectl describe deployment app -n %s", nsName))
	} else if message := r.deploymentDiagnostic(ctx, nsName, "postgres"); message != "" {
		diag.Component = "database"
		diag.Message = message
		diag.DebugCommands = append(diag.DebugCommands, fmt.Sprintf("kubectl describe deployment postgres -n %s", nsName))
	}

	diag.LastEvents = r.warningEvents(ctx, nsName, 3)
	return diag
}

func diagnosticComponent(reason string) string {
	switch {
	case strings.Contains(reason, "Migration"):
		return "migration"
	case strings.Contains(reason, "Seed"):
		return "seed"
	case strings.Contains(reason, "Database"):
		return "database"
	case strings.Contains(reason, "Deployment"):
		return "app"
	case strings.Contains(reason, "Ingress"):
		return "ingress"
	case strings.Contains(reason, "Service"):
		return "service"
	case strings.Contains(reason, "Quota"):
		return "quota"
	case strings.Contains(reason, "Namespace"):
		return "namespace"
	default:
		return "preview"
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
	if deploymentName == "app" {
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
		return events.Items[i].LastTimestamp.Time.After(events.Items[j].LastTimestamp.Time)
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
