package extension

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	platformv1alpha1 "github.com/ihsenalaya/preview-operator/api/v1alpha1"
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
	cz, err := s.getPreview(ctx, name)
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
	cz, err := s.getPreview(ctx, name)
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
	cz, err := s.getPreview(ctx, name)
	if err != nil {
		http.Error(w, "preview not found", http.StatusNotFound)
		return
	}
	if cz.Spec.Database == nil || !cz.Spec.Database.Enabled {
		http.Error(w, "database is not enabled for this preview", http.StatusConflict)
		return
	}
	if err := validateCheckpointName(checkpoint); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !checkpointExists(cz, checkpoint) {
		http.Error(w, fmt.Sprintf("checkpoint %q not found", checkpoint), http.StatusNotFound)
		return
	}
	nsName := cz.Status.NamespaceName
	if nsName == "" {
		http.Error(w, "preview namespace is not ready", http.StatusConflict)
		return
	}

	// Run the restore directly with a uniquely-named Job instead of patching
	// spec.database.checkpointRestore and waiting for the operator. The operator
	// reuses a single "checkpoint-restore-<name>" Job, which races (AlreadyExists
	// / stale-Job reuse) when the e2e suite fires many reset_db() calls back to
	// back — that race is what made reset_db() time out. A fresh job name per
	// call removes the collision entirely.
	if err := s.runRestoreJob(ctx, cz, nsName, checkpoint); err != nil {
		http.Error(w, err.Error(), http.StatusGatewayTimeout)
		return
	}

	updated, err := s.getPreview(ctx, name)
	if err != nil {
		updated = cz
	}
	writeJSON(w, http.StatusOK, checkpointActionResponse{
		Preview:     name,
		Action:      "restore",
		Checkpoint:  checkpoint,
		Checkpoints: checkpointNames(updated),
	})
}

// runRestoreJob creates a uniquely-named Job that truncates the preview database
// and replays the named checkpoint dump, then waits for it to finish. Because the
// job name is unique per call there is never a collision between consecutive
// restores, so the operator's shared-job race condition cannot occur.
func (s *Server) runRestoreJob(ctx context.Context, cz *platformv1alpha1.Preview, nsName, checkpoint string) error {
	jobName := fmt.Sprintf("ext-restore-%s-%d", checkpoint, time.Now().UnixNano())
	if len(jobName) > 60 {
		jobName = jobName[:60]
	}

	job := restoreJobManifest(cz, nsName, jobName, checkpoint)
	created, err := s.kubeClient.BatchV1().Jobs(nsName).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("create restore job: %w", err)
	}
	defer func() {
		policy := metav1.DeletePropagationBackground
		_ = s.kubeClient.BatchV1().Jobs(nsName).Delete(
			context.Background(), created.Name, metav1.DeleteOptions{PropagationPolicy: &policy})
	}()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		j, err := s.kubeClient.BatchV1().Jobs(nsName).Get(ctx, created.Name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("poll restore job: %w", err)
		}
		if j.Status.Succeeded > 0 {
			return nil
		}
		for _, cond := range j.Status.Conditions {
			if cond.Type == batchv1.JobFailed && cond.Status == corev1.ConditionTrue {
				return fmt.Errorf("restore job failed: %s", cond.Message)
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for restore of checkpoint %q", checkpoint)
		case <-ticker.C:
		}
	}
}

// restoreJobManifest builds the one-shot Job that resets the preview database to
// a checkpoint: TRUNCATE every public table, then replay db-checkpoint-<name>.
func restoreJobManifest(cz *platformv1alpha1.Preview, nsName, jobName, checkpoint string) *batchv1.Job {
	pgVersion := "15"
	if cz.Spec.Database != nil && cz.Spec.Database.Version != "" {
		pgVersion = cz.Spec.Database.Version
	}
	backoff := int32(0)
	ttl := int32(120)
	script := strings.Join([]string{
		`set -e`,
		`tables=$(psql -h postgres -U "$POSTGRES_USER" -d "$POSTGRES_DB" -At -c "SELECT string_agg(format('%I.%I', schemaname, tablename), ',') FROM pg_tables WHERE schemaname = 'public'")`,
		`if [ -n "$tables" ]; then`,
		`  psql -h postgres -U "$POSTGRES_USER" -d "$POSTGRES_DB" -c "TRUNCATE TABLE $tables RESTART IDENTITY CASCADE"`,
		`fi`,
		`psql -h postgres -U "$POSTGRES_USER" -d "$POSTGRES_DB" -f /data/dump.sql`,
	}, "\n")

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: nsName,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "preview-extension",
				"platform.company.io/task":     "checkpoint-restore",
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoff,
			TTLSecondsAfterFinished: &ttl,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					InitContainers: []corev1.Container{{
						Name:    "wait-for-postgres",
						Image:   "busybox:1.36",
						Command: []string{"sh", "-c", "until nc -z postgres 5432; do echo 'waiting for postgres...'; sleep 2; done"},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("10m"),
								corev1.ResourceMemory: resource.MustParse("32Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("50m"),
								corev1.ResourceMemory: resource.MustParse("64Mi"),
							},
						},
					}},
					Containers: []corev1.Container{{
						Name:    "checkpoint-restore",
						Image:   "postgres:" + pgVersion + "-alpine",
						Command: []string{"sh", "-c", script},
						EnvFrom: []corev1.EnvFromSource{{
							SecretRef: &corev1.SecretEnvSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: "postgres-credentials"},
							},
						}},
						Env: []corev1.EnvVar{{
							Name: "PGPASSWORD",
							ValueFrom: &corev1.EnvVarSource{
								SecretKeyRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "postgres-credentials"},
									Key:                  "POSTGRES_PASSWORD",
								},
							},
						}},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("50m"),
								corev1.ResourceMemory: resource.MustParse("64Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("250m"),
								corev1.ResourceMemory: resource.MustParse("256Mi"),
							},
						},
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "checkpoint-data",
							MountPath: "/data",
						}},
					}},
					Volumes: []corev1.Volume{{
						Name: "checkpoint-data",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: "db-checkpoint-" + checkpoint},
								Items: []corev1.KeyToPath{{
									Key:  "dump.sql",
									Path: "dump.sql",
								}},
							},
						},
					}},
				},
			},
		},
	}
}

func (s *Server) startCheckpointSave(ctx context.Context, cz *platformv1alpha1.Preview, checkpoint string) error {
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

func (s *Server) startCheckpointRestore(ctx context.Context, cz *platformv1alpha1.Preview, checkpoint string) error {
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

func (s *Server) waitForCheckpointAction(ctx context.Context, name, checkpoint, action string) (*platformv1alpha1.Preview, error) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		cz, err := s.getPreview(ctx, name)
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

func checkpointActionDone(cz *platformv1alpha1.Preview, checkpoint, action string) bool {
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
