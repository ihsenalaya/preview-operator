package controller

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	platformv1alpha1 "github.com/ihsenalaya/preview-operator/api/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestFetchPRDiffReturnsBody(t *testing.T) {
	reconciler := &PreviewReconciler{
		GitHubAPIBaseURL: "https://api.github.test",
		GitHubHTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.Method != http.MethodGet {
				t.Fatalf("expected GET, got %s", r.Method)
			}
			if got := r.Header.Get("Accept"); got != "application/vnd.github.diff" {
				t.Fatalf("unexpected Accept header: %q", got)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("diff --git a/app.py b/app.py")),
			}, nil
		})},
	}
	c := &platformv1alpha1.Preview{
		Spec: platformv1alpha1.PreviewSpec{
			PRNumber: 21,
			GitHub: &platformv1alpha1.GitHubIntegrationSpec{
				Enabled: true,
				Owner:   "ihsenalaya",
				Repo:    "idp-testing",
			},
		},
	}

	diff, err := reconciler.fetchPRDiff(context.Background(), c, "token")
	if err != nil {
		t.Fatalf("fetchPRDiff returned error: %v", err)
	}
	if diff != "diff --git a/app.py b/app.py" {
		t.Fatalf("unexpected diff: %q", diff)
	}
}

func TestFetchPRDiffSkipsWhenGitHubDisabled(t *testing.T) {
	reconciler := &PreviewReconciler{}
	diff, err := reconciler.fetchPRDiff(context.Background(), &platformv1alpha1.Preview{}, "token")
	if err != nil {
		t.Fatalf("fetchPRDiff returned error: %v", err)
	}
	if diff != "" {
		t.Fatalf("expected empty diff, got %q", diff)
	}
}

func TestAIGitHubTokenUsesDedicatedSecretWhenConfigured(t *testing.T) {
	scheme := testAIScheme(t)
	c := &platformv1alpha1.Preview{
		Spec: platformv1alpha1.PreviewSpec{
			AIEnrichment: &platformv1alpha1.AIEnrichmentSpec{
				Enabled: true,
				GitHubTokenSecretRef: &platformv1alpha1.GitHubTokenSecretRef{
					Name:      "ai-github-token",
					Namespace: defaultGitHubSecretNamespace,
				},
			},
			GitHub: &platformv1alpha1.GitHubIntegrationSpec{
				Enabled: true,
				TokenSecretRef: &platformv1alpha1.GitHubTokenSecretRef{
					Name:      "controller-github-token",
					Namespace: defaultGitHubSecretNamespace,
				},
			},
		},
	}
	aiSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "ai-github-token", Namespace: defaultGitHubSecretNamespace},
		Data:       map[string][]byte{defaultGitHubTokenSecretKey: []byte("ai-token")},
	}
	controllerSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "controller-github-token", Namespace: defaultGitHubSecretNamespace},
		Data:       map[string][]byte{defaultGitHubTokenSecretKey: []byte("controller-token")},
	}
	reconciler := &PreviewReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(c, aiSecret, controllerSecret).Build(),
		Scheme: scheme,
	}

	token, err := reconciler.aiGitHubToken(context.Background(), c)
	if err != nil {
		t.Fatalf("aiGitHubToken returned error: %v", err)
	}
	if token != "ai-token" {
		t.Fatalf("aiGitHubToken = %q, want %q", token, "ai-token")
	}
}

func TestAIGitHubTokenFallsBackToControllerSecret(t *testing.T) {
	scheme := testAIScheme(t)
	c := &platformv1alpha1.Preview{
		Spec: platformv1alpha1.PreviewSpec{
			AIEnrichment: &platformv1alpha1.AIEnrichmentSpec{Enabled: true},
			GitHub: &platformv1alpha1.GitHubIntegrationSpec{
				Enabled: true,
				TokenSecretRef: &platformv1alpha1.GitHubTokenSecretRef{
					Name:      "controller-github-token",
					Namespace: defaultGitHubSecretNamespace,
				},
			},
		},
	}
	controllerSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "controller-github-token", Namespace: defaultGitHubSecretNamespace},
		Data:       map[string][]byte{defaultGitHubTokenSecretKey: []byte("controller-token")},
	}
	reconciler := &PreviewReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(c, controllerSecret).Build(),
		Scheme: scheme,
	}

	token, err := reconciler.aiGitHubToken(context.Background(), c)
	if err != nil {
		t.Fatalf("aiGitHubToken returned error: %v", err)
	}
	if token != "controller-token" {
		t.Fatalf("aiGitHubToken = %q, want %q", token, "controller-token")
	}
}

func TestAISchemaDumpJobBuildsExpectedSpec(t *testing.T) {
	c := &platformv1alpha1.Preview{ObjectMeta: metav1.ObjectMeta{Name: "pr-21"}}
	reconciler := &PreviewReconciler{}

	job := reconciler.aiSchemaDumpJob(c, "preview-pr-21")
	if job.Name != aiSchemaJobName {
		t.Fatalf("unexpected job name: %s", job.Name)
	}
	if job.Spec.Template.Spec.Containers[0].Image != defaultAISchemaDumpImage {
		t.Fatalf("unexpected image: %s", job.Spec.Template.Spec.Containers[0].Image)
	}
	if len(job.Spec.Template.Spec.Containers[0].EnvFrom) != 1 {
		t.Fatalf("expected EnvFrom secret wiring")
	}
	if len(job.Spec.Template.Spec.Containers[0].Env) != 1 || job.Spec.Template.Spec.Containers[0].Env[0].Name != "PGPASSWORD" {
		t.Fatalf("expected PGPASSWORD env wiring for schema dump job")
	}
}

func TestFetchDBSchemaCreatesJobWhenMissing(t *testing.T) {
	scheme := testAIScheme(t)
	c := &platformv1alpha1.Preview{
		ObjectMeta: metav1.ObjectMeta{Name: "pr-21"},
		Spec: platformv1alpha1.PreviewSpec{
			Database: &platformv1alpha1.DatabaseSpec{Enabled: true},
		},
	}
	reconciler := &PreviewReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(c).Build(),
		Scheme: scheme,
	}

	_, ready, err := reconciler.fetchDBSchema(context.Background(), c, "preview-pr-21")
	if err != nil {
		t.Fatalf("fetchDBSchema returned error: %v", err)
	}
	if ready {
		t.Fatalf("expected fetchDBSchema to request requeue while job starts")
	}

	job := &batchv1.Job{}
	if err := reconciler.Get(context.Background(), types.NamespacedName{Name: aiSchemaJobName, Namespace: "preview-pr-21"}, job); err != nil {
		t.Fatalf("expected schema dump job to be created: %v", err)
	}
}

func TestReconcileResourceQuotaAddsAIHeadroom(t *testing.T) {
	scheme := testAIScheme(t)
	c := &platformv1alpha1.Preview{
		ObjectMeta: metav1.ObjectMeta{Name: "pr-23"},
		Spec: platformv1alpha1.PreviewSpec{
			ResourceTier: platformv1alpha1.TierMedium,
			Database: &platformv1alpha1.DatabaseSpec{
				Enabled: true,
			},
			AIEnrichment: &platformv1alpha1.AIEnrichmentSpec{
				Enabled: true,
			},
		},
	}
	reconciler := &PreviewReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(c).Build(),
		Scheme: scheme,
	}

	if err := reconciler.reconcileResourceQuota(context.Background(), c, "preview-pr-23"); err != nil {
		t.Fatalf("reconcileResourceQuota returned error: %v", err)
	}

	quota := &corev1.ResourceQuota{}
	if err := reconciler.Get(context.Background(), types.NamespacedName{Name: "preview-quota", Namespace: "preview-pr-23"}, quota); err != nil {
		t.Fatalf("expected resourcequota to be created: %v", err)
	}

	assertQuantity := func(name corev1.ResourceName, want string) {
		t.Helper()
		got, ok := quota.Spec.Hard[name]
		if !ok {
			t.Fatalf("missing quota entry for %s", name)
		}
		if got.Cmp(resource.MustParse(want)) != 0 {
			t.Fatalf("%s = %s, want %s", name, got.String(), want)
		}
	}

	assertQuantity(corev1.ResourceLimitsCPU, "1500m")
	assertQuantity(corev1.ResourceLimitsMemory, "1536Mi")
	assertQuantity(corev1.ResourceRequestsCPU, "350m")
	assertQuantity(corev1.ResourceRequestsMemory, "512Mi")
}

func TestGenerateAndStoreAIContentSkipsWhenConfigMapExists(t *testing.T) {
	scheme := testAIScheme(t)
	c := &platformv1alpha1.Preview{
		ObjectMeta: metav1.ObjectMeta{Name: "pr-21"},
		Spec: platformv1alpha1.PreviewSpec{
			AIEnrichment: &platformv1alpha1.AIEnrichmentSpec{Enabled: true},
		},
	}
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: aiEnrichmentConfigMap, Namespace: "preview-pr-21"}}
	reconciler := &PreviewReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(c, cm).Build(),
		Scheme: scheme,
	}

	ready, err := reconciler.generateAndStoreAIContent(context.Background(), c, "preview-pr-21")
	if err != nil {
		t.Fatalf("generateAndStoreAIContent returned error: %v", err)
	}
	if !ready {
		t.Fatalf("expected generateAndStoreAIContent to complete when configmap already exists")
	}
}

func TestGenerateAndStoreAIContentCreatesConfigMap(t *testing.T) {
	scheme := testAIScheme(t)
	c := &platformv1alpha1.Preview{
		ObjectMeta: metav1.ObjectMeta{Name: "pr-21"},
		Spec: platformv1alpha1.PreviewSpec{
			Branch:   "feature/ai",
			PRNumber: 21,
			AIEnrichment: &platformv1alpha1.AIEnrichmentSpec{
				Enabled: true,
				Model:   "gpt-test",
				APISecretRef: &platformv1alpha1.SecretKeyRef{
					Name: "ai-api",
				},
			},
		},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "ai-api", Namespace: defaultAISecretNamespace},
		Data:       map[string][]byte{defaultAISecretKey: []byte("test-key")},
	}
	reconciler := &PreviewReconciler{
		Client:       fake.NewClientBuilder().WithScheme(scheme).WithObjects(c, secret).Build(),
		Scheme:       scheme,
		AIAPIBaseURL: "https://ai.example.test",
		AIHTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body: io.NopCloser(strings.NewReader(
					`{"choices":[{"message":{"content":"{\"seed_sql\":\"INSERT INTO messages VALUES (1);\",\"test_script\":\"print('pass')\"}"}}]}`,
				)),
			}, nil
		})},
	}

	ready, err := reconciler.generateAndStoreAIContent(context.Background(), c, "preview-pr-21")
	if err != nil {
		t.Fatalf("generateAndStoreAIContent returned error: %v", err)
	}
	if !ready {
		t.Fatalf("expected generateAndStoreAIContent to complete without database schema fetch")
	}

	cm := &corev1.ConfigMap{}
	if err := reconciler.Get(context.Background(), types.NamespacedName{Name: aiEnrichmentConfigMap, Namespace: "preview-pr-21"}, cm); err != nil {
		t.Fatalf("expected ai enrichment configmap to be created: %v", err)
	}
	if cm.Data["seed.sql"] != "INSERT INTO messages VALUES (1);" {
		t.Fatalf("unexpected seed.sql: %q", cm.Data["seed.sql"])
	}
	if cm.Data["test.py"] != "print('pass')" {
		t.Fatalf("unexpected test.py: %q", cm.Data["test.py"])
	}
}

func TestGenerateAndStoreAIContentUsesSystemPromptConfigMap(t *testing.T) {
	scheme := testAIScheme(t)
	c := &platformv1alpha1.Preview{
		ObjectMeta: metav1.ObjectMeta{Name: "pr-21"},
		Spec: platformv1alpha1.PreviewSpec{
			Branch:   "feature/ai",
			PRNumber: 21,
			AIEnrichment: &platformv1alpha1.AIEnrichmentSpec{
				Enabled: true,
				Model:   "gpt-test",
				APISecretRef: &platformv1alpha1.SecretKeyRef{
					Name: "ai-api",
				},
			},
		},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "ai-api", Namespace: defaultAISecretNamespace},
		Data:       map[string][]byte{defaultAISecretKey: []byte("test-key")},
	}
	systemPrompt := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: aiSystemPromptConfigMap, Namespace: "operator-system"},
		Data:       map[string]string{aiSystemPromptKey: "Custom system prompt from Helm."},
	}
	prPrompt := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: aiPromptConfigMapName(c.Name), Namespace: defaultAISecretNamespace},
		Data:       map[string]string{aiPromptConfigMapKey: "Prefer 201 for POST /api/products."},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(c, secret, systemPrompt, prPrompt).Build()

	reconciler := &PreviewReconciler{
		Client:            fakeClient,
		APIReader:         fakeClient,
		Scheme:            scheme,
		OperatorNamespace: "operator-system",
		AIAPIBaseURL:      "https://ai.example.test",
		AIHTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("failed to read AI request body: %v", err)
			}
			payload := string(body)
			for _, want := range []string{
				"Custom system prompt from Helm.",
				"Additional instructions:",
				"Prefer 201 for POST /api/products.",
			} {
				if !strings.Contains(payload, want) {
					t.Fatalf("AI request body missing %q:\n%s", want, payload)
				}
			}
			if strings.Contains(payload, "You are a developer tool for Kubernetes preview environments.") {
				t.Fatalf("expected custom system prompt to replace the default template:\n%s", payload)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body: io.NopCloser(strings.NewReader(
					`{"choices":[{"message":{"content":"{\"seed_sql\":\"\",\"test_script\":\"print('pass')\"}"}}]}`,
				)),
			}, nil
		})},
	}

	ready, err := reconciler.generateAndStoreAIContent(context.Background(), c, "preview-pr-21")
	if err != nil {
		t.Fatalf("generateAndStoreAIContent returned error: %v", err)
	}
	if !ready {
		t.Fatalf("expected generateAndStoreAIContent to complete")
	}
}

func TestExtractAITestResults(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		want  []string
	}{
		{
			name:  "extracts PASS and FAIL lines",
			lines: []string{"some noise", "PASS GET /health", "FAIL POST /login - 401", "more noise"},
			want:  []string{"PASS GET /health", "FAIL POST /login - 401"},
		},
		{
			name:  "empty input",
			lines: []string{},
			want:  nil,
		},
		{
			name:  "no matching lines",
			lines: []string{"pip install requests", "running tests..."},
			want:  nil,
		},
		{
			name:  "trims whitespace",
			lines: []string{"  PASS GET /  "},
			want:  []string{"PASS GET /"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractAITestResults(tt.lines)
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("[%d] got %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestAIEnrichmentEnabled(t *testing.T) {
	tests := []struct {
		name string
		spec *platformv1alpha1.AIEnrichmentSpec
		want bool
	}{
		{"nil spec", nil, false},
		{"disabled", &platformv1alpha1.AIEnrichmentSpec{Enabled: false}, false},
		{"enabled", &platformv1alpha1.AIEnrichmentSpec{Enabled: true}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &platformv1alpha1.Preview{Spec: platformv1alpha1.PreviewSpec{AIEnrichment: tt.spec}}
			if got := aiEnrichmentEnabled(c); got != tt.want {
				t.Errorf("aiEnrichmentEnabled = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAIEnrichmentTaskDefaults(t *testing.T) {
	enabled := &platformv1alpha1.Preview{
		Spec: platformv1alpha1.PreviewSpec{
			AIEnrichment: &platformv1alpha1.AIEnrichmentSpec{Enabled: true},
		},
	}
	if !aiSeedEnabled(enabled) {
		t.Fatal("expected seed task to default to enabled when omitted")
	}
	if !aiTestsEnabled(enabled) {
		t.Fatal("expected test task to default to enabled when omitted")
	}

	disabled := &platformv1alpha1.Preview{
		Spec: platformv1alpha1.PreviewSpec{
			AIEnrichment: &platformv1alpha1.AIEnrichmentSpec{
				Enabled: true,
				Seed:    &platformv1alpha1.AIEnrichmentTaskSpec{Enabled: false},
				Tests:   &platformv1alpha1.AIEnrichmentTaskSpec{Enabled: false},
			},
		},
	}
	if aiSeedEnabled(disabled) {
		t.Fatal("expected explicit seed=false to be honored")
	}
	if aiTestsEnabled(disabled) {
		t.Fatal("expected explicit tests=false to be honored")
	}
}

func TestSetAIEnrichmentConditionReflectsFailure(t *testing.T) {
	c := &platformv1alpha1.Preview{
		Spec: platformv1alpha1.PreviewSpec{
			AIEnrichment: &platformv1alpha1.AIEnrichmentSpec{Enabled: true},
		},
		Status: platformv1alpha1.PreviewStatus{
			AIEnrichment: &platformv1alpha1.AIEnrichmentStatus{
				Phase:       phaseFailed,
				SeedStatus:  phaseSucceeded,
				TestsStatus: phaseFailed,
				Error:       "AI test job failed",
			},
		},
	}

	setAIEnrichmentCondition(c)

	condition := meta.FindStatusCondition(c.Status.Conditions, platformv1alpha1.ConditionAIEnrichmentReady)
	if condition == nil {
		t.Fatal("expected AIEnrichmentReady condition to be set")
	}
	if condition.Status != metav1.ConditionFalse {
		t.Fatalf("condition status = %s, want %s", condition.Status, metav1.ConditionFalse)
	}
	if condition.Reason != "AIEnrichmentFailed" {
		t.Fatalf("condition reason = %q, want %q", condition.Reason, "AIEnrichmentFailed")
	}
}

func TestBuildAIEnrichmentSectionShowsDefaultTasks(t *testing.T) {
	c := &platformv1alpha1.Preview{
		Spec: platformv1alpha1.PreviewSpec{
			AIEnrichment: &platformv1alpha1.AIEnrichmentSpec{Enabled: true},
		},
		Status: platformv1alpha1.PreviewStatus{
			AIEnrichment: &platformv1alpha1.AIEnrichmentStatus{
				SeedStatus:  phaseSucceeded,
				TestsStatus: phaseFailed,
				TestResults: []string{"FAIL GET /messages - 500"},
			},
		},
	}

	section := buildAIEnrichmentSection(c)
	for _, want := range []string{
		"### AI Enrichment",
		"- Seed: SUCCESS `Succeeded`",
		"- Tests: FAIL `Failed`",
		"`FAIL GET /messages - 500`",
	} {
		if !strings.Contains(section, want) {
			t.Fatalf("buildAIEnrichmentSection missing %q:\n%s", want, section)
		}
	}
}

func TestAITestJobSpec(t *testing.T) {
	c := &platformv1alpha1.Preview{
		ObjectMeta: metav1.ObjectMeta{Name: "pr-21"},
		Spec: platformv1alpha1.PreviewSpec{
			AIEnrichment: &platformv1alpha1.AIEnrichmentSpec{
				Tests: &platformv1alpha1.AIEnrichmentTaskSpec{Enabled: true},
			},
		},
	}
	reconciler := &PreviewReconciler{}
	job := reconciler.aiTestJob(c, "preview-pr-21")

	container := job.Spec.Template.Spec.Containers[0]

	// Must have APP_URL injected
	found := false
	for _, e := range container.Env {
		if e.Name == "APP_URL" && e.Value == defaultAIInternalAppURL {
			found = true
		}
	}
	if !found {
		t.Error("expected APP_URL env var to be set on test job container")
	}

	// Must NOT have postgres secret (tests don't need DB access)
	if len(container.EnvFrom) != 0 {
		t.Errorf("test job should not mount postgres secret, got %d EnvFrom entries", len(container.EnvFrom))
	}

	// Must mount ConfigMap volume
	if len(job.Spec.Template.Spec.Volumes) == 0 {
		t.Error("expected ConfigMap volume on test job")
	}
}

func TestAISeedJobSpec(t *testing.T) {
	c := &platformv1alpha1.Preview{
		ObjectMeta: metav1.ObjectMeta{Name: "pr-21"},
		Spec: platformv1alpha1.PreviewSpec{
			AIEnrichment: &platformv1alpha1.AIEnrichmentSpec{
				Seed: &platformv1alpha1.AIEnrichmentTaskSpec{Enabled: true},
			},
		},
	}
	reconciler := &PreviewReconciler{}
	job := reconciler.aiSeedJob(c, "preview-pr-21")

	container := job.Spec.Template.Spec.Containers[0]

	// Must have postgres secret
	if len(container.EnvFrom) == 0 {
		t.Error("expected postgres secret on seed job container via EnvFrom")
	}
	if container.EnvFrom[0].SecretRef == nil || container.EnvFrom[0].SecretRef.Name != postgresSecretName {
		t.Errorf("expected postgres-credentials secret, got %v", container.EnvFrom)
	}
	if len(container.Env) != 1 || container.Env[0].Name != "PGPASSWORD" {
		t.Errorf("expected PGPASSWORD env var on seed job, got %v", container.Env)
	}

	// Must mount ConfigMap volume
	if len(job.Spec.Template.Spec.Volumes) == 0 {
		t.Error("expected ConfigMap volume on seed job")
	}
}

func TestReconcileAISeedJobSkipsWhenDisabled(t *testing.T) {
	c := &platformv1alpha1.Preview{
		Spec: platformv1alpha1.PreviewSpec{
			AIEnrichment: &platformv1alpha1.AIEnrichmentSpec{
				Seed: &platformv1alpha1.AIEnrichmentTaskSpec{Enabled: false},
			},
		},
	}
	reconciler := &PreviewReconciler{}
	state, err := reconciler.reconcileAISeedJob(context.Background(), c, "preview-pr-21")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != phaseSkipped {
		t.Errorf("expected Skipped, got %q", state)
	}
}

func TestReconcileAISeedJobRunsWhenTaskSpecOmitted(t *testing.T) {
	scheme := testAIScheme(t)
	c := &platformv1alpha1.Preview{
		ObjectMeta: metav1.ObjectMeta{Name: "pr-21"},
		Spec: platformv1alpha1.PreviewSpec{
			AIEnrichment: &platformv1alpha1.AIEnrichmentSpec{Enabled: true},
		},
	}
	reconciler := &PreviewReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(c).Build(),
		Scheme: scheme,
	}

	state, err := reconciler.reconcileAISeedJob(context.Background(), c, "preview-pr-21")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != phaseRunning {
		t.Fatalf("expected Running, got %q", state)
	}

	job := &batchv1.Job{}
	if err := reconciler.Get(context.Background(), types.NamespacedName{Name: aiSeedJobName, Namespace: "preview-pr-21"}, job); err != nil {
		t.Fatalf("expected ai-seed job to be created: %v", err)
	}
}

func TestReconcileAITestJobRunsWhenTaskSpecOmitted(t *testing.T) {
	scheme := testAIScheme(t)
	c := &platformv1alpha1.Preview{
		ObjectMeta: metav1.ObjectMeta{Name: "pr-21"},
		Spec: platformv1alpha1.PreviewSpec{
			AIEnrichment: &platformv1alpha1.AIEnrichmentSpec{Enabled: true},
		},
	}
	reconciler := &PreviewReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(c).Build(),
		Scheme: scheme,
	}

	state, results, err := reconciler.reconcileAITestJob(context.Background(), c, "preview-pr-21")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != phaseRunning {
		t.Fatalf("expected Running, got %q", state)
	}
	if len(results) != 0 {
		t.Fatalf("expected no results while job is starting, got %v", results)
	}

	job := &batchv1.Job{}
	if err := reconciler.Get(context.Background(), types.NamespacedName{Name: aiTestJobName, Namespace: "preview-pr-21"}, job); err != nil {
		t.Fatalf("expected ai-tests job to be created: %v", err)
	}
}

func TestReconcileAIJobReturnsRunningOnCreate(t *testing.T) {
	scheme := testAIScheme(t)
	c := &platformv1alpha1.Preview{
		ObjectMeta: metav1.ObjectMeta{Name: "pr-21"},
		Spec:       platformv1alpha1.PreviewSpec{},
	}
	reconciler := &PreviewReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(c).Build(),
		Scheme: scheme,
	}
	desired := reconciler.aiTestJob(c, "preview-pr-21")

	state, err := reconciler.reconcileAIJob(context.Background(), c, "preview-pr-21", aiTestJobName, desired, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != phaseRunning {
		t.Errorf("expected Running on job creation, got %q", state)
	}
}

func TestReconcileAIJobReturnsSucceededWhenJobDone(t *testing.T) {
	scheme := testAIScheme(t)
	c := &platformv1alpha1.Preview{
		ObjectMeta: metav1.ObjectMeta{Name: "pr-21"},
	}
	existingJob := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: aiTestJobName, Namespace: "preview-pr-21"},
		Status:     batchv1.JobStatus{Succeeded: 1},
	}
	reconciler := &PreviewReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(c, existingJob).Build(),
		Scheme: scheme,
	}
	desired := reconciler.aiTestJob(c, "preview-pr-21")

	state, err := reconciler.reconcileAIJob(context.Background(), c, "preview-pr-21", aiTestJobName, desired, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != phaseSucceeded {
		t.Errorf("expected Succeeded, got %q", state)
	}
}

func TestReconcileAISeedJobKeepsCompletedJobUntilTTL(t *testing.T) {
	scheme := testAIScheme(t)
	c := &platformv1alpha1.Preview{
		ObjectMeta: metav1.ObjectMeta{Name: "pr-21"},
		Spec: platformv1alpha1.PreviewSpec{
			AIEnrichment: &platformv1alpha1.AIEnrichmentSpec{Enabled: true},
		},
	}
	existingJob := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: aiSeedJobName, Namespace: "preview-pr-21"},
		Status:     batchv1.JobStatus{Succeeded: 1},
	}
	reconciler := &PreviewReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(c, existingJob).Build(),
		Scheme: scheme,
	}

	state, err := reconciler.reconcileAISeedJob(context.Background(), c, "preview-pr-21")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != phaseSucceeded {
		t.Fatalf("expected Succeeded, got %q", state)
	}

	job := &batchv1.Job{}
	if err := reconciler.Get(context.Background(), types.NamespacedName{Name: aiSeedJobName, Namespace: "preview-pr-21"}, job); err != nil {
		t.Fatalf("expected completed ai-seed job to remain until TTL cleanup: %v", err)
	}
}

func TestReconcileAIJobReturnsFailedWhenJobFailed(t *testing.T) {
	scheme := testAIScheme(t)
	c := &platformv1alpha1.Preview{ObjectMeta: metav1.ObjectMeta{Name: "pr-21"}}
	existingJob := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: aiTestJobName, Namespace: "preview-pr-21"},
		Status: batchv1.JobStatus{
			Conditions: []batchv1.JobCondition{{
				Type:    batchv1.JobFailed,
				Status:  corev1.ConditionTrue,
				Message: "BackoffLimitExceeded",
			}},
		},
	}
	reconciler := &PreviewReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(c, existingJob).Build(),
		Scheme: scheme,
	}
	desired := reconciler.aiTestJob(c, "preview-pr-21")

	state, err := reconciler.reconcileAIJob(context.Background(), c, "preview-pr-21", aiTestJobName, desired, true)
	if err == nil {
		t.Fatal("expected error for failed job, got nil")
	}
	if state != phaseFailed {
		t.Errorf("expected Failed, got %q", state)
	}
}

func TestAIAPIKey_missingSecret(t *testing.T) {
	scheme := testAIScheme(t)
	c := &platformv1alpha1.Preview{
		Spec: platformv1alpha1.PreviewSpec{
			AIEnrichment: &platformv1alpha1.AIEnrichmentSpec{
				Enabled:      true,
				APISecretRef: &platformv1alpha1.SecretKeyRef{Name: "missing-secret"},
			},
		},
	}
	reconciler := &PreviewReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).Build(),
		Scheme: scheme,
	}
	_, err := reconciler.aiAPIKey(context.Background(), c)
	if err == nil {
		t.Fatal("expected error when secret is missing")
	}
}

func TestAIAPIKey_missingKeyInSecret(t *testing.T) {
	scheme := testAIScheme(t)
	c := &platformv1alpha1.Preview{
		Spec: platformv1alpha1.PreviewSpec{
			AIEnrichment: &platformv1alpha1.AIEnrichmentSpec{
				Enabled:      true,
				APISecretRef: &platformv1alpha1.SecretKeyRef{Name: "ai-api"},
			},
		},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "ai-api", Namespace: defaultAISecretNamespace},
		Data:       map[string][]byte{"wrong-key": []byte("value")},
	}
	reconciler := &PreviewReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(c, secret).Build(),
		Scheme: scheme,
	}
	_, err := reconciler.aiAPIKey(context.Background(), c)
	if err == nil {
		t.Fatal("expected error when key is missing from secret")
	}
}

func TestGenerateAndStoreAIContent_missingAPIURL(t *testing.T) {
	scheme := testAIScheme(t)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "ai-api", Namespace: defaultAISecretNamespace},
		Data:       map[string][]byte{defaultAISecretKey: []byte("sk-test")},
	}
	c := &platformv1alpha1.Preview{
		ObjectMeta: metav1.ObjectMeta{Name: "pr-21"},
		Spec: platformv1alpha1.PreviewSpec{
			AIEnrichment: &platformv1alpha1.AIEnrichmentSpec{
				Enabled:      true,
				APISecretRef: &platformv1alpha1.SecretKeyRef{Name: "ai-api"},
			},
		},
	}
	reconciler := &PreviewReconciler{
		Client:       fake.NewClientBuilder().WithScheme(scheme).WithObjects(c, secret).Build(),
		Scheme:       scheme,
		AIAPIBaseURL: "", // not configured
	}

	_, err := reconciler.generateAndStoreAIContent(context.Background(), c, "preview-pr-21")
	if err == nil {
		t.Fatal("expected error when AI API URL is not configured")
	}
	if !strings.Contains(err.Error(), "AI API URL") {
		t.Errorf("error should mention AI API URL, got: %v", err)
	}
}

func testAIScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := platformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to register platform scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to register core scheme: %v", err)
	}
	if err := batchv1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to register batch scheme: %v", err)
	}
	return scheme
}
