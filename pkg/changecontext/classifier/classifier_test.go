package classifier_test

import (
	"testing"

	platformv1alpha1 "github.com/ihsenalaya/preview-operator/api/v1alpha1"
	"github.com/ihsenalaya/preview-operator/pkg/changecontext/classifier"
)

func TestClassify(t *testing.T) {
	tests := []struct {
		path string
		want platformv1alpha1.ChangeFileType
	}{
		// Database migrations
		{"db/migrations/001_create_users.sql", platformv1alpha1.ChangeFileTypeDatabaseMigration},
		{"db/migrations/20260510_add_products.sql", platformv1alpha1.ChangeFileTypeDatabaseMigration},
		{"schema.sql", platformv1alpha1.ChangeFileTypeDatabaseMigration},
		// API contract
		{"api/openapi.yaml", platformv1alpha1.ChangeFileTypeAPIContract},
		{"api/openapi.yml", platformv1alpha1.ChangeFileTypeAPIContract},
		{"api/payment.proto", platformv1alpha1.ChangeFileTypeAPIContract},
		// Backend
		{"src/payments/service.ts", platformv1alpha1.ChangeFileTypeBackend},
		{"src/utils/helper.py", platformv1alpha1.ChangeFileTypeBackend},
		{"src/cmd/main.go", platformv1alpha1.ChangeFileTypeBackend},
		// Frontend
		{"web/src/components/Payment.tsx", platformv1alpha1.ChangeFileTypeFrontend},
		{"frontend/App.css", platformv1alpha1.ChangeFileTypeFrontend},
		{"styles.css", platformv1alpha1.ChangeFileTypeFrontend},
		{"Button.tsx", platformv1alpha1.ChangeFileTypeFrontend},
		// Docs
		{"docs/setup.md", platformv1alpha1.ChangeFileTypeDocs},
		{"README.md", platformv1alpha1.ChangeFileTypeDocs},
		// Other
		{".github/workflows/ci.yml", platformv1alpha1.ChangeFileTypeOther},
		{"Dockerfile", platformv1alpha1.ChangeFileTypeOther},
		{"go.mod", platformv1alpha1.ChangeFileTypeOther},
	}

	for _, tt := range tests {
		got := classifier.Classify(tt.path)
		if got != tt.want {
			t.Errorf("Classify(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestClassify_WindowsPath(t *testing.T) {
	got := classifier.Classify("db\\migrations\\001.sql")
	if got != platformv1alpha1.ChangeFileTypeDatabaseMigration {
		t.Errorf("expected database-migration for windows path, got %q", got)
	}
}

func TestDetectImpacts_Mixed(t *testing.T) {
	files := []platformv1alpha1.ChangedFile{
		{Path: "db/migrations/001.sql", Type: platformv1alpha1.ChangeFileTypeDatabaseMigration},
		{Path: "api/openapi.yaml", Type: platformv1alpha1.ChangeFileTypeAPIContract},
		{Path: "src/service.py", Type: platformv1alpha1.ChangeFileTypeBackend},
	}
	got := classifier.DetectImpacts(files)

	check := func(name string, val bool) {
		t.Helper()
		if !val {
			t.Errorf("expected %s=true", name)
		}
	}
	check("Database", got.Database)
	check("APIContract", got.APIContract)
	check("Backend", got.Backend)
	check("RequiresSeedData", got.RequiresSeedData)
	check("RequiresContractTests", got.RequiresContractTests)
	check("RequiresRegressionTests", got.RequiresRegressionTests)
	if got.Frontend {
		t.Error("expected Frontend=false")
	}
}

func TestDetectImpacts_FrontendOnly(t *testing.T) {
	files := []platformv1alpha1.ChangedFile{
		{Path: "web/App.tsx", Type: platformv1alpha1.ChangeFileTypeFrontend},
	}
	got := classifier.DetectImpacts(files)

	if got.Database || got.APIContract || got.Backend {
		t.Error("unexpected impact for frontend-only change")
	}
	if !got.Frontend {
		t.Error("expected Frontend=true")
	}
	if got.RequiresSeedData || got.RequiresContractTests || got.RequiresRegressionTests {
		t.Error("unexpected test requirement for frontend-only change")
	}
}

func TestDetectImpacts_Empty(t *testing.T) {
	got := classifier.DetectImpacts(nil)
	if got.Database || got.APIContract || got.Backend || got.Frontend {
		t.Error("expected all impacts false for empty file list")
	}
}
