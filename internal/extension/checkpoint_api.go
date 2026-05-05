package extension

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"

	platformv1alpha1 "github.com/company/cellenza-operator/api/v1alpha1"
)

var checkpointNamePattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

type checkpointListResponse struct {
	Checkpoints []string `json:"checkpoints"`
}

type checkpointActionResponse struct {
	Preview     string   `json:"preview"`
	Action      string   `json:"action"`
	Checkpoint  string   `json:"checkpoint"`
	Checkpoints []string `json:"checkpoints,omitempty"`
}

func validateCheckpointName(name string) error {
	if len(name) == 0 {
		return fmt.Errorf("checkpoint name is required")
	}
	if len(name) > 48 {
		return fmt.Errorf("checkpoint name %q is too long (max 48 chars)", name)
	}
	if !checkpointNamePattern.MatchString(name) {
		return fmt.Errorf("checkpoint name %q must match %s", name, checkpointNamePattern.String())
	}
	return nil
}

func (s *Server) serveCheckpointAPI(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()

	name, checkpoint, restore, ok := parseCheckpointAPIPath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}

	switch {
	case r.Method == http.MethodGet && checkpoint == "" && !restore:
		s.handleCheckpointList(ctx, w, name)
	case r.Method == http.MethodPost && checkpoint != "" && !restore:
		s.handleCheckpointSave(ctx, w, name, checkpoint)
	case r.Method == http.MethodPost && checkpoint != "" && restore:
		s.handleCheckpointRestore(ctx, w, name, checkpoint)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func parseCheckpointAPIPath(path string) (name, checkpoint string, restore, ok bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 4 || parts[0] != "api" || parts[1] != "previews" || parts[3] != "checkpoints" {
		return "", "", false, false
	}
	name = parts[2]
	if len(parts) == 4 {
		return name, "", false, true
	}
	if len(parts) == 5 {
		return name, parts[4], false, true
	}
	if len(parts) == 6 && parts[5] == "restore" {
		return name, parts[4], true, true
	}
	return "", "", false, false
}

func (s *Server) handleCheckpointList(ctx context.Context, w http.ResponseWriter, name string) {
	cz, err := s.getCellenza(ctx, name)
	if err != nil {
		http.Error(w, "preview not found", http.StatusNotFound)
		return
	}
	if cz.Spec.Database == nil || !cz.Spec.Database.Enabled {
		http.Error(w, "database is not enabled for this preview", http.StatusConflict)
		return
	}
	writeJSON(w, http.StatusOK, checkpointListResponse{Checkpoints: checkpointNames(cz)})
}

func (s *Server) handleCheckpointSave(ctx context.Context, w http.ResponseWriter, name, checkpoint string) {
	cz, err := s.getCellenza(ctx, name)
	if err != nil {
		http.Error(w, "preview not found", http.StatusNotFound)
		return
	}
	if err := s.startCheckpointSave(ctx, cz, checkpoint); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	updated, err := s.waitForCheckpointAction(ctx, name, checkpoint, "save")
	if err != nil {
		http.Error(w, err.Error(), http.StatusGatewayTimeout)
		return
	}
	writeJSON(w, http.StatusOK, checkpointActionResponse{
		Preview:     name,
		Action:      "save",
		Checkpoint:  checkpoint,
		Checkpoints: checkpointNames(updated),
	})
}

func (s *Server) handleCheckpointRestore(ctx context.Context, w http.ResponseWriter, name, checkpoint string) {
	cz, err := s.getCellenza(ctx, name)
	if err != nil {
		http.Error(w, "preview not found", http.StatusNotFound)
		return
	}
	if err := s.startCheckpointRestore(ctx, cz, checkpoint); err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}
	updated, err := s.waitForCheckpointAction(ctx, name, checkpoint, "restore")
	if err != nil {
		http.Error(w, err.Error(), http.StatusGatewayTimeout)
		return
	}
	writeJSON(w, http.StatusOK, checkpointActionResponse{
		Preview:     name,
		Action:      "restore",
		Checkpoint:  checkpoint,
		Checkpoints: checkpointNames(updated),
	})
}

func (s *Server) startCheckpointSave(ctx context.Context, cz *platformv1alpha1.Cellenza, checkpoint string) error {
	if cz.Spec.Database == nil || !cz.Spec.Database.Enabled {
		return fmt.Errorf("database is not enabled for this preview")
	}
	if err := validateCheckpointName(checkpoint); err != nil {
		return err
	}
	patch := client.MergeFrom(cz.DeepCopy())
	cz.Spec.Database.CheckpointSave = checkpoint
	cz.Spec.Database.CheckpointRestore = ""
	return s.crClient.Patch(ctx, cz, patch)
}

func (s *Server) startCheckpointRestore(ctx context.Context, cz *platformv1alpha1.Cellenza, checkpoint string) error {
	if cz.Spec.Database == nil || !cz.Spec.Database.Enabled {
		return fmt.Errorf("database is not enabled for this preview")
	}
	if err := validateCheckpointName(checkpoint); err != nil {
		return err
	}
	if !checkpointExists(cz, checkpoint) {
		return fmt.Errorf("checkpoint %q not found", checkpoint)
	}
	patch := client.MergeFrom(cz.DeepCopy())
	cz.Spec.Database.CheckpointRestore = checkpoint
	cz.Spec.Database.CheckpointSave = ""
	return s.crClient.Patch(ctx, cz, patch)
}

func (s *Server) waitForCheckpointAction(ctx context.Context, name, checkpoint, action string) (*platformv1alpha1.Cellenza, error) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		cz, err := s.getCellenza(ctx, name)
		if err != nil {
			return nil, err
		}
		if checkpointActionDone(cz, checkpoint, action) {
			return cz, nil
		}
		if cz.Status.Phase == platformv1alpha1.PhaseFailed {
			if cz.Status.Diagnostics != nil && cz.Status.Diagnostics.Message != "" {
				return nil, fmt.Errorf("checkpoint %s failed: %s", action, cz.Status.Diagnostics.Message)
			}
			return nil, fmt.Errorf("checkpoint %s failed", action)
		}

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("timeout waiting for checkpoint %s %q", action, checkpoint)
		case <-ticker.C:
		}
	}
}

func checkpointActionDone(cz *platformv1alpha1.Cellenza, checkpoint, action string) bool {
	if cz.Spec.Database == nil {
		return false
	}
	if action == "save" {
		return cz.Spec.Database.CheckpointSave == ""
	}
	return cz.Spec.Database.CheckpointRestore == ""
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
