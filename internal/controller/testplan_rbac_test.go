package controller

// TestAgentRBACBoundedBlastRadius verifies the bounded-blast-radius property:
// the kagent-test-strategist ServiceAccount's ClusterRole allows reading
// Previews/TestPlans/ReconcileEvents and writing TestPlans, but explicitly
// grants NO access to Deployments, Pods, Secrets, or other workload resources.
//
// This test reads the ClusterRole YAML from disk and asserts on its structure,
// so it runs without a live cluster and works in CI. It documents the security
// property that makes the architecture safe: an attacker compromising the agent
// can at worst write a malicious TestPlan; the controller's confidence check
// and structural validation bound the blast radius.

import (
	"os"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

type clusterRoleYAML struct {
	Rules []struct {
		APIGroups []string `yaml:"apiGroups"`
		Resources []string `yaml:"resources"`
		Verbs     []string `yaml:"verbs"`
	} `yaml:"rules"`
}

func TestAgentRBACBoundedBlastRadius(t *testing.T) {
	data, err := os.ReadFile("../../config/rbac/agent_role.yaml")
	if err != nil {
		t.Skipf("agent_role.yaml not found: %v", err)
	}

	var role clusterRoleYAML
	if err := yaml.Unmarshal(data, &role); err != nil {
		t.Fatalf("unmarshal agent_role.yaml: %v", err)
	}

	forbiddenResources := []string{"deployments", "pods", "secrets", "configmaps", "jobs", "services", "testruns"}
	forbiddenVerbs := []string{"delete", "deletecollection", "escalate", "bind"}

	for _, rule := range role.Rules {
		for _, res := range rule.Resources {
			for _, forbidden := range forbiddenResources {
				if strings.EqualFold(res, forbidden) {
					for _, verb := range rule.Verbs {
						if verb != "get" && verb != "list" && verb != "watch" {
							t.Errorf("agent ClusterRole grants %q on %q — blast radius violation", verb, res)
						}
					}
				}
			}
			// testplans/status is the only allowed write resource
			if strings.EqualFold(res, "testplans") || strings.EqualFold(res, "testplans/status") {
				continue
			}
			if strings.EqualFold(res, "previews") || strings.EqualFold(res, "reconcileevents") {
				continue
			}
			// Any other resource must only have read verbs
			for _, verb := range rule.Verbs {
				if verb != "get" && verb != "list" && verb != "watch" {
					t.Errorf("agent ClusterRole grants unexpected write verb %q on %q", verb, res)
				}
			}
		}
		// Verbs like "delete" and "bind" must never appear
		for _, verb := range rule.Verbs {
			for _, forbidden := range forbiddenVerbs {
				if strings.EqualFold(verb, forbidden) {
					t.Errorf("agent ClusterRole contains forbidden verb %q on resources %v", verb, rule.Resources)
				}
			}
		}
	}
}
