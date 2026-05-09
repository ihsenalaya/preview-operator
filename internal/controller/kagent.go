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

// kagentAgentGVK is the GVK for the kagent Agent CR (v0.9+, v1alpha2).
var kagentAgentGVK = schema.GroupVersionKind{
	Group:   "kagent.dev",
	Version: "v1alpha2",
	Kind:    "Agent",
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

func kagentAgentCRName(c *platformv1alpha1.Preview) string {
	return fmt.Sprintf("%s-failure-analysis", c.Name)
}

// triggerKagentAnalysis creates a kagent Agent CR when the test suite has failed.
// The Agent is pre-loaded with the failure context as its system message so it is
// immediately usable from the kagent UI without any manual configuration.
// It is idempotent: if the Agent already exists or was already recorded in status, it is a no-op.
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

	agentName := kagentAgentCRName(c)
	ns := kagentNamespace(c)

	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(kagentAgentGVK)
	err := r.Get(ctx, types.NamespacedName{Name: agentName, Namespace: ns}, existing)
	if err != nil && !errors.IsNotFound(err) {
		logger.Error(err, "Failed to check kagent Agent existence", "agent", agentName)
		return
	}

	if errors.IsNotFound(err) {
		agent := buildKagentAgent(c, agentName, ns)
		if createErr := r.Create(ctx, agent); createErr != nil {
			logger.Error(createErr, "Failed to create kagent Agent", "agent", agentName)
			return
		}
		logger.Info("Created kagent failure-analysis Agent", "agent", agentName, "namespace", ns)
	} else {
		logger.Info("kagent Agent already exists", "agent", agentName)
	}

	r.recordKagentTriggered(ctx, c, agentName)
}

func buildKagentAgent(c *platformv1alpha1.Preview, agentName, ns string) *unstructured.Unstructured {
	agent := &unstructured.Unstructured{}
	agent.SetGroupVersionKind(kagentAgentGVK)
	agent.SetName(agentName)
	agent.SetNamespace(ns)
	agent.SetLabels(map[string]string{
		labelManagedBy:   "preview-operator",
		labelPreviewName: c.Name,
	})

	// Build tools list: use the built-in kagent tool server for k8s inspection.
	tools := []interface{}{
		map[string]interface{}{
			"type": "McpServer",
			"mcpServer": map[string]interface{}{
				"kind":     "RemoteMCPServer",
				"apiGroup": "kagent.dev",
				"name":     "kagent-tool-server",
				"toolNames": []interface{}{
					"k8s_get_pod_logs",
					"k8s_get_resources",
					"k8s_get_events",
					"k8s_describe_resource",
					"k8s_get_resource_yaml",
				},
			},
		},
	}

	_ = unstructured.SetNestedField(agent.Object, "Declarative", "spec", "type")
	_ = unstructured.SetNestedField(agent.Object,
		fmt.Sprintf("Failure analysis agent for Preview %q (PR #%d)", c.Name, c.Spec.PRNumber),
		"spec", "description")
	_ = unstructured.SetNestedField(agent.Object, "default-model-config", "spec", "declarative", "modelConfig")
	_ = unstructured.SetNestedField(agent.Object, buildKagentSystemMessage(c), "spec", "declarative", "systemMessage")
	_ = unstructured.SetNestedSlice(agent.Object, tools, "spec", "declarative", "tools")

	return agent
}

func buildKagentSystemMessage(c *platformv1alpha1.Preview) string {
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
	fmt.Fprintf(&b, "You are a failure-analysis agent for the Preview environment %q.\n\n", c.Name)
	fmt.Fprintf(&b, "Context:\n")
	fmt.Fprintf(&b, "  PR #%d — branch %q\n", c.Spec.PRNumber, c.Spec.Branch)
	fmt.Fprintf(&b, "  Namespace: %s\n", c.Status.NamespaceName)

	if len(failedSuites) > 0 {
		fmt.Fprintf(&b, "  Failed suites: %s\n", strings.Join(failedSuites, ", "))
	}

	if c.Spec.GitHub != nil && c.Spec.GitHub.Owner != "" {
		fmt.Fprintf(&b, "  GitHub repo: %s/%s\n", c.Spec.GitHub.Owner, c.Spec.GitHub.Repo)
	}

	b.WriteString("\nWhen asked to analyze the failure:\n")
	b.WriteString("1. Inspect the namespace resources, pod logs, jobs, and events.\n")
	b.WriteString("2. Identify the root cause of each failed test suite.\n")
	b.WriteString("3. Post a structured failure analysis as a GitHub PR comment using this format:\n")
	b.WriteString("   Risk level | Failed suite | Evidence | Likely cause | Suggested fix | Confidence\n")

	return b.String()
}

func (r *PreviewReconciler) recordKagentTriggered(ctx context.Context, c *platformv1alpha1.Preview, agentName string) {
	if c.Status.Kagent == nil {
		c.Status.Kagent = &platformv1alpha1.KagentStatus{}
	}
	c.Status.Kagent.TaskName = agentName
	now := metav1.Now()
	c.Status.Kagent.TriggeredAt = &now
	_ = r.Status().Update(ctx, c)
}
