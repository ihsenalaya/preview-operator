package controller

import (
	"context"
	"fmt"
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
	smokeJobName       = "smoke-tests"
	regressionJobName  = "regression-tests"
	e2eJobName         = "e2e-tests"
	testSuiteConfigMap = "cellenza-test-suite"

	suiteStepSaving            = "saving"
	suiteStepSmoke             = "smoke"
	suiteStepRestoreRegression = "restore-regression"
	suiteStepRegression        = "regression"
	suiteStepRestoreE2E        = "restore-e2e"
	suiteStepE2E               = "e2e"

	testJobCPURequest    = "50m"
	testJobMemoryRequest = "128Mi"
	testJobCPULimit      = "500m"
	testJobMemoryLimit   = "512Mi"

	// Playwright/Chromium needs more headroom than a plain Python test runner.
	e2eJobCPURequest    = "200m"
	e2eJobMemoryRequest = "512Mi"
	e2eJobCPULimit      = "1000m"
	e2eJobMemoryLimit   = "1Gi"

	playwrightImage = "mcr.microsoft.com/playwright/python:v1.44.0-jammy"

	// smokeScript is embedded in the operator — no external file required.
	// It tests the health endpoint and the main products endpoint.
	smokeScript = `import requests,sys,os
BASE=os.environ.get('APP_URL','http://app:80')
checks=[('/healthz',200),('/api/products',200)]
p,f=0,0
for path,code in checks:
    try:
        r=requests.get(BASE+path,timeout=5)
        ok=r.status_code==code
        label='PASS' if ok else 'FAIL'
        print(label+' smoke '+path+': '+str(r.status_code))
        p,f=(p+1,f) if ok else (p,f+1)
    except Exception as e:
        print('FAIL smoke '+path+': '+str(e))
        f+=1
print('Results: '+str(p)+' passed, '+str(f)+' failed')
sys.exit(1 if f>0 else 0)
`
)

func testSuiteEnabled(c *platformv1alpha1.Cellenza) bool {
	return c.Spec.TestSuite != nil && c.Spec.TestSuite.Enabled
}

func smokeEnabled(c *platformv1alpha1.Cellenza) bool {
	if c.Spec.TestSuite.Smoke == nil {
		return true
	}
	return c.Spec.TestSuite.Smoke.Enabled
}

func regressionEnabled(c *platformv1alpha1.Cellenza) bool {
	if c.Spec.TestSuite.Regression == nil {
		return true
	}
	return c.Spec.TestSuite.Regression.Enabled
}

func e2eEnabled(c *platformv1alpha1.Cellenza) bool {
	if c.Spec.TestSuite.E2E == nil {
		return true
	}
	return c.Spec.TestSuite.E2E.Enabled
}

func ensureTestSuiteStatus(c *platformv1alpha1.Cellenza) *platformv1alpha1.TestSuiteStatus {
	if c.Status.Tests == nil {
		c.Status.Tests = &platformv1alpha1.TestSuiteStatus{}
	}
	return c.Status.Tests
}

// reconcileTestSuite orchestrates the test pipeline sequentially:
// checkpoint-save → smoke → restore → regression → restore → e2e
func (r *CellenzaReconciler) reconcileTestSuite(ctx context.Context, c *platformv1alpha1.Cellenza, nsName string) (ctrl.Result, error) {
	if !testSuiteEnabled(c) {
		return ctrl.Result{}, nil
	}

	tests := ensureTestSuiteStatus(c)
	if tests.Phase == phaseSucceeded || tests.Phase == phaseFailed {
		return ctrl.Result{}, nil
	}

	if err := r.ensureTestSuiteConfigMap(ctx, c, nsName); err != nil {
		return ctrl.Result{}, err
	}

	dbEnabled := databaseEnabled(c)

	// Initialise step on first entry.
	if tests.Step == "" {
		if dbEnabled {
			tests.Step = suiteStepSaving
		} else {
			tests.Step = suiteStepSmoke
		}
		tests.Phase = phaseRunning
		if err := r.Status().Update(ctx, c); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
	}

	previewURL := c.Status.URL

	switch tests.Step {

	case suiteStepSaving:
		done, err := r.ensureSuiteCheckpointSaved(ctx, c, nsName)
		if err != nil {
			return ctrl.Result{}, err
		}
		if !done {
			return ctrl.Result{RequeueAfter: 3 * time.Second}, nil
		}
		tests.Step = suiteStepSmoke
		if err := r.Status().Update(ctx, c); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil

	case suiteStepSmoke:
		if smokeEnabled(c) {
			state, output := r.checkOrCreateTestJob(ctx, c, nsName, smokeJobName, r.smokeTestJob(c, nsName))
			tests.Smoke.Phase = state
			tests.Smoke.Output = output
			tests.Smoke.Passed, tests.Smoke.Failed = countTestResults(output)
			if !testResultFinal(state) {
				tests.Phase = phaseRunning
				if err := r.Status().Update(ctx, c); err != nil {
					return ctrl.Result{}, err
				}
				return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
			}
		} else {
			tests.Smoke.Phase = phaseSkipped
		}
		if dbEnabled && regressionEnabled(c) {
			tests.Step = suiteStepRestoreRegression
		} else {
			tests.Step = suiteStepRegression
		}
		if err := r.Status().Update(ctx, c); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil

	case suiteStepRestoreRegression:
		done, err := r.ensureSuiteCheckpointRestored(ctx, c, nsName, suiteRestoreRegressionJob)
		if err != nil {
			return ctrl.Result{}, err
		}
		if !done {
			return ctrl.Result{RequeueAfter: 3 * time.Second}, nil
		}
		tests.Step = suiteStepRegression
		if err := r.Status().Update(ctx, c); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil

	case suiteStepRegression:
		if regressionEnabled(c) {
			state, output := r.checkOrCreateTestJob(ctx, c, nsName, regressionJobName, r.regressionTestJob(c, nsName, previewURL))
			tests.Regression.Phase = state
			tests.Regression.Output = output
			tests.Regression.Passed, tests.Regression.Failed = countTestResults(output)
			if !testResultFinal(state) {
				tests.Phase = phaseRunning
				if err := r.Status().Update(ctx, c); err != nil {
					return ctrl.Result{}, err
				}
				return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
			}
		} else {
			tests.Regression.Phase = phaseSkipped
		}
		if dbEnabled && e2eEnabled(c) {
			tests.Step = suiteStepRestoreE2E
		} else {
			tests.Step = suiteStepE2E
		}
		if err := r.Status().Update(ctx, c); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil

	case suiteStepRestoreE2E:
		done, err := r.ensureSuiteCheckpointRestored(ctx, c, nsName, suiteRestoreE2EJob)
		if err != nil {
			return ctrl.Result{}, err
		}
		if !done {
			return ctrl.Result{RequeueAfter: 3 * time.Second}, nil
		}
		tests.Step = suiteStepE2E
		if err := r.Status().Update(ctx, c); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil

	case suiteStepE2E:
		if e2eEnabled(c) {
			state, output := r.checkOrCreateTestJob(ctx, c, nsName, e2eJobName, r.e2eTestJob(c, nsName, previewURL))
			tests.E2E.Phase = state
			tests.E2E.Output = output
			tests.E2E.Passed, tests.E2E.Failed = countTestResults(output)
			if !testResultFinal(state) {
				tests.Phase = phaseRunning
				if err := r.Status().Update(ctx, c); err != nil {
					return ctrl.Result{}, err
				}
				return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
			}
		} else {
			tests.E2E.Phase = phaseSkipped
		}
	}

	anyFailed := tests.Smoke.Phase == phaseFailed ||
		tests.Regression.Phase == phaseFailed ||
		tests.E2E.Phase == phaseFailed
	if anyFailed {
		tests.Phase = phaseFailed
	} else {
		tests.Phase = phaseSucceeded
	}

	r.setTestSuiteCondition(c)
	if err := r.Status().Update(ctx, c); err != nil {
		return ctrl.Result{}, err
	}
	r.postTestResultsComment(ctx, c)
	return ctrl.Result{}, nil
}

// checkOrCreateTestJob creates a test job if it doesn't exist, otherwise returns its current state and logs.
func (r *CellenzaReconciler) checkOrCreateTestJob(ctx context.Context, c *platformv1alpha1.Cellenza, nsName, jobName string, desired *batchv1.Job) (string, []string) {
	job := &batchv1.Job{}
	err := r.Get(ctx, types.NamespacedName{Name: jobName, Namespace: nsName}, job)
	if errors.IsNotFound(err) {
		if err := controllerutil.SetControllerReference(c, desired, r.Scheme); err != nil {
			return phaseFailed, []string{fmt.Sprintf("FAIL %s setup: %v", jobName, err)}
		}
		if err := r.Create(ctx, desired); err != nil {
			return phaseFailed, []string{fmt.Sprintf("FAIL %s create: %v", jobName, err)}
		}
		return phaseRunning, nil
	}
	if err != nil {
		return phaseFailed, []string{fmt.Sprintf("FAIL %s get: %v", jobName, err)}
	}
	_ = r.ensureControllerReference(ctx, c, job)

	podLabels := client.MatchingLabels{"job-name": jobName}

	if job.Status.Succeeded > 0 {
		podName := r.firstPodName(ctx, nsName, podLabels)
		var lines []string
		if podName != "" {
			lines = r.fetchPodLogs(ctx, nsName, podName, jobName, 200)
		}
		return phaseSucceeded, extractTestResults(lines)
	}

	for _, cond := range job.Status.Conditions {
		if cond.Type == batchv1.JobFailed && cond.Status == corev1.ConditionTrue {
			podName := r.firstPodName(ctx, nsName, podLabels)
			var lines []string
			if podName != "" {
				lines = r.fetchPodLogs(ctx, nsName, podName, jobName, 200)
			}
			return phaseFailed, extractTestResults(lines)
		}
	}

	return phaseRunning, nil
}

// ensureTestSuiteConfigMap creates the test suite ConfigMap with the smoke script.
func (r *CellenzaReconciler) ensureTestSuiteConfigMap(ctx context.Context, c *platformv1alpha1.Cellenza, nsName string) error {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testSuiteConfigMap,
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
			"app.kubernetes.io/component": "test-suite",
		}
		cm.Data = map[string]string{
			"smoke.py": smokeScript,
		}
		return nil
	})
	return err
}

func (r *CellenzaReconciler) smokeTestJob(c *platformv1alpha1.Cellenza, nsName string) *batchv1.Job {
	image := "python:3.12-slim"
	if c.Spec.TestSuite.Smoke != nil && c.Spec.TestSuite.Smoke.Image != "" {
		image = c.Spec.TestSuite.Smoke.Image
	}
	cmd := []string{"sh", "-c", "pip install requests -q 2>/dev/null && python /data/smoke.py"}
	if c.Spec.TestSuite.Smoke != nil && len(c.Spec.TestSuite.Smoke.Command) > 0 {
		cmd = c.Spec.TestSuite.Smoke.Command
	}

	job := r.testJob(c, nsName, smokeJobName, image, cmd, testSuiteConfigMap, "smoke.py", smokeJobName, false)
	job.Spec.Template.Spec.Containers[0].Env = append(
		job.Spec.Template.Spec.Containers[0].Env,
		corev1.EnvVar{Name: "APP_URL", Value: appServiceURL(c)},
	)
	return job
}

func (r *CellenzaReconciler) regressionTestJob(c *platformv1alpha1.Cellenza, nsName, previewURL string) *batchv1.Job {
	image := mainAppImage(c)
	if c.Spec.TestSuite.Regression != nil && c.Spec.TestSuite.Regression.Image != "" {
		image = c.Spec.TestSuite.Regression.Image
	}
	cmd := []string{"sh", "-c", "pip install requests -q 2>/dev/null && python /app/tests/regression.py"}
	if c.Spec.TestSuite.Regression != nil && len(c.Spec.TestSuite.Regression.Command) > 0 {
		cmd = c.Spec.TestSuite.Regression.Command
	}

	job := r.testJobNoMount(c, nsName, regressionJobName, image, cmd, regressionJobName, true)
	job.Spec.Template.Spec.Containers[0].Env = append(
		job.Spec.Template.Spec.Containers[0].Env,
		corev1.EnvVar{Name: "APP_URL", Value: appServiceURL(c)},
		corev1.EnvVar{Name: "PREVIEW_URL", Value: previewURL},
	)
	return job
}

// e2eTestJob builds a Job that runs real browser tests via Playwright.
// An init container copies e2e.py from the app image into a shared emptyDir,
// then the Playwright container (with Chromium pre-installed) executes the tests.
func (r *CellenzaReconciler) e2eTestJob(c *platformv1alpha1.Cellenza, nsName, previewURL string) *batchv1.Job {
	appImage := mainAppImage(c)
	pwImage := playwrightImage
	if c.Spec.TestSuite.E2E != nil && c.Spec.TestSuite.E2E.Image != "" {
		pwImage = c.Spec.TestSuite.E2E.Image
	}
	cmd := []string{"sh", "-c", "python -m pip install requests playwright==1.44.0 -q >/dev/null 2>&1 && python /data/tests/e2e.py"}
	if c.Spec.TestSuite.E2E != nil && len(c.Spec.TestSuite.E2E.Command) > 0 {
		cmd = c.Spec.TestSuite.E2E.Command
	}

	backoffLimit := int32(0)
	ttl := int32(300)

	initContainer := corev1.Container{
		Name:            "copy-tests",
		Image:           appImage,
		Command:         []string{"sh", "-c", "mkdir -p /data/tests && cp -R /app/tests/. /data/tests/"},
		ImagePullPolicy: corev1.PullIfNotPresent,
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("50m"),
				corev1.ResourceMemory: resource.MustParse("64Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("128Mi"),
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: "test-data", MountPath: "/data"},
		},
	}

	mainContainer := corev1.Container{
		Name:            e2eJobName,
		Image:           pwImage,
		Command:         cmd,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Env: []corev1.EnvVar{
			{Name: "APP_URL", Value: appServiceURL(c)},
			{Name: "PREVIEW_URL", Value: previewURL},
			{Name: "CHECKPOINT_API", Value: fmt.Sprintf("http://cellenza-extension.cellenza-operator-system.svc.cluster.local:8090/api/previews/%s", c.Name)},
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(e2eJobCPURequest),
				corev1.ResourceMemory: resource.MustParse(e2eJobMemoryRequest),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(e2eJobCPULimit),
				corev1.ResourceMemory: resource.MustParse(e2eJobMemoryLimit),
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: "test-data", MountPath: "/data"},
		},
	}

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      e2eJobName,
			Namespace: nsName,
			Labels:    testJobLabels(c, e2eJobName),
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &ttl,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						labelManagedBy:             "cellenza-operator",
						labelCellenzaName:          c.Name,
						"platform.company.io/task": e2eJobName,
					},
				},
				Spec: corev1.PodSpec{
					RestartPolicy:  corev1.RestartPolicyNever,
					InitContainers: []corev1.Container{initContainer},
					Containers:     []corev1.Container{mainContainer},
					Volumes: []corev1.Volume{{
						Name: "test-data",
						VolumeSource: corev1.VolumeSource{
							EmptyDir: &corev1.EmptyDirVolumeSource{},
						},
					}},
				},
			},
		},
	}
}

// testJob builds a Job that mounts a script from a ConfigMap.
func (r *CellenzaReconciler) testJob(c *platformv1alpha1.Cellenza, nsName, jobName, image string, cmd []string, cmName, fileName, containerName string, withPostgres bool) *batchv1.Job {
	backoffLimit := int32(0)
	ttl := int32(300)

	container := corev1.Container{
		Name:            containerName,
		Image:           image,
		Command:         cmd,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Resources:       testJobResources(),
		VolumeMounts: []corev1.VolumeMount{
			{Name: "test-data", MountPath: "/data"},
		},
	}
	if withPostgres {
		container.EnvFrom = []corev1.EnvFromSource{{
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: postgresSecretName},
			},
		}}
	}

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: nsName,
			Labels:    testJobLabels(c, jobName),
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &ttl,
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
						Name: "test-data",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: cmName},
								Items:                []corev1.KeyToPath{{Key: fileName, Path: fileName}},
							},
						},
					}},
				},
			},
		},
	}
}

// testJobNoMount builds a Job without a ConfigMap volume (tests are in the image).
func (r *CellenzaReconciler) testJobNoMount(c *platformv1alpha1.Cellenza, nsName, jobName, image string, cmd []string, containerName string, withPostgres bool) *batchv1.Job {
	backoffLimit := int32(0)
	ttl := int32(300)

	container := corev1.Container{
		Name:            containerName,
		Image:           image,
		Command:         cmd,
		ImagePullPolicy: corev1.PullAlways,
		Resources:       testJobResources(),
	}
	if withPostgres && databaseEnabledForSpec(c) {
		container.EnvFrom = []corev1.EnvFromSource{{
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: postgresSecretName},
			},
		}}
	}

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: nsName,
			Labels:    testJobLabels(c, jobName),
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &ttl,
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
				},
			},
		},
	}
}

func databaseEnabledForSpec(c *platformv1alpha1.Cellenza) bool {
	return c.Spec.Database != nil && c.Spec.Database.Enabled
}

func testJobResources() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(testJobCPURequest),
			corev1.ResourceMemory: resource.MustParse(testJobMemoryRequest),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(testJobCPULimit),
			corev1.ResourceMemory: resource.MustParse(testJobMemoryLimit),
		},
	}
}

func testJobLabels(c *platformv1alpha1.Cellenza, jobName string) map[string]string {
	return map[string]string{
		labelManagedBy:                "cellenza-operator",
		labelCellenzaName:             c.Name,
		"app.kubernetes.io/component": "test-suite",
		"platform.company.io/task":    jobName,
	}
}

func (r *CellenzaReconciler) setTestSuiteCondition(c *platformv1alpha1.Cellenza) {
	tests := c.Status.Tests
	if tests == nil {
		return
	}

	status := metav1.ConditionTrue
	reason := "TestSuiteSucceeded"
	msg := buildTestSuiteSummary(tests)

	if tests.Phase == phaseFailed {
		status = metav1.ConditionFalse
		reason = "TestSuiteFailed"
	}

	c.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionTestSuiteReady,
		Status:             status,
		Reason:             reason,
		Message:            msg,
		LastTransitionTime: metav1.Now(),
	})
}

func buildTestSuiteSummary(tests *platformv1alpha1.TestSuiteStatus) string {
	parts := []string{}
	if tests.Smoke.Phase != "" && tests.Smoke.Phase != phaseSkipped {
		parts = append(parts, fmt.Sprintf("smoke=%s(%dp/%df)", tests.Smoke.Phase, tests.Smoke.Passed, tests.Smoke.Failed))
	}
	if tests.Regression.Phase != "" && tests.Regression.Phase != phaseSkipped {
		parts = append(parts, fmt.Sprintf("regression=%s(%dp/%df)", tests.Regression.Phase, tests.Regression.Passed, tests.Regression.Failed))
	}
	if tests.E2E.Phase != "" && tests.E2E.Phase != phaseSkipped {
		parts = append(parts, fmt.Sprintf("e2e=%s(%dp/%df)", tests.E2E.Phase, tests.E2E.Passed, tests.E2E.Failed))
	}
	if len(parts) == 0 {
		return "test suite completed"
	}
	return strings.Join(parts, ", ")
}

func extractTestResults(lines []string) []string {
	var results []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "PASS") || strings.HasPrefix(trimmed, "FAIL") || strings.HasPrefix(trimmed, "Results:") {
			results = append(results, trimmed)
		}
	}
	return results
}

func countTestResults(output []string) (int, int) {
	passed, failed := 0, 0
	for _, line := range output {
		if strings.HasPrefix(line, "PASS") {
			passed++
		} else if strings.HasPrefix(line, "FAIL") {
			failed++
		}
	}
	return passed, failed
}

func testResultFinal(phase string) bool {
	return phase == phaseSucceeded || phase == phaseFailed || phase == phaseSkipped
}
