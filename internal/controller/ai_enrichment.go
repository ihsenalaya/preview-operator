package controller

import (
	"context"
	"fmt"
	"io"
	"net/http"
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
	"github.com/company/cellenza-operator/internal/ai"
)

const (
	aiEnrichmentConfigMap    = "ai-enrichment"
	aiSeedJobName            = "ai-seed"
	aiTestJobName            = "ai-tests"
	aiSchemaJobName          = "ai-schema-dump"
	aiSystemPromptConfigMap  = "ai-prompt-template"
	defaultAISecretNamespace = "cellenza-operator-system"
	defaultAISecretKey       = "api-key"
	defaultAISchemaDumpImage = "postgres:15-alpine"
	defaultAIInternalAppURL  = "http://app:80"
	defaultAITestImage       = "python:3.12-slim"
	aiPromptConfigMapKey     = "instructions"
	aiSystemPromptKey        = "ai-system-prompt.txt"

	phaseSucceeded  = "Succeeded"
	phaseFailed     = "Failed"
	phaseRunning    = "Running"
	phaseSkipped    = "Skipped"
	phaseGenerating = "Generating"
	phasePending    = "Pending"
)

func aiPromptConfigMapName(czName string) string {
	return "ai-prompt-" + czName
}

func (r *CellenzaReconciler) settingsReader() client.Reader {
	if r.APIReader != nil {
		return r.APIReader
	}
	return r.Client
}

func (r *CellenzaReconciler) operatorNamespace() string {
	if strings.TrimSpace(r.OperatorNamespace) == "" {
		return defaultAISecretNamespace
	}
	return strings.TrimSpace(r.OperatorNamespace)
}

func (r *CellenzaReconciler) fetchSystemPrompt(ctx context.Context) string {
	cm := &corev1.ConfigMap{}
	key := types.NamespacedName{Name: aiSystemPromptConfigMap, Namespace: r.operatorNamespace()}
	if err := r.settingsReader().Get(ctx, key, cm); err != nil {
		return ""
	}
	return strings.TrimSpace(cm.Data[aiSystemPromptKey])
}

func (r *CellenzaReconciler) fetchExtraInstructions(ctx context.Context, c *platformv1alpha1.Cellenza) string {
	cm := &corev1.ConfigMap{}
	key := types.NamespacedName{Name: aiPromptConfigMapName(c.Name), Namespace: defaultAISecretNamespace}
	if err := r.settingsReader().Get(ctx, key, cm); err != nil {
		return ""
	}
	return strings.TrimSpace(cm.Data[aiPromptConfigMapKey])
}

func aiEnrichmentEnabled(c *platformv1alpha1.Cellenza) bool {
	return c.Spec.AIEnrichment != nil && c.Spec.AIEnrichment.Enabled
}

func aiSeedEnabled(c *platformv1alpha1.Cellenza) bool {
	if !aiEnrichmentEnabled(c) {
		return false
	}
	if c.Spec.AIEnrichment.Seed == nil {
		return true
	}
	return c.Spec.AIEnrichment.Seed.Enabled
}

func aiTestsEnabled(c *platformv1alpha1.Cellenza) bool {
	if !aiEnrichmentEnabled(c) {
		return false
	}
	if c.Spec.AIEnrichment.Tests == nil {
		return true
	}
	return c.Spec.AIEnrichment.Tests.Enabled
}

func (r *CellenzaReconciler) fetchPRDiff(ctx context.Context, c *platformv1alpha1.Cellenza, token string) (string, error) {
	if !githubEnabled(c) || c.Spec.GitHub == nil || c.Spec.GitHub.Owner == "" || c.Spec.GitHub.Repo == "" {
		return "", nil
	}

	baseURL := strings.TrimRight(r.GitHubAPIBaseURL, "/")
	if baseURL == "" {
		baseURL = defaultGitHubAPIBaseURL
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		fmt.Sprintf("%s/repos/%s/%s/pulls/%d", baseURL, c.Spec.GitHub.Owner, c.Spec.GitHub.Repo, c.Spec.PRNumber),
		nil,
	)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github.diff")

	httpClient := r.GitHubHTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub diff fetch error %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return string(body), nil
}

func (r *CellenzaReconciler) fetchDBSchema(ctx context.Context, c *platformv1alpha1.Cellenza, nsName string) (string, bool, error) {
	if !databaseEnabled(c) {
		return "", true, nil
	}

	// Explicit guard: schema dump must wait for migration to complete so the
	// dumped schema reflects the post-migration state, not a stale snapshot.
	if c.Spec.Database.Migration != nil && c.Spec.Database.Migration.Enabled {
		if r.databaseTaskStatus(c, componentMigration) != phaseSucceeded {
			return "", false, nil
		}
	}

	job := &batchv1.Job{}
	err := r.Get(ctx, types.NamespacedName{Name: aiSchemaJobName, Namespace: nsName}, job)
	if errors.IsNotFound(err) {
		newJob := r.aiSchemaDumpJob(c, nsName)
		if err := controllerutil.SetControllerReference(c, newJob, r.Scheme); err != nil {
			return "", false, err
		}
		if err := r.Create(ctx, newJob); err != nil {
			return "", false, err
		}
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if err := r.ensureControllerReference(ctx, c, job); err != nil {
		return "", false, err
	}

	for _, cond := range job.Status.Conditions {
		if cond.Type == batchv1.JobFailed && cond.Status == corev1.ConditionTrue {
			return "", false, fmt.Errorf("database schema dump job %s/%s failed: %s", nsName, aiSchemaJobName, cond.Message)
		}
	}

	if job.Status.Succeeded == 0 {
		return "", false, nil
	}
	if r.KubeClient == nil {
		return "", false, fmt.Errorf("kubernetes client is required to read schema dump logs")
	}

	podName := r.firstPodName(ctx, nsName, client.MatchingLabels{"job-name": aiSchemaJobName})
	if podName == "" {
		return "", false, fmt.Errorf("schema dump job %s/%s succeeded but no pod logs were found", nsName, aiSchemaJobName)
	}

	lines := r.fetchPodLogs(ctx, nsName, podName, "schema-dump", 400)
	schema := strings.TrimSpace(strings.Join(lines, "\n"))
	if err := r.Delete(ctx, job); err != nil && !errors.IsNotFound(err) {
		return "", false, err
	}
	if schema == "" {
		return "", false, fmt.Errorf("schema dump job %s/%s produced empty output", nsName, aiSchemaJobName)
	}
	return schema, true, nil
}

func (r *CellenzaReconciler) generateAndStoreAIContent(ctx context.Context, c *platformv1alpha1.Cellenza, nsName string) (bool, error) {
	if c.Spec.AIEnrichment == nil || !c.Spec.AIEnrichment.Enabled {
		return true, nil
	}

	existing := &corev1.ConfigMap{}
	if err := r.Get(ctx, types.NamespacedName{Name: aiEnrichmentConfigMap, Namespace: nsName}, existing); err == nil {
		return true, nil
	} else if !errors.IsNotFound(err) {
		return false, err
	}

	apiKey, err := r.aiAPIKey(ctx, c)
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(r.AIAPIBaseURL) == "" {
		return false, fmt.Errorf("AI API URL is not configured")
	}

	var diff string
	if githubEnabled(c) {
		token, err := r.aiGitHubToken(ctx, c)
		if err != nil {
			return false, err
		}
		diff, err = r.fetchPRDiff(ctx, c, token)
		if err != nil {
			return false, err
		}
	}

	schema, ready, err := r.fetchDBSchema(ctx, c, nsName)
	if err != nil {
		return false, err
	}
	if !ready {
		return false, nil
	}

	aiClient := ai.NewClient(r.AIAPIBaseURL, apiKey, c.Spec.AIEnrichment.Model)
	if r.AIHTTPClient != nil {
		aiClient.HTTPClient = r.AIHTTPClient
	}
	generated, err := aiClient.Generate(ctx, ai.GenerateRequest{
		PRDiff:            diff,
		DBSchema:          schema,
		AppURL:            defaultAIInternalAppURL,
		Branch:            c.Spec.Branch,
		PRNumber:          c.Spec.PRNumber,
		SystemPrompt:      r.fetchSystemPrompt(ctx),
		ExtraInstructions: r.fetchExtraInstructions(ctx, c),
	})
	if err != nil {
		return false, err
	}

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      aiEnrichmentConfigMap,
			Namespace: nsName,
		},
	}
	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, configMap, func() error {
		if err := controllerutil.SetControllerReference(c, configMap, r.Scheme); err != nil {
			return err
		}
		configMap.Labels = map[string]string{
			labelManagedBy:                "cellenza-operator",
			labelCellenzaName:             c.Name,
			"app.kubernetes.io/component": "ai-enrichment",
		}
		configMap.Data = map[string]string{
			"seed.sql": generated.SeedSQL,
			"test.py":  generated.TestScript,
		}
		return nil
	})
	if err != nil {
		return false, err
	}

	return true, nil
}

func (r *CellenzaReconciler) aiGitHubToken(ctx context.Context, c *platformv1alpha1.Cellenza) (string, error) {
	if c.Spec.AIEnrichment != nil && c.Spec.AIEnrichment.GitHubTokenSecretRef != nil && c.Spec.AIEnrichment.GitHubTokenSecretRef.Name != "" {
		return r.githubTokenFromRef(ctx, c.Spec.AIEnrichment.GitHubTokenSecretRef)
	}
	return r.githubToken(ctx, c)
}

func (r *CellenzaReconciler) aiAPIKey(ctx context.Context, c *platformv1alpha1.Cellenza) (string, error) {
	spec := c.Spec.AIEnrichment
	if spec == nil || spec.APISecretRef == nil || spec.APISecretRef.Name == "" {
		return "", fmt.Errorf("spec.aiEnrichment.apiSecretRef.name is required when AI enrichment is enabled")
	}

	namespace := spec.APISecretRef.Namespace
	if namespace == "" {
		namespace = defaultAISecretNamespace
	}
	key := spec.APISecretRef.Key
	if key == "" {
		key = defaultAISecretKey
	}

	secret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Name: spec.APISecretRef.Name, Namespace: namespace}, secret); err != nil {
		if errors.IsNotFound(err) {
			return "", fmt.Errorf("AI API key Secret %s/%s was not found", namespace, spec.APISecretRef.Name)
		}
		return "", err
	}

	value, ok := secret.Data[key]
	if !ok || len(value) == 0 {
		return "", fmt.Errorf("AI API key Secret %s/%s does not contain key %q", namespace, spec.APISecretRef.Name, key)
	}
	return strings.TrimSpace(string(value)), nil
}

func (r *CellenzaReconciler) aiSchemaDumpJob(c *platformv1alpha1.Cellenza, nsName string) *batchv1.Job {
	backoffLimit := int32(1)
	ttlSecondsAfterFinished := int32(60)

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      aiSchemaJobName,
			Namespace: nsName,
			Labels: map[string]string{
				labelManagedBy:                "cellenza-operator",
				labelCellenzaName:             c.Name,
				"app.kubernetes.io/component": "ai-enrichment",
				"platform.company.io/task":    "schema-dump",
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &ttlSecondsAfterFinished,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						labelManagedBy:             "cellenza-operator",
						labelCellenzaName:          c.Name,
						"platform.company.io/task": "schema-dump",
					},
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:    "schema-dump",
							Image:   defaultAISchemaDumpImage,
							Command: []string{"sh", "-c", "pg_dump --schema-only -h postgres -U \"$POSTGRES_USER\" \"$POSTGRES_DB\""},
							EnvFrom: []corev1.EnvFromSource{
								{
									SecretRef: &corev1.SecretEnvSource{
										LocalObjectReference: corev1.LocalObjectReference{Name: postgresSecretName},
									},
								},
							},
							Env: []corev1.EnvVar{
								{
									Name: "PGPASSWORD",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{Name: postgresSecretName},
											Key:                  "POSTGRES_PASSWORD",
										},
									},
								},
							},
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
						},
					},
				},
			},
		},
	}
}

func (r *CellenzaReconciler) reconcileAIEnrichment(ctx context.Context, c *platformv1alpha1.Cellenza, nsName string) (ctrl.Result, error) {
	if !aiEnrichmentEnabled(c) {
		return ctrl.Result{}, nil
	}

	aiStatus := ensureAIEnrichmentStatus(c)
	if aiStatus.Phase == phaseSucceeded || aiStatus.Phase == phaseFailed {
		return ctrl.Result{}, nil
	}

	configMap := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{Name: aiEnrichmentConfigMap, Namespace: nsName}, configMap)
	if errors.IsNotFound(err) {
		if aiStatus.Phase != phaseGenerating || aiStatus.Error != "" {
			aiStatus.Phase = phaseGenerating
			aiStatus.Error = ""
			if err := r.Status().Update(ctx, c); err != nil {
				return ctrl.Result{}, err
			}
			if err := r.refreshCellenza(ctx, types.NamespacedName{Name: c.Name, Namespace: c.Namespace}, c); err != nil {
				return ctrl.Result{}, err
			}
			aiStatus = ensureAIEnrichmentStatus(c)
		}
		ready, genErr := r.generateAndStoreAIContent(ctx, c, nsName)
		if genErr != nil {
			r.markAIEnrichmentFailed(c, genErr.Error())
			if statusErr := r.Status().Update(ctx, c); statusErr != nil {
				return ctrl.Result{}, statusErr
			}
			return ctrl.Result{}, nil
		}
		if !ready {
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}
	} else if err != nil {
		return ctrl.Result{}, err
	}

	seedDone := true
	if aiSeedEnabled(c) {
		state, seedErr := r.reconcileAISeedJob(ctx, c, nsName)
		aiStatus.SeedStatus = state
		if seedErr != nil {
			aiStatus.SeedStatus = phaseFailed
			aiStatus.Error = seedErr.Error()
		}
		seedDone = state == phaseSucceeded || state == phaseSkipped || state == phaseFailed
	}

	testsDone := true
	if aiTestsEnabled(c) {
		state, results, testErr := r.reconcileAITestJob(ctx, c, nsName)
		aiStatus.TestsStatus = state
		if len(results) > 0 {
			aiStatus.TestResults = results
		}
		if testErr != nil {
			aiStatus.TestsStatus = phaseFailed
			aiStatus.Error = testErr.Error()
		}
		testsDone = state == phaseSucceeded || state == phaseSkipped || state == phaseFailed
	}

	if !seedDone || !testsDone {
		aiStatus.Phase = phaseRunning
		if err := r.Status().Update(ctx, c); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	if aiStatus.Error != "" || aiStatus.SeedStatus == phaseFailed || aiStatus.TestsStatus == phaseFailed {
		aiStatus.Phase = phaseFailed
	} else {
		aiStatus.Phase = phaseSucceeded
	}
	now := metav1.Now()
	aiStatus.CompletedAt = &now
	aiStatus.Summary = buildAIEnrichmentSummary(c)
	setAIEnrichmentCondition(c)
	if err := r.Status().Update(ctx, c); err != nil {
		return ctrl.Result{}, err
	}
	r.syncGitHubAIComment(ctx, c)

	return ctrl.Result{}, nil
}

func (r *CellenzaReconciler) reconcileAISeedJob(ctx context.Context, c *platformv1alpha1.Cellenza, nsName string) (string, error) {
	if !aiSeedEnabled(c) {
		return phaseSkipped, nil
	}
	return r.reconcileAIJob(ctx, c, nsName, aiSeedJobName, r.aiSeedJob(c, nsName), true)
}

func (r *CellenzaReconciler) reconcileAITestJob(ctx context.Context, c *platformv1alpha1.Cellenza, nsName string) (string, []string, error) {
	if !aiTestsEnabled(c) {
		return phaseSkipped, nil, nil
	}

	state, err := r.reconcileAIJob(ctx, c, nsName, aiTestJobName, r.aiTestJob(c, nsName), true)
	if state != phaseSucceeded && state != phaseFailed {
		return state, nil, err
	}

	podName := r.firstPodName(ctx, nsName, client.MatchingLabels{"job-name": aiTestJobName})
	if podName == "" {
		return state, nil, err
	}
	lines := r.fetchPodLogs(ctx, nsName, podName, "ai-tests", 200)
	return state, extractAITestResults(lines), err
}

func (r *CellenzaReconciler) reconcileAIJob(ctx context.Context, c *platformv1alpha1.Cellenza, nsName, jobName string, desired *batchv1.Job, preserveLogs bool) (string, error) {
	job := &batchv1.Job{}
	err := r.Get(ctx, types.NamespacedName{Name: jobName, Namespace: nsName}, job)
	if errors.IsNotFound(err) {
		if err := controllerutil.SetControllerReference(c, desired, r.Scheme); err != nil {
			return "", err
		}
		if err := r.Create(ctx, desired); err != nil {
			return "", err
		}
		return phaseRunning, nil
	}
	if err != nil {
		return "", err
	}
	if err := r.ensureControllerReference(ctx, c, job); err != nil {
		return "", err
	}

	if job.Status.Succeeded > 0 {
		if !preserveLogs {
			if err := r.Delete(ctx, job); err != nil && !errors.IsNotFound(err) {
				return "", err
			}
		}
		return phaseSucceeded, nil
	}

	for _, cond := range job.Status.Conditions {
		if cond.Type == batchv1.JobFailed && cond.Status == corev1.ConditionTrue {
			return phaseFailed, fmt.Errorf("AI job %s/%s failed: %s", nsName, jobName, cond.Message)
		}
	}
	return phaseRunning, nil
}

func (r *CellenzaReconciler) aiSeedJob(c *platformv1alpha1.Cellenza, nsName string) *batchv1.Job {
	image := defaultAISchemaDumpImage
	if c.Spec.AIEnrichment != nil && c.Spec.AIEnrichment.Seed != nil && c.Spec.AIEnrichment.Seed.Image != "" {
		image = c.Spec.AIEnrichment.Seed.Image
	}
	backoffLimit := int32(1)
	ttlSecondsAfterFinished := int32(300)

	return r.aiConfigMapBackedJob(c, nsName, aiSeedJobName, image, []string{"sh", "-c", "psql -h postgres -U \"$POSTGRES_USER\" -d \"$POSTGRES_DB\" -f /data/seed.sql"}, "seed.sql", "ai-seed", &backoffLimit, &ttlSecondsAfterFinished, true)
}

func (r *CellenzaReconciler) aiTestJob(c *platformv1alpha1.Cellenza, nsName string) *batchv1.Job {
	image := defaultAITestImage
	if c.Spec.AIEnrichment != nil && c.Spec.AIEnrichment.Tests != nil && c.Spec.AIEnrichment.Tests.Image != "" {
		image = c.Spec.AIEnrichment.Tests.Image
	}
	backoffLimit := int32(0)
	ttlSecondsAfterFinished := int32(300)

	job := r.aiConfigMapBackedJob(c, nsName, aiTestJobName, image, []string{"sh", "-c", "pip install requests -q && python /data/test.py"}, "test.py", "ai-tests", &backoffLimit, &ttlSecondsAfterFinished, false)
	job.Spec.Template.Spec.Containers[0].Env = append(job.Spec.Template.Spec.Containers[0].Env, corev1.EnvVar{Name: "APP_URL", Value: defaultAIInternalAppURL})
	return job
}

func (r *CellenzaReconciler) aiConfigMapBackedJob(c *platformv1alpha1.Cellenza, nsName, jobName, image string, command []string, fileName, containerName string, backoffLimit, ttl *int32, withPostgresSecret bool) *batchv1.Job {
	container := corev1.Container{
		Name:            containerName,
		Image:           image,
		Command:         command,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("50m"),
				corev1.ResourceMemory: resource.MustParse("128Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("500m"),
				corev1.ResourceMemory: resource.MustParse("512Mi"),
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: "ai-data", MountPath: "/data"},
		},
	}
	if withPostgresSecret {
		container.EnvFrom = []corev1.EnvFromSource{{
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: postgresSecretName},
			},
		}}
		container.Env = append(container.Env, corev1.EnvVar{
			Name: "PGPASSWORD",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: postgresSecretName},
					Key:                  "POSTGRES_PASSWORD",
				},
			},
		})
	}

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: nsName,
			Labels: map[string]string{
				labelManagedBy:                "cellenza-operator",
				labelCellenzaName:             c.Name,
				"app.kubernetes.io/component": "ai-enrichment",
				"platform.company.io/task":    jobName,
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            backoffLimit,
			TTLSecondsAfterFinished: ttl,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						labelManagedBy:             "cellenza-operator",
						labelCellenzaName:          c.Name,
						"platform.company.io/task": jobName,
					},
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers:    []corev1.Container{container},
					Volumes: []corev1.Volume{{
						Name: "ai-data",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: aiEnrichmentConfigMap},
								Items: []corev1.KeyToPath{{
									Key:  fileName,
									Path: fileName,
								}},
							},
						},
					}},
				},
			},
		},
	}
}

func ensureAIEnrichmentStatus(c *platformv1alpha1.Cellenza) *platformv1alpha1.AIEnrichmentStatus {
	if c.Status.AIEnrichment == nil {
		c.Status.AIEnrichment = &platformv1alpha1.AIEnrichmentStatus{}
	}
	return c.Status.AIEnrichment
}

func (r *CellenzaReconciler) markAIEnrichmentFailed(c *platformv1alpha1.Cellenza, msg string) {
	aiStatus := ensureAIEnrichmentStatus(c)
	aiStatus.Phase = phaseFailed
	aiStatus.Error = msg
	now := metav1.Now()
	aiStatus.CompletedAt = &now
	aiStatus.Summary = buildAIEnrichmentSummary(c)
	setAIEnrichmentCondition(c)
}

func buildAIEnrichmentSummary(c *platformv1alpha1.Cellenza) string {
	aiStatus := c.Status.AIEnrichment
	if aiStatus == nil {
		return "AI enrichment not started"
	}
	var parts []string
	if aiSeedEnabled(c) {
		parts = append(parts, fmt.Sprintf("seed=%s", defaultStatus(aiStatus.SeedStatus)))
	}
	if aiTestsEnabled(c) {
		parts = append(parts, fmt.Sprintf("tests=%s", defaultStatus(aiStatus.TestsStatus)))
	}
	if len(parts) == 0 {
		return "AI enrichment completed"
	}
	return "AI enrichment " + strings.Join(parts, ", ")
}

func setAIEnrichmentCondition(c *platformv1alpha1.Cellenza) {
	aiStatus := c.Status.AIEnrichment
	if aiStatus == nil {
		return
	}

	status := metav1.ConditionFalse
	reason := "AIEnrichmentFailed"
	message := buildAIEnrichmentSummary(c)
	if aiStatus.Error != "" {
		message = fmt.Sprintf("%s (%s)", message, aiStatus.Error)
	}

	switch aiStatus.Phase {
	case phaseSucceeded:
		status = metav1.ConditionTrue
		reason = "AIEnrichmentCompleted"
	case phaseGenerating, phaseRunning, phasePending:
		reason = "AIEnrichmentInProgress"
	}

	c.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionAIEnrichmentReady,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})
}

func extractAITestResults(lines []string) []string {
	var results []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "PASS") || strings.HasPrefix(trimmed, "FAIL") {
			results = append(results, trimmed)
		}
	}
	return results
}
