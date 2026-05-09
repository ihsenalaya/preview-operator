package controller

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"

	platformv1alpha1 "github.com/ihsenalaya/preview-operator/api/v1alpha1"
)

var kagentTaskGVK = schema.GroupVersionKind{
	Group:   "kagent.dev",
	Version: "v1alpha1",
	Kind:    "Task",
}

func kagentEnabled(c *platformv1alpha1.Preview) bool {
	return c.Spec.Kagent != nil && c.Spec.Kagent.Enabled
}

func kagentNamespace(c *platformv1alpha1.Preview) string {
	if c.Spec.Kagent == nil || c.Spec.Kagent.Namespace == "" {
		return "kagent-system"
	}
	return c.Spec.Kagent.Namespace
}

func kagentAgentName(c *platformv1alpha1.Preview) string {
	if c.Spec.Kagent == nil || c.Spec.Kagent.AgentName == "" {
		return "preview-troubleshooter-agent"
	}
	return c.Spec.Kagent.AgentName
}

func kagentTaskName(c *platformv1alpha1.Preview) string {
	return fmt.Sprintf("%s-failure-analysis", c.Name)
}

// triggerKagentAnalysis creates a kagent Task CR when the test suite has failed.
// It is idempotent: if the Task already exists or was already recorded in status, it is a no-op.
func (r *PreviewReconciler) triggerKagentAnalysis(ctx context.Context, c *platformv1alpha1.Preview) {
	logger := log.FromContext(ctx)

	if !kagentEnabled(c) {
		return
	}
	if c.Status.Tests == nil || c.Status.Tests.Phase != phaseFailed {
		return
	}
	if c.Status.Kagent != nil && c.Status.Kagent.TaskName != "" {
		return // already triggered
	}

	taskName := kagentTaskName(c)
	ns := kagentNamespace(c)

	// Check if the Task already exists (e.g. created by a previous reconcile that crashed before status update).
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(kagentTaskGVK)
	err := r.Get(ctx, types.NamespacedName{Name: taskName, Namespace: ns}, existing)
	if err != nil && !errors.IsNotFound(err) {
		logger.Error(err, "Failed to check kagent Task existence", "task", taskName)
		return
	}

	if errors.IsNotFound(err) {
		task := buildKagentTask(c, taskName, ns)
		if createErr := r.Create(ctx, task); createErr != nil {
			logger.Error(createErr, "Failed to create kagent Task", "task", taskName)
			return
		}
		logger.Info("Triggered kagent failure analysis", "task", taskName, "namespace", ns)
	} else {
		logger.Info("kagent Task already exists", "task", taskName)
	}

	r.recordKagentTriggered(ctx, c, taskName)
}

func buildKagentTask(c *platformv1alpha1.Preview, taskName, ns string) *unstructured.Unstructured {
	task := &unstructured.Unstructured{}
	task.SetGroupVersionKind(kagentTaskGVK)
	task.SetName(taskName)
	task.SetNamespace(ns)
	task.SetLabels(map[string]string{
		labelManagedBy:   "preview-operator",
		labelPreviewName: c.Name,
	})
	_ = unstructured.SetNestedField(task.Object, kagentAgentName(c), "spec", "agentRef", "name")
	_ = unstructured.SetNestedField(task.Object, buildKagentPrompt(c), "spec", "prompt")
	return task
}

func buildKagentPrompt(c *platformv1alpha1.Preview) string {
	tests := c.Status.Tests

	var failedSuites []string
	if tests != nil {
		if tests.Smoke.Phase == phaseFailed {
			failedSuites = append(failedSuites, "smoke")
		}
		if tests.Contract.Phase == phaseFailed {
			failedSuites = append(failedSuites, "microcks-contract")
		}
		if tests.Regression.Phase == phaseFailed {
			failedSuites = append(failedSuites, "regression")
		}
		if tests.E2E.Phase == phaseFailed {
			failedSuites = append(failedSuites, "e2e")
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Analyze the test failure in Preview environment %q (PR #%d, branch %q).\n\n",
		c.Name, c.Spec.PRNumber, c.Spec.Branch)
	fmt.Fprintf(&b, "Namespace: %s\n", c.Status.NamespaceName)

	if len(failedSuites) > 0 {
		fmt.Fprintf(&b, "Failed suites: %s\n", strings.Join(failedSuites, ", "))
	}

	if c.Spec.GitHub != nil && c.Spec.GitHub.Owner != "" {
		fmt.Fprintf(&b, "GitHub repo: %s/%s — PR #%d\n", c.Spec.GitHub.Owner, c.Spec.GitHub.Repo, c.Spec.PRNumber)
	}

	b.WriteString("\nInspect the namespace resources, job logs, and events. " +
		"Then post a structured failure analysis as a GitHub PR comment " +
		"following the format: Risk level / Failed suite / Evidence / Likely cause / Suggested fix / Confidence.")

	return b.String()
}

func (r *PreviewReconciler) recordKagentTriggered(ctx context.Context, c *platformv1alpha1.Preview, taskName string) {
	if c.Status.Kagent == nil {
		c.Status.Kagent = &platformv1alpha1.KagentStatus{}
	}
	c.Status.Kagent.TaskName = taskName
	now := metav1.Now()
	c.Status.Kagent.TriggeredAt = &now
	_ = r.Status().Update(ctx, c)
}
