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
