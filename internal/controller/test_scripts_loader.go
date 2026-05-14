package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const testScriptsConfigMap = "preview-test-scripts"

// TestScripts holds the scripts and default job configuration loaded from the
// preview-test-scripts ConfigMap in the operator namespace.
// The operator falls back to hardcoded defaults when the ConfigMap is absent.
type TestScripts struct {
	SmokeScript         string
	MicrocksScript      string
	MicrocksImportScript string

	SmokeImage    string
	SmokeCommand  string

	MigrationCommand string

	RegressionCommand string

	E2EImage   string
	E2ECommand string
}

// loadTestScripts reads preview-test-scripts from the operator namespace.
// Missing keys fall back to the hardcoded defaults below.
func (r *PreviewReconciler) loadTestScripts(ctx context.Context) *TestScripts {
	s := defaultTestScripts()

	cm := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      testScriptsConfigMap,
		Namespace: r.OperatorNamespace,
	}, cm)
	if err != nil {
		log.FromContext(ctx).V(1).Info("preview-test-scripts ConfigMap not found, using built-in defaults", "namespace", r.OperatorNamespace)
		return s
	}

	if v, ok := cm.Data["smoke.py"]; ok && v != "" {
		s.SmokeScript = v
	}
	if v, ok := cm.Data["microcks.py"]; ok && v != "" {
		s.MicrocksScript = v
	}
	if v, ok := cm.Data["microcks-import.py"]; ok && v != "" {
		s.MicrocksImportScript = v
	}
	if v, ok := cm.Data["smoke.image"]; ok && v != "" {
		s.SmokeImage = v
	}
	if v, ok := cm.Data["smoke.command"]; ok && v != "" {
		s.SmokeCommand = v
	}
	if v, ok := cm.Data["migration.command"]; ok && v != "" {
		s.MigrationCommand = v
	}
	if v, ok := cm.Data["regression.command"]; ok && v != "" {
		s.RegressionCommand = v
	}
	if v, ok := cm.Data["e2e.image"]; ok && v != "" {
		s.E2EImage = v
	}
	if v, ok := cm.Data["e2e.command"]; ok && v != "" {
		s.E2ECommand = v
	}

	return s
}

// defaultTestScripts returns hardcoded fallback values used when the
// preview-test-scripts ConfigMap is absent or a key is missing.
func defaultTestScripts() *TestScripts {
	return &TestScripts{
		SmokeScript:          smokeScript,
		MicrocksScript:       microcksContractScript,
		MicrocksImportScript: microcksImportScript,

		SmokeImage:   "python:3.12-slim",
		SmokeCommand: "pip install requests -q 2>/dev/null && python /data/smoke.py",

		MigrationCommand: "pip install alembic psycopg2-binary -q 2>/dev/null && alembic upgrade head && echo 'PASS migration alembic upgrade head: OK'",

		RegressionCommand: "pip install requests -q 2>/dev/null && python /app/tests/regression.py",

		E2EImage:   playwrightImage,
		E2ECommand: "python -m pip install requests playwright==1.44.0 -q >/dev/null 2>&1 && python /data/tests/e2e.py",
	}
}
