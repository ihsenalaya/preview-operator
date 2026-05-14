package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const testScriptsConfigMap = "preview-test-scripts"

// TestScripts holds default job configuration loaded from the preview-test-scripts
// ConfigMap in the operator namespace. Scripts themselves live in the app image
// under /app/tests/. The ConfigMap only stores overridable defaults for commands
// and images. Falls back to built-in defaults when the ConfigMap is absent.
type TestScripts struct {
	MigrationCommand  string
	RegressionCommand string
	E2EImage          string
	E2ECommand        string
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

// defaultTestScripts returns hardcoded fallback values.
func defaultTestScripts() *TestScripts {
	return &TestScripts{
		MigrationCommand:  "pip install alembic psycopg2-binary -q 2>/dev/null && alembic upgrade head && echo 'PASS migration alembic upgrade head: OK'",
		RegressionCommand: "python /app/tests/regression.py",
		E2EImage:          playwrightImage,
		E2ECommand:        "python -m pip install requests playwright==1.44.0 -q >/dev/null 2>&1 && python /data/tests/e2e.py",
	}
}
