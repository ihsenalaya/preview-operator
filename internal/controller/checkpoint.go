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

	platformv1alpha1 "github.com/company/cellenza-operator/api/v1alpha1"
)

const (
	checkpointConfigMapPrefix  = "db-checkpoint-"
	checkpointSaveJobPrefix    = "checkpoint-save-"
	checkpointRestoreJobPrefix = "checkpoint-restore-"
	checkpointConfigMapKey     = "dump.sql"
	checkpointMaxBytes         = 950 * 1024
	checkpointSaveTaskLabel    = "checkpoint-save"
	checkpointRestoreTaskLabel = "checkpoint-restore"
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

func (r *CellenzaReconciler) reconcileCheckpoints(ctx context.Context, c *platformv1alpha1.Cellenza, nsName string) (bool, ctrl.Result, error) {
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

func (r *CellenzaReconciler) reconcileCheckpointSave(ctx context.Context, c *platformv1alpha1.Cellenza, nsName, checkpoint string) (ctrl.Result, error) {
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

func (r *CellenzaReconciler) reconcileCheckpointRestore(ctx context.Context, c *platformv1alpha1.Cellenza, nsName, checkpoint string) (ctrl.Result, error) {
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

func (r *CellenzaReconciler) clearCheckpointRequest(ctx context.Context, c *platformv1alpha1.Cellenza, nsName string, saved bool) error {
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

	if err := r.refreshCellenza(ctx, types.NamespacedName{Name: c.Name, Namespace: c.Namespace}, c); err != nil {
		return err
	}
	names, err := r.listCheckpointNames(ctx, nsName)
	if err != nil {
		return err
	}
	r.setDatabaseStatus(c, wasReady)
	c.Status.Database.Checkpoints = names
	return r.Status().Update(ctx, c)
}

func (r *CellenzaReconciler) listCheckpointNames(ctx context.Context, nsName string) ([]string, error) {
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

func (r *CellenzaReconciler) ensureCheckpointConfigMap(ctx context.Context, c *platformv1alpha1.Cellenza, nsName, checkpoint string) (*corev1.ConfigMap, error) {
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

func (r *CellenzaReconciler) fetchCheckpointDump(ctx context.Context, nsName, jobName string) (string, error) {
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

func (r *CellenzaReconciler) storeCheckpointConfigMap(ctx context.Context, c *platformv1alpha1.Cellenza, nsName, checkpoint, dump string) error {
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
			labelManagedBy:                "cellenza-operator",
			labelCellenzaName:             c.Name,
			"app.kubernetes.io/component": "db-checkpoint",
		}
		cm.Data = map[string]string{
			checkpointConfigMapKey: dump,
		}
		return nil
	})
	return err
}

func (r *CellenzaReconciler) checkpointSaveJob(c *platformv1alpha1.Cellenza, nsName, checkpoint string) *batchv1.Job {
	backoffLimit := int32(0)
	ttl := int32(300)
	image := fmt.Sprintf("postgres:%s-alpine", databaseVersion(c))

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      checkpointSaveJobName(checkpoint),
			Namespace: nsName,
			Labels: map[string]string{
				labelManagedBy:                "cellenza-operator",
				labelCellenzaName:             c.Name,
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
						labelManagedBy:             "cellenza-operator",
						labelCellenzaName:          c.Name,
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

func (r *CellenzaReconciler) checkpointRestoreJob(c *platformv1alpha1.Cellenza, nsName, checkpoint string) *batchv1.Job {
	backoffLimit := int32(0)
	ttl := int32(300)
	image := fmt.Sprintf("postgres:%s-alpine", databaseVersion(c))
	jobName := checkpointRestoreJobName(checkpoint)

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: nsName,
			Labels: map[string]string{
				labelManagedBy:                "cellenza-operator",
				labelCellenzaName:             c.Name,
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
						labelManagedBy:             "cellenza-operator",
						labelCellenzaName:          c.Name,
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
