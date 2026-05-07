package extension

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	platformv1alpha1 "github.com/company/cellenza-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestCmdStatusShowsAIState(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := platformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to register scheme: %v", err)
	}

	cz := &platformv1alpha1.Cellenza{
		ObjectMeta: metav1.ObjectMeta{Name: "pr-42"},
		Spec: platformv1alpha1.CellenzaSpec{
			Branch:       "feature/ai",
			PRNumber:     42,
			ResourceTier: platformv1alpha1.TierMedium,
			Replicas:     1,
			AIEnrichment: &platformv1alpha1.AIEnrichmentSpec{
				Enabled: true,
			},
		},
		Status: platformv1alpha1.CellenzaStatus{
			Phase: platformv1alpha1.PhaseRunning,
			AIEnrichment: &platformv1alpha1.AIEnrichmentStatus{
				Phase:       "Failed",
				SeedStatus:  "Succeeded",
				TestsStatus: "Failed",
				TestResults: []string{"FAIL GET /messages - 500"},
				Error:       "AI test job failed",
			},
		},
	}

	server := &Server{
		crClient: fake.NewClientBuilder().WithScheme(scheme).WithObjects(cz).Build(),
	}

	body := server.cmdStatus(context.Background(), []string{"pr-42"})
	for _, want := range []string{
		"**IA:**",
		"- Phase: Failed",
		"- Seed: Succeeded",
		"- Tests: Failed",
		"- FAIL GET /messages - 500",
		"- Erreur: AI test job failed",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("cmdStatus output missing %q:\n%s", want, body)
		}
	}
}

func TestCmdStatusShowsAIRerunMode(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := platformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to register scheme: %v", err)
	}

	cz := &platformv1alpha1.Cellenza{
		ObjectMeta: metav1.ObjectMeta{Name: "pr-42"},
		Spec: platformv1alpha1.CellenzaSpec{
			Branch:       "feature/ai",
			PRNumber:     42,
			ResourceTier: platformv1alpha1.TierMedium,
			Replicas:     1,
			AIEnrichment: &platformv1alpha1.AIEnrichmentSpec{
				Enabled: true,
			},
		},
		Status: platformv1alpha1.CellenzaStatus{
			Phase: platformv1alpha1.PhaseRunning,
			AIEnrichment: &platformv1alpha1.AIEnrichmentStatus{
				Phase:     "Running",
				RerunOnly: true,
			},
		},
	}

	server := &Server{
		crClient: fake.NewClientBuilder().WithScheme(scheme).WithObjects(cz).Build(),
	}

	body := server.cmdStatus(context.Background(), []string{"pr-42"})
	if !strings.Contains(body, "AI-only rerun") {
		t.Fatalf("cmdStatus output missing rerun mode:\n%s", body)
	}
}

func TestCmdRetestAIRequestsOperatorManagedRerun(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := platformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to register scheme: %v", err)
	}

	cz := &platformv1alpha1.Cellenza{
		ObjectMeta: metav1.ObjectMeta{Name: "pr-42"},
		Spec: platformv1alpha1.CellenzaSpec{
			PRNumber: 42,
			Database: &platformv1alpha1.DatabaseSpec{Enabled: true},
			AIEnrichment: &platformv1alpha1.AIEnrichmentSpec{
				Enabled: true,
			},
		},
	}

	server := &Server{
		crClient: fake.NewClientBuilder().WithScheme(scheme).WithObjects(cz).Build(),
	}

	body := server.cmdRetestAI(context.Background(), []string{"pr-42"})
	for _, want := range []string{
		"**Rerun IA lancé**",
		"Rejouer migration + seed de la base",
		"Ignorer le test suite standard pour ce cycle",
		"Rejouer `ai-seed` puis `ai-tests`",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("cmdRetestAI output missing %q:\n%s", want, body)
		}
	}

	updated := &platformv1alpha1.Cellenza{}
	if err := server.crClient.Get(context.Background(), client.ObjectKey{Name: "pr-42"}, updated); err != nil {
		t.Fatalf("failed to fetch updated cellenza: %v", err)
	}
	if updated.Spec.AIEnrichment == nil || !updated.Spec.AIEnrichment.RerunRequested {
		t.Fatalf("expected rerunRequested to be set, got %#v", updated.Spec.AIEnrichment)
	}
}

func TestCmdRetestAIRejectsWhenAIDisabled(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := platformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to register scheme: %v", err)
	}

	cz := &platformv1alpha1.Cellenza{
		ObjectMeta: metav1.ObjectMeta{Name: "pr-42"},
	}

	server := &Server{
		crClient: fake.NewClientBuilder().WithScheme(scheme).WithObjects(cz).Build(),
	}

	body := server.cmdRetestAI(context.Background(), []string{"pr-42"})
	if !strings.Contains(body, "n'a pas l'enrichissement IA activé") {
		t.Fatalf("unexpected response:\n%s", body)
	}
}

func TestCmdListCheckpoints(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := platformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to register scheme: %v", err)
	}

	cz := &platformv1alpha1.Cellenza{
		ObjectMeta: metav1.ObjectMeta{Name: "pr-42"},
		Spec: platformv1alpha1.CellenzaSpec{
			Database: &platformv1alpha1.DatabaseSpec{Enabled: true},
		},
		Status: platformv1alpha1.CellenzaStatus{
			Database: &platformv1alpha1.DatabaseStatus{
				Checkpoints: []string{"post-seed", "post-order"},
			},
		},
	}

	server := &Server{
		crClient: fake.NewClientBuilder().WithScheme(scheme).WithObjects(cz).Build(),
	}

	body := server.cmdListCheckpoints(context.Background(), []string{"pr-42"})
	for _, want := range []string{"post-seed", "post-order"} {
		if !strings.Contains(body, want) {
			t.Fatalf("cmdListCheckpoints output missing %q:\n%s", want, body)
		}
	}
}

func TestCheckpointAPIList(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := platformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to register scheme: %v", err)
	}

	cz := &platformv1alpha1.Cellenza{
		ObjectMeta: metav1.ObjectMeta{Name: "pr-42"},
		Spec: platformv1alpha1.CellenzaSpec{
			Database: &platformv1alpha1.DatabaseSpec{Enabled: true},
		},
		Status: platformv1alpha1.CellenzaStatus{
			Database: &platformv1alpha1.DatabaseStatus{
				Checkpoints: []string{"post-seed"},
			},
		},
	}

	server := &Server{
		crClient: fake.NewClientBuilder().WithScheme(scheme).WithObjects(cz).Build(),
	}

	req := httptest.NewRequest(http.MethodGet, "/api/previews/pr-42/checkpoints", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}

	var body checkpointListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(body.Checkpoints) != 1 || body.Checkpoints[0] != "post-seed" {
		t.Fatalf("unexpected checkpoints: %#v", body.Checkpoints)
	}
}
