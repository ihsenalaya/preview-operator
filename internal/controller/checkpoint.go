package controller

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	platformv1alpha1 "github.com/ihsenalaya/preview-operator/api/v1alpha1"
)

const (
	checkpointConfigMapPrefix  = "db-checkpoint-"
	checkpointSaveJobPrefix    = "checkpoint-save-"
	checkpointRestoreJobPrefix = "checkpoint-restore-"
	checkpointConfigMapKey     = "dump.sql"
	checkpointMaxBytes         = 950 * 1024
	checkpointSaveTaskLabel    = "checkpoint-save"
	checkpointRestoreTaskLabel = "checkpoint-restore"

	suiteCheckpointName       = "after-seed"
	suiteCheckpointSaveJob    = "suite-checkpoint-save"
	suiteRestoreRegressionJob = "suite-restore-regression"
	suiteRestoreE2EJob        = "suite-restore-e2e"
)

var checkpointNamePattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

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

func checkpointConfigMapName(name string) string {
	return checkpointConfigMapPrefix + name
}

func checkpointSaveJobName(name string) string {
	return checkpointSaveJobPrefix + name
}

func checkpointRestoreJobName(name string) string {
	return checkpointRestoreJobPrefix + name
}

func (r *PreviewReconciler) reconcileCheckpoints(ctx context.Context, c *platformv1alpha1.Preview, nsName string) (bool, ctrl.Result, error) {
	if !databaseEnabled(c) || c.Spec.Database == nil {
		return false, ctrl.Result{}, nil
	}
	if c.Spec.Database.CheckpointSave != "" && c.Spec.Database.CheckpointRestore != "" {
		return true, ctrl.Result{}, fmt.Errorf("only one of checkpointSave or checkpointRestore can be set at a time")
	}

	names, err := r.listCheckpointNames(ctx, nsName)
	if err != nil {
		return false, ctrl.Result{}, err
	}
	if c.Status.Database != nil {
		c.Status.Database.Checkpoints = names
	}

	if c.Spec.Database.CheckpointSave != "" {
		result, err := r.reconcileCheckpointSave(ctx, c, nsName, c.Spec.Database.CheckpointSave)
		return true, result, err
	}
	if c.Spec.Database.CheckpointRestore != "" {
		result, err := r.reconcileCheckpointRestore(ctx, c, nsName, c.Spec.Database.CheckpointRestore)
		return true, result, err
	}
	return false, ctrl.Result{}, nil
}

func (r *PreviewReconciler) reconcileCheckpointSave(ctx context.Context, c *platformv1alpha1.Preview, nsName, checkpoint string) (ctrl.Result, error) {
	if err := validateCheckpointName(checkpoint); err != nil {
		return ctrl.Result{}, err
	}

	jobName := checkpointSaveJobName(checkpoint)
	job := &batchv1.Job{}
	err := r.Get(ctx, types.NamespacedName{Name: jobName, Namespace: nsName}, job)
	if errors.IsNotFound(err) {
		newJob := r.checkpointSaveJob(c, nsName, checkpoint)
		if err := controllerutil.SetControllerReference(c, newJob, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.Create(ctx, newJob); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
	}
	if err != nil {
		return ctrl.Result{}, err
	}
	if err := r.ensureControllerReference(ctx, c, job); err != nil {
		return ctrl.Result{}, err
	}

	for _, cond := range job.Status.Conditions {
		if cond.Type == batchv1.JobFailed && cond.Status == corev1.ConditionTrue {
			return ctrl.Result{}, fmt.Errorf("checkpoint save job %s/%s failed: %s", nsName, jobName, cond.Message)
		}
	}
	if job.Status.Succeeded == 0 {
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
	}

	dump, err := r.fetchCheckpointDump(ctx, nsName, jobName)
	if err != nil {
		return ctrl.Result{}, err
	}
	if err := r.storeCheckpointConfigMap(ctx, c, nsName, checkpoint, dump); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.Delete(ctx, job); err != nil && !errors.IsNotFound(err) {
		return ctrl.Result{}, err
	}
	if err := r.clearCheckpointRequest(ctx, c, nsName, true); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{Requeue: true}, nil
}

func (r *PreviewReconciler) reconcileCheckpointRestore(ctx context.Context, c *platformv1alpha1.Preview, nsName, checkpoint string) (ctrl.Result, error) {
	if err := validateCheckpointName(checkpoint); err != nil {
		return ctrl.Result{}, err
	}
	if _, err := r.ensureCheckpointConfigMap(ctx, c, nsName, checkpoint); err != nil {
		return ctrl.Result{}, err
	}

	jobName := checkpointRestoreJobName(checkpoint)
	job := &batchv1.Job{}
	err := r.Get(ctx, types.NamespacedName{Name: jobName, Namespace: nsName}, job)
	if errors.IsNotFound(err) {
		newJob := r.checkpointRestoreJob(c, nsName, checkpoint)
		if err := controllerutil.SetControllerReference(c, newJob, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.Create(ctx, newJob); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
	}
	if err != nil {
		return ctrl.Result{}, err
	}
	if err := r.ensureControllerReference(ctx, c, job); err != nil {
		return ctrl.Result{}, err
	}

	for _, cond := range job.Status.Conditions {
		if cond.Type == batchv1.JobFailed && cond.Status == corev1.ConditionTrue {
			return ctrl.Result{}, fmt.Errorf("checkpoint restore job %s/%s failed: %s", nsName, jobName, cond.Message)
		}
	}
	if job.Status.Succeeded == 0 {
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
	}

	if err := r.Delete(ctx, job); err != nil && !errors.IsNotFound(err) {
		return ctrl.Result{}, err
	}
	if err := r.clearCheckpointRequest(ctx, c, nsName, false); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{Requeue: true}, nil
}

func (r *PreviewReconciler) clearCheckpointRequest(ctx context.Context, c *platformv1alpha1.Preview, nsName string, saved bool) error {
	wasReady := c.Status.Database != nil && c.Status.Database.Ready
	base := client.MergeFrom(c.DeepCopy())
	if saved {
		c.Spec.Database.CheckpointSave = ""
	} else {
		c.Spec.Database.CheckpointRestore = ""
	}
	if err := r.Patch(ctx, c, base); err != nil {
		return err
	}

	if err := r.refreshPreview(ctx, types.NamespacedName{Name: c.Name, Namespace: c.Namespace}, c); err != nil {
		return err
	}
	names, err := r.listCheckpointNames(ctx, nsName)
	if err != nil {
		return err
	}
	r.setDatabaseStatus(c, wasReady)
	c.Status.ObservedGeneration = c.Generation
	c.Status.Database.Checkpoints = names
	return r.Status().Update(ctx, c)
}

func (r *PreviewReconciler) listCheckpointNames(ctx context.Context, nsName string) ([]string, error) {
	list := &corev1.ConfigMapList{}
	if err := r.List(ctx, list, client.InNamespace(nsName)); err != nil {
		return nil, err
	}

	var names []string
	for _, item := range list.Items {
		if strings.HasPrefix(item.Name, checkpointConfigMapPrefix) {
			names = append(names, strings.TrimPrefix(item.Name, checkpointConfigMapPrefix))
		}
	}
	sort.Strings(names)
	return names, nil
}

func (r *PreviewReconciler) ensureCheckpointConfigMap(ctx context.Context, c *platformv1alpha1.Preview, nsName, checkpoint string) (*corev1.ConfigMap, error) {
	cm := &corev1.ConfigMap{}
	key := types.NamespacedName{Name: checkpointConfigMapName(checkpoint), Namespace: nsName}
	if err := r.Get(ctx, key, cm); err != nil {
		if errors.IsNotFound(err) {
			return nil, fmt.Errorf("checkpoint %q not found", checkpoint)
		}
		return nil, err
	}
	if err := r.ensureControllerReference(ctx, c, cm); err != nil {
		return nil, err
	}
	return cm, nil
}

func (r *PreviewReconciler) fetchCheckpointDump(ctx context.Context, nsName, jobName string) (string, error) {
	if r.KubeClient == nil {
		return "", fmt.Errorf("kubernetes client is required to read checkpoint job logs")
	}
	podName := r.firstPodName(ctx, nsName, client.MatchingLabels{"job-name": jobName})
	if podName == "" {
		return "", fmt.Errorf("checkpoint job %s/%s succeeded but no pod logs were found", nsName, jobName)
	}

	req := r.KubeClient.CoreV1().Pods(nsName).GetLogs(podName, &corev1.PodLogOptions{
		Container: "checkpoint-save",
	})
	stream, err := req.Stream(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = stream.Close() }()

	data, err := io.ReadAll(io.LimitReader(stream, checkpointMaxBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) == 0 {
		return "", fmt.Errorf("checkpoint job %s/%s produced empty output", nsName, jobName)
	}
	if len(data) > checkpointMaxBytes {
		return "", fmt.Errorf("checkpoint dump exceeded %d bytes and cannot fit in a ConfigMap", checkpointMaxBytes)
	}
	return strings.TrimSpace(string(data)), nil
}

func (r *PreviewReconciler) storeCheckpointConfigMap(ctx context.Context, c *platformv1alpha1.Preview, nsName, checkpoint, dump string) error {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      checkpointConfigMapName(checkpoint),
			Namespace: nsName,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, cm, func() error {
		if err := controllerutil.SetControllerReference(c, cm, r.Scheme); err != nil {
			return err
		}
		cm.Labels = map[string]string{
			labelManagedBy:                "preview-operator",
			labelPreviewName:              c.Name,
			"app.kubernetes.io/component": "db-checkpoint",
		}
		cm.Data = map[string]string{
			checkpointConfigMapKey: dump,
		}
		return nil
	})
	return err
}

func (r *PreviewReconciler) checkpointSaveJob(c *platformv1alpha1.Preview, nsName, checkpoint string) *batchv1.Job {
	backoffLimit := int32(0)
	ttl := int32(300)
	image := fmt.Sprintf("postgres:%s-alpine", databaseVersion(c))

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      checkpointSaveJobName(checkpoint),
			Namespace: nsName,
			Labels: map[string]string{
				labelManagedBy:                "preview-operator",
				labelPreviewName:              c.Name,
				"app.kubernetes.io/component": "db-checkpoint",
				"platform.company.io/task":    checkpointSaveTaskLabel,
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &ttl,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						labelManagedBy:             "preview-operator",
						labelPreviewName:           c.Name,
						"platform.company.io/task": checkpointSaveTaskLabel,
					},
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{{
						Name:            "checkpoint-save",
						Image:           image,
						Command:         []string{"sh", "-c", `pg_dump --data-only --no-owner --no-privileges -h postgres -U "$POSTGRES_USER" "$POSTGRES_DB"`},
						ImagePullPolicy: corev1.PullIfNotPresent,
						EnvFrom: []corev1.EnvFromSource{{
							SecretRef: &corev1.SecretEnvSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: postgresSecretName},
							},
						}},
						Env: []corev1.EnvVar{{
							Name: "PGPASSWORD",
							ValueFrom: &corev1.EnvVarSource{
								SecretKeyRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: postgresSecretName},
									Key:                  "POSTGRES_PASSWORD",
								},
							},
						}},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse(aiJobCPURequest),
								corev1.ResourceMemory: resource.MustParse(aiJobMemoryRequest),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse(aiJobCPULimit),
								corev1.ResourceMemory: resource.MustParse(aiJobMemoryLimit),
							},
						},
					}},
				},
			},
		},
	}
}

func (r *PreviewReconciler) checkpointRestoreJob(c *platformv1alpha1.Preview, nsName, checkpoint string) *batchv1.Job {
	backoffLimit := int32(0)
	ttl := int32(300)
	image := fmt.Sprintf("postgres:%s-alpine", databaseVersion(c))
	jobName := checkpointRestoreJobName(checkpoint)

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: nsName,
			Labels: map[string]string{
				labelManagedBy:                "preview-operator",
				labelPreviewName:              c.Name,
				"app.kubernetes.io/component": "db-checkpoint",
				"platform.company.io/task":    checkpointRestoreTaskLabel,
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &ttl,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						labelManagedBy:             "preview-operator",
						labelPreviewName:           c.Name,
						"platform.company.io/task": checkpointRestoreTaskLabel,
					},
				},
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
						Name:            "checkpoint-restore",
						Image:           image,
						Command:         []string{"sh", "-c", restoreCheckpointScript()},
						ImagePullPolicy: corev1.PullIfNotPresent,
						EnvFrom: []corev1.EnvFromSource{{
							SecretRef: &corev1.SecretEnvSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: postgresSecretName},
							},
						}},
						Env: []corev1.EnvVar{{
							Name: "PGPASSWORD",
							ValueFrom: &corev1.EnvVarSource{
								SecretKeyRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: postgresSecretName},
									Key:                  "POSTGRES_PASSWORD",
								},
							},
						}},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse(aiJobCPURequest),
								corev1.ResourceMemory: resource.MustParse(aiJobMemoryRequest),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse(aiJobCPULimit),
								corev1.ResourceMemory: resource.MustParse(aiJobMemoryLimit),
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
								LocalObjectReference: corev1.LocalObjectReference{Name: checkpointConfigMapName(checkpoint)},
								Items: []corev1.KeyToPath{{
									Key:  checkpointConfigMapKey,
									Path: checkpointConfigMapKey,
								}},
							},
						},
					}},
				},
			},
		},
	}
}

func restoreCheckpointScript() string {
	return strings.Join([]string{
		`set -e`,
		`tables=$(psql -h postgres -U "$POSTGRES_USER" -d "$POSTGRES_DB" -At -c "SELECT string_agg(format('%I.%I', schemaname, tablename), ',') FROM pg_tables WHERE schemaname = 'public'")`,
		`if [ -n "$tables" ]; then`,
		`  psql -h postgres -U "$POSTGRES_USER" -d "$POSTGRES_DB" -c "TRUNCATE TABLE $tables RESTART IDENTITY CASCADE"`,
		`fi`,
		`psql -h postgres -U "$POSTGRES_USER" -d "$POSTGRES_DB" -f /data/dump.sql`,
	}, "\n")
}

// ensureSuiteCheckpointSaved creates and waits for the suite checkpoint-save job.
// Returns (true, nil) once the dump is stored in the ConfigMap.
func (r *PreviewReconciler) ensureSuiteCheckpointSaved(ctx context.Context, c *platformv1alpha1.Preview, nsName string) (bool, error) {
	// Idempotent: if ConfigMap already exists, we're done.
	cmKey := types.NamespacedName{Name: checkpointConfigMapName(suiteCheckpointName), Namespace: nsName}
	if err := r.Get(ctx, cmKey, &corev1.ConfigMap{}); err == nil {
		return true, nil
	}

	job := &batchv1.Job{}
	err := r.Get(ctx, types.NamespacedName{Name: suiteCheckpointSaveJob, Namespace: nsName}, job)
	if errors.IsNotFound(err) {
		newJob := r.checkpointSaveJob(c, nsName, suiteCheckpointName)
		newJob.Name = suiteCheckpointSaveJob
		if err := controllerutil.SetControllerReference(c, newJob, r.Scheme); err != nil {
			return false, err
		}
		return false, r.Create(ctx, newJob)
	}
	if err != nil {
		return false, err
	}
	for _, cond := range job.Status.Conditions {
		if cond.Type == batchv1.JobFailed && cond.Status == corev1.ConditionTrue {
			return false, fmt.Errorf("suite checkpoint save job failed: %s", cond.Message)
		}
	}
	if job.Status.Succeeded == 0 {
		return false, nil
	}
	dump, err := r.fetchCheckpointDump(ctx, nsName, suiteCheckpointSaveJob)
	if err != nil {
		return false, err
	}
	return true, r.storeCheckpointConfigMap(ctx, c, nsName, suiteCheckpointName, dump)
}

// ensureSuiteCheckpointRestored creates and waits for a restore job identified by jobName.
// Returns (true, nil) once the restore job has succeeded.
//
// Dispatches on Spec.Database.IsolationMode:
//   - "restore" (default): pg_dump + psql restore via the saved ConfigMap
//   - "migration": DROP SCHEMA public CASCADE + replay the user migration command
//
// Both produce the same effect (clean DB ready for the next suite); migration mode
// is the baseline for RQ3 comparison.
func (r *PreviewReconciler) ensureSuiteCheckpointRestored(ctx context.Context, c *platformv1alpha1.Preview, nsName, jobName string) (bool, error) {
	if isolationMode(c) == "migration" {
		return r.ensureSuiteMigrationReplayed(ctx, c, nsName, jobName)
	}
	if _, err := r.ensureCheckpointConfigMap(ctx, c, nsName, suiteCheckpointName); err != nil {
		return false, fmt.Errorf("suite checkpoint not found: %w", err)
	}
	job := &batchv1.Job{}
	err := r.Get(ctx, types.NamespacedName{Name: jobName, Namespace: nsName}, job)
	if errors.IsNotFound(err) {
		newJob := r.checkpointRestoreJob(c, nsName, suiteCheckpointName)
		newJob.Name = jobName
		if err := controllerutil.SetControllerReference(c, newJob, r.Scheme); err != nil {
			return false, err
		}
		return false, r.Create(ctx, newJob)
	}
	if err != nil {
		return false, err
	}
	for _, cond := range job.Status.Conditions {
		if cond.Type == batchv1.JobFailed && cond.Status == corev1.ConditionTrue {
			return false, fmt.Errorf("suite checkpoint restore job %s failed: %s", jobName, cond.Message)
		}
	}
	return job.Status.Succeeded > 0, nil
}

// migrationReplayDropSchemaScript wipes the application data so the subsequent
// container can re-run the user's migration on a clean DB. Used in the init
// container of migrationReplayJob.
func migrationReplayDropSchemaScript() string {
	return strings.Join([]string{
		`set -e`,
		`# Wipe all application data via schema drop + recreate (faster + more`,
		`# thorough than per-table TRUNCATE for cross-FK graphs).`,
		`psql -h postgres -U "$POSTGRES_USER" -d "$POSTGRES_DB" -c "DROP SCHEMA public CASCADE; CREATE SCHEMA public; GRANT ALL ON SCHEMA public TO \"$POSTGRES_USER\"; GRANT ALL ON SCHEMA public TO public;"`,
	}, "\n")
}

// ensureSuiteMigrationReplayed creates and waits for a migration-replay Job
// identified by jobName. Mirrors ensureSuiteCheckpointRestored but uses
// DROP SCHEMA + migration replay instead of pg_dump restore.
//
// Used as the RQ3 baseline (paper §6) — measured ~37.6s per cycle vs ~14.6s
// for checkpoint restore (~2.57× more expensive, same isolation outcome).
func (r *PreviewReconciler) ensureSuiteMigrationReplayed(ctx context.Context, c *platformv1alpha1.Preview, nsName, jobName string) (bool, error) {
	job := &batchv1.Job{}
	err := r.Get(ctx, types.NamespacedName{Name: jobName, Namespace: nsName}, job)
	if errors.IsNotFound(err) {
		newJob := r.migrationReplayJob(c, nsName, jobName)
		if err := controllerutil.SetControllerReference(c, newJob, r.Scheme); err != nil {
			return false, err
		}
		return false, r.Create(ctx, newJob)
	}
	if err != nil {
		return false, err
	}
	for _, cond := range job.Status.Conditions {
		if cond.Type == batchv1.JobFailed && cond.Status == corev1.ConditionTrue {
			return false, fmt.Errorf("suite migration replay job %s failed: %s", jobName, cond.Message)
		}
	}
	return job.Status.Succeeded > 0, nil
}

// migrationReplayJob builds a Job that drops the public schema (init container,
// postgres image) and then replays the user-provided migration command (main
// container, user image) — same shape as databaseTaskJob.
//
// Falls back to a noop main container if Spec.Database.Migration is nil or
// empty (defensive: replay is meaningless without a migration command, but
// the schema-drop init still happens to wipe data).
func (r *PreviewReconciler) migrationReplayJob(c *platformv1alpha1.Preview, nsName, jobName string) *batchv1.Job {
	backoffLimit := int32(0)
	ttl := int32(300)
	pgImage := fmt.Sprintf("postgres:%s-alpine", databaseVersion(c))

	// User migration spec — main container uses these.
	var (
		userCmd   []string
		userArgs  []string
		userImage string
	)
	if c.Spec.Database != nil && c.Spec.Database.Migration != nil {
		userCmd = c.Spec.Database.Migration.Command
		userArgs = c.Spec.Database.Migration.Args
		userImage = c.Spec.Database.Migration.Image
	}
	if userImage == "" {
		userImage = c.Spec.Image
	}
	if len(userCmd) == 0 {
		// Defensive fallback: just a no-op so the Job can complete after the init.
		userCmd = []string{"sh", "-c", "echo 'migrationReplayJob: no migration command provided; schema dropped only'"}
	}

	commonEnv := []corev1.EnvVar{
		{Name: "POSTGRES_USER", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: postgresSecretName}, Key: "POSTGRES_USER"}}},
		{Name: "POSTGRES_PASSWORD", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: postgresSecretName}, Key: "POSTGRES_PASSWORD"}}},
		{Name: "POSTGRES_DB", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: postgresSecretName}, Key: "POSTGRES_DB"}}},
		{Name: "DATABASE_URL", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: postgresSecretName}, Key: "DATABASE_URL"}}},
	}
	psqlEnv := append([]corev1.EnvVar{
		{Name: "PGPASSWORD", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: postgresSecretName}, Key: "POSTGRES_PASSWORD"}}},
	}, commonEnv...)

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: nsName,
			Labels: map[string]string{
				labelManagedBy:                "preview-operator",
				labelPreviewName:              c.Name,
				"app.kubernetes.io/component": "db-migration-replay",
				"platform.company.io/task":    "migration-replay",
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &ttl,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						labelManagedBy:             "preview-operator",
						labelPreviewName:           c.Name,
						"platform.company.io/task": "migration-replay",
					},
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					InitContainers: []corev1.Container{
						{
							Name:    "wait-for-postgres",
							Image:   "busybox:1.36",
							Command: []string{"sh", "-c", "until nc -z postgres 5432; do echo 'waiting for postgres...'; sleep 2; done"},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("10m"), corev1.ResourceMemory: resource.MustParse("32Mi")},
								Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("50m"), corev1.ResourceMemory: resource.MustParse("64Mi")},
							},
						},
						{
							Name:            "drop-schema",
							Image:           pgImage,
							Command:         []string{"sh", "-c", migrationReplayDropSchemaScript()},
							ImagePullPolicy: corev1.PullIfNotPresent,
							Env:             psqlEnv,
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("50m"), corev1.ResourceMemory: resource.MustParse("64Mi")},
								Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("200m"), corev1.ResourceMemory: resource.MustParse("128Mi")},
							},
						},
					},
					Containers: []corev1.Container{{
						Name:    "migration-replay",
						Image:   userImage,
						Command: userCmd,
						Args:    userArgs,
						Env:     commonEnv,
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse(aiJobCPURequest),
								corev1.ResourceMemory: resource.MustParse(aiJobMemoryRequest),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse(aiJobCPULimit),
								corev1.ResourceMemory: resource.MustParse(aiJobMemoryLimit),
							},
						},
					}},
				},
			},
		},
	}
}

// ensureSuiteCheckpointSaved becomes a no-op in migration mode — no dump is needed
// since the inter-suite reset re-runs the migration command instead of restoring.
// In restore mode, the original save flow is unchanged.
func (r *PreviewReconciler) ensureSuiteCheckpointSavedOrSkipped(ctx context.Context, c *platformv1alpha1.Preview, nsName string) (bool, error) {
	if isolationMode(c) == "migration" {
		return true, nil
	}
	return r.ensureSuiteCheckpointSaved(ctx, c, nsName)
}
