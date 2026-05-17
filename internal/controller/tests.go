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

	platformv1alpha1 "github.com/ihsenalaya/preview-operator/api/v1alpha1"
	"github.com/ihsenalaya/preview-operator/internal/policy"
)

const (
	smokeJobName       = "smoke-tests"
	microcksImportJob  = "microcks-import"
	microcksJobName    = "microcks-contract-tests"
	regressionJobName  = "regression-tests"
	e2eJobName         = "e2e-tests"
	suiteStepSaving            = "saving"
	suiteStepSmoke             = "smoke"
	suiteStepMigration         = "migration"
	suiteStepImportSpec        = "import-spec"
	suiteStepContract          = "contract"
	suiteStepRestoreRegression = "restore-regression"
	suiteStepRegression        = "regression"
	suiteStepRestoreE2E        = "restore-e2e"
	suiteStepE2E               = "e2e"

	migrationTestJobName = "migration-tests"

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
)

func testSuiteEnabled(c *platformv1alpha1.Preview) bool {
	return c.Spec.TestSuite != nil && c.Spec.TestSuite.Enabled
}

func contractTestEnabled(c *platformv1alpha1.Preview) bool {
	if c.Spec.TestSuite == nil || c.Spec.TestSuite.ContractTesting == nil {
		return false
	}
	return c.Spec.TestSuite.ContractTesting.Enabled
}

func smokeEnabled(c *platformv1alpha1.Preview) bool {
	if c.Spec.TestSuite.Smoke == nil {
		return true
	}
	return c.Spec.TestSuite.Smoke.Enabled
}

func regressionEnabled(c *platformv1alpha1.Preview) bool {
	return c.Spec.TestSuite.Regression == nil || c.Spec.TestSuite.Regression.Enabled
}

func e2eEnabled(c *platformv1alpha1.Preview) bool {
	if c.Spec.TestSuite.E2E == nil {
		return true
	}
	return c.Spec.TestSuite.E2E.Enabled
}

func migrationEnabled(c *platformv1alpha1.Preview) bool {
	if c.Spec.TestSuite == nil || c.Spec.TestSuite.Migration == nil {
		return false
	}
	return c.Spec.TestSuite.Migration.Enabled
}

func checkpointIsolationEnabled(c *platformv1alpha1.Preview) bool {
	if c.Spec.Database == nil || c.Spec.Database.IsolationEnabled == nil {
		return true
	}
	return *c.Spec.Database.IsolationEnabled
}

// isolationMode returns the selected isolation mechanism: "restore" (default,
// pg_dump+psql restore) or "migration" (DROP SCHEMA + replay migration command).
// Only meaningful when checkpointIsolationEnabled(c) is true.
func isolationMode(c *platformv1alpha1.Preview) string {
	if c.Spec.Database == nil || c.Spec.Database.IsolationMode == "" {
		return "restore"
	}
	return c.Spec.Database.IsolationMode
}

func ensureTestSuiteStatus(c *platformv1alpha1.Preview) *platformv1alpha1.TestSuiteStatus {
	if c.Status.Tests == nil {
		c.Status.Tests = &platformv1alpha1.TestSuiteStatus{}
	}
	return c.Status.Tests
}

// reconcileTestSuite orchestrates the test pipeline sequentially:
// checkpoint-save → smoke → restore → regression → restore → e2e
// plan is the accepted TestPlan (nil means FullSuite — run everything enabled by spec).
func (r *PreviewReconciler) reconcileTestSuite(ctx context.Context, c *platformv1alpha1.Preview, nsName string, plan *platformv1alpha1.TestPlan) (ctrl.Result, error) {
	if !testSuiteEnabled(c) {
		return ctrl.Result{}, nil
	}

	tests := ensureTestSuiteStatus(c)
	if tests.Phase == phaseSucceeded || tests.Phase == phaseFailed {
		return ctrl.Result{}, nil
	}

	scripts := r.loadTestScripts(ctx)

	dbEnabled := databaseEnabled(c)

	// Initialise step on first entry.
	if tests.Step == "" {
		if dbEnabled && checkpointIsolationEnabled(c) {
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
		// Publish the checkpoint name in status so the extension restore endpoint can see it.
		if names, err := r.listCheckpointNames(ctx, nsName); err == nil && c.Status.Database != nil {
			c.Status.Database.Checkpoints = names
		}
		tests.Step = suiteStepSmoke
		if err := r.Status().Update(ctx, c); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil

	case suiteStepSmoke:
		if smokeEnabled(c) && policy.IsSuiteSelected(plan, platformv1alpha1.TestSuiteSmoke) {
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
		if migrationEnabled(c) && policy.IsSuiteSelected(plan, platformv1alpha1.TestSuiteMigration) {
			tests.Step = suiteStepMigration
		} else {
			contractSelected := contractTestEnabled(c) && policy.IsSuiteSelected(plan, platformv1alpha1.TestSuiteContract)
			if contractSelected && c.Spec.TestSuite.ContractTesting.SpecURL != "" {
				tests.Step = suiteStepImportSpec
			} else if contractSelected {
				tests.Step = suiteStepContract
			} else if dbEnabled && regressionEnabled(c) && policy.IsSuiteSelected(plan, platformv1alpha1.TestSuiteRegression) {
				if checkpointIsolationEnabled(c) {
					tests.Step = suiteStepRestoreRegression
				} else {
					tests.Step = suiteStepRegression
				}
			} else {
				tests.Step = suiteStepRegression
			}
		}
		if err := r.Status().Update(ctx, c); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil

	case suiteStepMigration:
		if migrationEnabled(c) && policy.IsSuiteSelected(plan, platformv1alpha1.TestSuiteMigration) {
			state, output := r.checkOrCreateTestJob(ctx, c, nsName, migrationTestJobName, r.migrationTestJob(c, nsName, scripts))
			tests.Migration.Phase = state
			tests.Migration.Output = output
			if !testResultFinal(state) {
				tests.Phase = phaseRunning
				if err := r.Status().Update(ctx, c); err != nil {
					return ctrl.Result{}, err
				}
				return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
			}
		} else {
			tests.Migration.Phase = phaseSkipped
		}
		contractSelected := contractTestEnabled(c) && policy.IsSuiteSelected(plan, platformv1alpha1.TestSuiteContract)
		if contractSelected && c.Spec.TestSuite.ContractTesting.SpecURL != "" {
			tests.Step = suiteStepImportSpec
		} else if contractSelected {
			tests.Step = suiteStepContract
		} else if dbEnabled && regressionEnabled(c) && policy.IsSuiteSelected(plan, platformv1alpha1.TestSuiteRegression) {
			if checkpointIsolationEnabled(c) {
				tests.Step = suiteStepRestoreRegression
			} else {
				tests.Step = suiteStepRegression
			}
		} else {
			tests.Step = suiteStepRegression
		}
		if err := r.Status().Update(ctx, c); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil

	case suiteStepImportSpec:
		state, _ := r.checkOrCreateTestJob(ctx, c, nsName, microcksImportJob, r.microcksImportJob(c, nsName))
		if !testResultFinal(state) {
			tests.Phase = phaseRunning
			if err := r.Status().Update(ctx, c); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}
		// Import failure is non-blocking — contract test will just return 0 results.
		tests.Step = suiteStepContract
		if err := r.Status().Update(ctx, c); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil

	case suiteStepContract:
		if contractTestEnabled(c) && policy.IsSuiteSelected(plan, platformv1alpha1.TestSuiteContract) {
			state, output := r.checkOrCreateTestJob(ctx, c, nsName, microcksJobName, r.microcksContractTestJob(c, nsName))
			tests.Contract.Phase = state
			tests.Contract.Output = output
			tests.Contract.Passed, tests.Contract.Failed = countTestResults(output)
			if !testResultFinal(state) {
				tests.Phase = phaseRunning
				if err := r.Status().Update(ctx, c); err != nil {
					return ctrl.Result{}, err
				}
				return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
			}
		} else {
			tests.Contract.Phase = phaseSkipped
		}
		// Contract failure does not block regression — continue pipeline regardless.
		if dbEnabled && regressionEnabled(c) {
			if checkpointIsolationEnabled(c) {
				tests.Step = suiteStepRestoreRegression
			} else {
				tests.Step = suiteStepRegression
			}
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
		if regressionEnabled(c) && policy.IsSuiteSelected(plan, platformv1alpha1.TestSuiteRegression) {
			state, output := r.checkOrCreateTestJob(ctx, c, nsName, regressionJobName, r.regressionTestJob(c, nsName, previewURL, scripts))
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
		if dbEnabled && e2eEnabled(c) && policy.IsSuiteSelected(plan, platformv1alpha1.TestSuiteE2E) {
			if checkpointIsolationEnabled(c) {
				tests.Step = suiteStepRestoreE2E
			} else {
				tests.Step = suiteStepE2E
			}
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
		if e2eEnabled(c) && policy.IsSuiteSelected(plan, platformv1alpha1.TestSuiteE2E) {
			state, output := r.checkOrCreateTestJob(ctx, c, nsName, e2eJobName, r.e2eTestJob(c, nsName, previewURL, scripts))
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
		tests.Migration.Phase == phaseFailed ||
		tests.Contract.Phase == phaseFailed ||
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
	r.triggerKagentAnalysis(ctx, c)
	return ctrl.Result{}, nil
}

// checkOrCreateTestJob creates a test job if it doesn't exist, otherwise returns its current state and logs.
func (r *PreviewReconciler) checkOrCreateTestJob(ctx context.Context, c *platformv1alpha1.Preview, nsName, jobName string, desired *batchv1.Job) (string, []string) {
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

func (r *PreviewReconciler) smokeTestJob(c *platformv1alpha1.Preview, nsName string) *batchv1.Job {
	image := mainAppImage(c)
	if c.Spec.TestSuite.Smoke != nil && c.Spec.TestSuite.Smoke.Image != "" {
		image = c.Spec.TestSuite.Smoke.Image
	}
	cmd := []string{"python", "/app/tests/smoke.py"}
	if c.Spec.TestSuite.Smoke != nil && len(c.Spec.TestSuite.Smoke.Command) > 0 {
		cmd = c.Spec.TestSuite.Smoke.Command
	}

	job := r.testJobNoMount(c, nsName, smokeJobName, image, cmd, smokeJobName, false)
	job.Spec.Template.Spec.Containers[0].Env = append(
		job.Spec.Template.Spec.Containers[0].Env,
		corev1.EnvVar{Name: "APP_URL", Value: appServiceURL(c)},
	)
	return job
}

func (r *PreviewReconciler) microcksImportJob(c *platformv1alpha1.Preview, nsName string) *batchv1.Job {
	ct := c.Spec.TestSuite.ContractTesting
	backoffLimit := int32(0)
	ttl := int32(300)

	keycloakURL := ct.KeycloakURL
	if keycloakURL == "" {
		// Default: derive from MicrocksURL by replacing host
		keycloakURL = "http://microcks-keycloak.microcks.svc.cluster.local:8080/realms/microcks"
	}
	username := ct.ImportUsername
	if username == "" {
		username = "manager"
	}
	password := ct.ImportPassword
	if password == "" {
		password = "microcks123"
	}

	env := []corev1.EnvVar{
		{Name: "MICROCKS_KEYCLOAK_URL", Value: keycloakURL},
		{Name: "MICROCKS_URL", Value: ct.MicrocksURL},
		{Name: "MICROCKS_USERNAME", Value: username},
		{Name: "MICROCKS_PASSWORD", Value: password},
		{Name: "SPEC_URL", Value: ct.SpecURL},
	}
	if ct.CredentialsSecretName != "" {
		optional := true
		env = append(env,
			corev1.EnvVar{Name: "MICROCKS_CLIENT_ID", ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: ct.CredentialsSecretName},
					Key:                  "client_id", Optional: &optional,
				},
			}},
			corev1.EnvVar{Name: "MICROCKS_CLIENT_SECRET", ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: ct.CredentialsSecretName},
					Key:                  "client_secret", Optional: &optional,
				},
			}},
		)
	}

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      microcksImportJob,
			Namespace: nsName,
			Labels:    testJobLabels(c, microcksImportJob),
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &ttl,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						labelManagedBy:             "preview-operator",
						labelPreviewName:           c.Name,
						"platform.company.io/task": microcksImportJob,
					},
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{{
						Name:            microcksImportJob,
						Image:           mainAppImage(c),
						Command:         []string{"python", "/app/tests/microcks-import.py"},
						Env:             env,
						ImagePullPolicy: corev1.PullIfNotPresent,
						Resources:       testJobResources(),
					}},
				},
			},
		},
	}
}

func (r *PreviewReconciler) microcksContractTestJob(c *platformv1alpha1.Preview, nsName string) *batchv1.Job {
	ct := c.Spec.TestSuite.ContractTesting
	backoffLimit := int32(0)
	ttl := int32(600)

	apiName := ct.APIName
	if apiName == "" {
		apiName = "Preview Catalog API"
	}
	apiVersion := ct.APIVersion
	if apiVersion == "" {
		apiVersion = "1.0.0"
	}
	testRunner := ct.TestRunner
	if testRunner == "" {
		testRunner = "OPEN_API_SCHEMA"
	}
	timeoutSec := ct.TimeoutSeconds
	if timeoutSec == 0 {
		timeoutSec = 60
	}

	backendFQDN := appServiceFQDN(c, nsName)
	env := []corev1.EnvVar{
		{Name: "MICROCKS_URL", Value: ct.MicrocksURL},
		{Name: "BACKEND_URL", Value: backendFQDN},
		{Name: "API_NAME", Value: apiName},
		{Name: "API_VERSION", Value: apiVersion},
		{Name: "TEST_RUNNER", Value: testRunner},
		{Name: "TEST_TIMEOUT_MS", Value: fmt.Sprintf("%d", int(timeoutSec)*1000)},
	}

	if ct.KeycloakURL != "" {
		env = append(env, corev1.EnvVar{Name: "MICROCKS_KEYCLOAK_URL", Value: ct.KeycloakURL})
	}

	if ct.CredentialsSecretName != "" {
		optional := true
		env = append(env,
			corev1.EnvVar{
				Name: "MICROCKS_CLIENT_ID",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: ct.CredentialsSecretName},
						Key:                  "client_id",
						Optional:             &optional,
					},
				},
			},
			corev1.EnvVar{
				Name: "MICROCKS_CLIENT_SECRET",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: ct.CredentialsSecretName},
						Key:                  "client_secret",
						Optional:             &optional,
					},
				},
			},
		)
	}

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      microcksJobName,
			Namespace: nsName,
			Labels:    testJobLabels(c, microcksJobName),
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &ttl,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						labelManagedBy:             "preview-operator",
						labelPreviewName:           c.Name,
						"platform.company.io/task": microcksJobName,
					},
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{{
						Name:            microcksJobName,
						Image:           mainAppImage(c),
						Command:         []string{"python", "/app/tests/microcks.py"},
						Env:             env,
						ImagePullPolicy: corev1.PullIfNotPresent,
						Resources:       testJobResources(),
					}},
				},
			},
		},
	}
}

func (r *PreviewReconciler) migrationTestJob(c *platformv1alpha1.Preview, nsName string, scripts *TestScripts) *batchv1.Job {
	image := mainAppImage(c)
	if c.Spec.TestSuite.Migration != nil && c.Spec.TestSuite.Migration.Image != "" {
		image = c.Spec.TestSuite.Migration.Image
	}
	cmd := []string{"sh", "-c", scripts.MigrationCommand}
	if c.Spec.TestSuite.Migration != nil && len(c.Spec.TestSuite.Migration.Command) > 0 {
		cmd = c.Spec.TestSuite.Migration.Command
	}
	return r.testJobNoMount(c, nsName, migrationTestJobName, image, cmd, migrationTestJobName, true)
}

func (r *PreviewReconciler) regressionTestJob(c *platformv1alpha1.Preview, nsName, previewURL string, scripts *TestScripts) *batchv1.Job {
	image := mainAppImage(c)
	if c.Spec.TestSuite.Regression != nil && c.Spec.TestSuite.Regression.Image != "" {
		image = c.Spec.TestSuite.Regression.Image
	}
	cmd := []string{"sh", "-c", scripts.RegressionCommand}
	if c.Spec.TestSuite.Regression != nil && len(c.Spec.TestSuite.Regression.Command) > 0 {
		cmd = c.Spec.TestSuite.Regression.Command
	}

	job := r.testJobNoMount(c, nsName, regressionJobName, image, cmd, regressionJobName, true)
	job.Spec.Template.Spec.Containers[0].Env = append(
		job.Spec.Template.Spec.Containers[0].Env,
		corev1.EnvVar{Name: "APP_URL", Value: appServiceURL(c)},
		corev1.EnvVar{Name: "FRONTEND_URL", Value: frontendServiceURL(c)},
		corev1.EnvVar{Name: "PREVIEW_URL", Value: previewURL},
	)
	return job
}

// e2eTestJob builds a Job that runs real browser tests via Playwright.
// An init container copies e2e.py from the app image into a shared emptyDir,
// then the Playwright container (with Chromium pre-installed) executes the tests.
func (r *PreviewReconciler) e2eTestJob(c *platformv1alpha1.Preview, nsName, previewURL string, scripts *TestScripts) *batchv1.Job {
	appImage := mainAppImage(c)
	pwImage := scripts.E2EImage
	if c.Spec.TestSuite.E2E != nil && c.Spec.TestSuite.E2E.Image != "" {
		pwImage = c.Spec.TestSuite.E2E.Image
	}
	cmd := []string{"sh", "-c", scripts.E2ECommand}
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
			{Name: "APP_URL", Value: frontendServiceURL(c)},
			{Name: "FRONTEND_URL", Value: frontendServiceURL(c)},
			{Name: "PREVIEW_URL", Value: previewURL},
			{Name: "CHECKPOINT_API", Value: fmt.Sprintf("http://preview-extension.preview-operator-system.svc.cluster.local:8090/api/previews/%s", c.Name)},
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
						labelManagedBy:             "preview-operator",
						labelPreviewName:          c.Name,
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
func (r *PreviewReconciler) testJob(c *platformv1alpha1.Preview, nsName, jobName, image string, cmd []string, cmName, fileName, containerName string, withPostgres bool) *batchv1.Job {
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
						labelManagedBy:             "preview-operator",
						labelPreviewName:          c.Name,
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
func (r *PreviewReconciler) testJobNoMount(c *platformv1alpha1.Preview, nsName, jobName, image string, cmd []string, containerName string, withPostgres bool) *batchv1.Job {
	backoffLimit := int32(0)
	ttl := int32(300)

	container := corev1.Container{
		Name:            containerName,
		Image:           image,
		Command:         cmd,
		ImagePullPolicy: corev1.PullIfNotPresent,
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
						labelManagedBy:             "preview-operator",
						labelPreviewName:          c.Name,
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

func databaseEnabledForSpec(c *platformv1alpha1.Preview) bool {
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

func testJobLabels(c *platformv1alpha1.Preview, jobName string) map[string]string {
	return map[string]string{
		labelManagedBy:                "preview-operator",
		labelPreviewName:             c.Name,
		"app.kubernetes.io/component": "test-suite",
		"platform.company.io/task":    jobName,
	}
}

func (r *PreviewReconciler) setTestSuiteCondition(c *platformv1alpha1.Preview) {
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
	if tests.Contract.Phase != "" && tests.Contract.Phase != phaseSkipped {
		parts = append(parts, fmt.Sprintf("contract=%s(%dp/%df)", tests.Contract.Phase, tests.Contract.Passed, tests.Contract.Failed))
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
