package controller

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	platformv1alpha1 "github.com/company/cellenza-operator/api/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
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
	reconciler := &CellenzaReconciler{
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
	c := &platformv1alpha1.Cellenza{
		Spec: platformv1alpha1.CellenzaSpec{
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
	reconciler := &CellenzaReconciler{}
	diff, err := reconciler.fetchPRDiff(context.Background(), &platformv1alpha1.Cellenza{}, "token")
	if err != nil {
		t.Fatalf("fetchPRDiff returned error: %v", err)
	}
	if diff != "" {
		t.Fatalf("expected empty diff, got %q", diff)
	}
}

func TestAISchemaDumpJobBuildsExpectedSpec(t *testing.T) {
	c := &platformv1alpha1.Cellenza{ObjectMeta: metav1.ObjectMeta{Name: "pr-21"}}
	reconciler := &CellenzaReconciler{}

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
}

func TestFetchDBSchemaCreatesJobWhenMissing(t *testing.T) {
	scheme := testAIScheme(t)
	c := &platformv1alpha1.Cellenza{
		ObjectMeta: metav1.ObjectMeta{Name: "pr-21"},
		Spec: platformv1alpha1.CellenzaSpec{
			Database: &platformv1alpha1.DatabaseSpec{Enabled: true},
		},
	}
	reconciler := &CellenzaReconciler{
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

func TestGenerateAndStoreAIContentSkipsWhenConfigMapExists(t *testing.T) {
	scheme := testAIScheme(t)
	c := &platformv1alpha1.Cellenza{
		ObjectMeta: metav1.ObjectMeta{Name: "pr-21"},
		Spec: platformv1alpha1.CellenzaSpec{
			AIEnrichment: &platformv1alpha1.AIEnrichmentSpec{Enabled: true},
		},
	}
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: aiEnrichmentConfigMap, Namespace: "preview-pr-21"}}
	reconciler := &CellenzaReconciler{
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
	c := &platformv1alpha1.Cellenza{
		ObjectMeta: metav1.ObjectMeta{Name: "pr-21"},
		Spec: platformv1alpha1.CellenzaSpec{
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
	reconciler := &CellenzaReconciler{
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
