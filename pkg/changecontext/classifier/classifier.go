package classifier

import (
	"path"
	"strings"

	platformv1alpha1 "github.com/ihsenalaya/preview-operator/api/v1alpha1"
)

// Classify returns the ChangeFileType for the given file path.
// Rules are applied in priority order; the first match wins.
func Classify(filePath string) platformv1alpha1.ChangeFileType {
	p := strings.ToLower(strings.ReplaceAll(filePath, "\\", "/"))
	ext := strings.ToLower(path.Ext(p))

	if strings.HasPrefix(p, "db/migrations/") || ext == ".sql" {
		return platformv1alpha1.ChangeFileTypeDatabaseMigration
	}

	if p == "api/openapi.yaml" || p == "api/openapi.yml" ||
		(strings.HasPrefix(p, "api/") && ext == ".proto") {
		return platformv1alpha1.ChangeFileTypeAPIContract
	}

	if strings.HasPrefix(p, "src/") && (ext == ".ts" || ext == ".py" || ext == ".go") {
		return platformv1alpha1.ChangeFileTypeBackend
	}

	if strings.HasPrefix(p, "web/") || strings.HasPrefix(p, "frontend/") ||
		ext == ".tsx" || ext == ".css" {
		return platformv1alpha1.ChangeFileTypeFrontend
	}

	if ext == ".md" || strings.HasPrefix(p, "docs/") {
		return platformv1alpha1.ChangeFileTypeDocs
	}

	return platformv1alpha1.ChangeFileTypeOther
}

// DetectImpacts derives a DetectedImpacts struct from a classified file list.
func DetectImpacts(files []platformv1alpha1.ChangedFile) platformv1alpha1.DetectedImpacts {
	var impacts platformv1alpha1.DetectedImpacts
	for _, f := range files {
		switch f.Type {
		case platformv1alpha1.ChangeFileTypeDatabaseMigration:
			impacts.Database = true
			impacts.RequiresSeedData = true
		case platformv1alpha1.ChangeFileTypeAPIContract:
			impacts.APIContract = true
			impacts.RequiresContractTests = true
		case platformv1alpha1.ChangeFileTypeBackend:
			impacts.Backend = true
			impacts.RequiresRegressionTests = true
		case platformv1alpha1.ChangeFileTypeFrontend:
			impacts.Frontend = true
		}
	}
	return impacts
}
